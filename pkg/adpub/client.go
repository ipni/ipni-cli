package adpub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Client interface {
	GetAdvertisement(context.Context, cid.Cid) (*Advertisement, error)
	Close() error
	Distance(context.Context, cid.Cid, cid.Cid) (int, cid.Cid, error)
	List(context.Context, cid.Cid, int, io.Writer) error
	SyncEntriesWithRetry(context.Context, cid.Cid) error
}

type client struct {
	adChainDepthLimit int64
	entriesDepthLimit int64
	maxSyncRetry      uint64
	syncRetryBackoff  time.Duration

	publisher peer.AddrInfo
	host      host.Host
	ownsHost  bool
	topic     string
}

var ErrContentNotFound = errors.New("content not found at publisher")

// NewClient creates a new client for a content advertisement publisher.
func NewClient(addrInfo peer.AddrInfo, options ...Option) (Client, error) {
	opts, err := getOpts(options)
	if err != nil {
		return nil, err
	}

	var ownsHost bool
	if opts.p2pHost == nil {
		opts.p2pHost, err = libp2p.New()
		if err != nil {
			return nil, err
		}
		ownsHost = true
	}

	opts.p2pHost.Peerstore().AddAddrs(addrInfo.ID, addrInfo.Addrs, time.Hour)

	return &client{
		adChainDepthLimit: opts.adChainDepthLimit,
		entriesDepthLimit: opts.entriesDepthLimit,
		maxSyncRetry:      opts.maxSyncRetry,
		syncRetryBackoff:  opts.syncRetryBackoff,

		publisher: addrInfo,
		host:      opts.p2pHost,
		ownsHost:  ownsHost,
		topic:     opts.topic,
	}, nil
}

func selectEntriesWithLimit(limit selector.RecursionLimit) datamodel.Node {
	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	return ssb.ExploreRecursive(limit, ssb.ExploreFields(
		func(efsb builder.ExploreFieldsSpecBuilder) {
			efsb.Insert("Next", ssb.ExploreRecursiveEdge())
		})).Node()
}

func (c *client) Distance(ctx context.Context, oldestCid, newestCid cid.Cid) (int, cid.Cid, error) {
	if oldestCid == cid.Undef {
		return 0, cid.Undef, errors.New("must specify a oldest CID")
	}

	var depthLimit int64
	if c.adChainDepthLimit != 0 {
		depthLimit = c.adChainDepthLimit + 1
	}

	// Create a linksystem that only counts, and does not store data.
	cs := newCountStore()
	gsds := dssync.MutexWrap(datastore.NewMapDatastore())
	sub, err := dagsync.NewSubscriber(c.host, gsds, cs.LinkSystem, c.topic)
	if err != nil {
		return 0, cid.Undef, err
	}
	defer sub.Close()

	newestCid, err = sub.SyncAdChain(ctx, c.publisher, dagsync.ScopedDepthLimit(depthLimit),
		dagsync.WithHeadAdCid(newestCid), dagsync.WithStopAdCid(oldestCid))
	if err != nil {
		return 0, cid.Undef, err
	}

	dist := cs.distance()
	if int64(dist) > c.adChainDepthLimit {
		dist = -1
	}

	return dist, newestCid, nil
}

func (c *client) List(ctx context.Context, latestCid cid.Cid, n int, w io.Writer) error {
	store := newClientStore()
	sub, err := dagsync.NewSubscriber(c.host, store.Batching, store.LinkSystem, c.topic)
	if err != nil {
		return err
	}
	defer sub.Close()

	latestCid, err = sub.SyncAdChain(ctx, c.publisher, dagsync.WithHeadAdCid(latestCid), dagsync.ScopedDepthLimit(int64(n)))
	if err != nil {
		return err
	}

	return store.list(ctx, latestCid, n, w)
}

func (c *client) GetAdvertisement(ctx context.Context, adCid cid.Cid) (*Advertisement, error) {
	store := newClientStore()
	sub, err := dagsync.NewSubscriber(c.host, store.Batching, store.LinkSystem, c.topic)
	if err != nil {
		return nil, err
	}
	defer sub.Close()

	// Sync the advertisement without entries first.
	adCid, err = c.syncAdWithRetry(ctx, adCid, sub)
	if err != nil {
		return nil, err
	}

	// Load the synced advertisement from local store.
	ad, err := store.getAdvertisement(ctx, adCid)
	if err != nil {
		return nil, err
	}

	if ad.IsRemove {
		return ad, nil
	}

	// Return the partially synced advertisement useful for output to client.
	return ad, err
}

func (c *client) syncAdWithRetry(ctx context.Context, adCid cid.Cid, sub *dagsync.Subscriber) (cid.Cid, error) {
	if c.maxSyncRetry == 0 {
		return sub.SyncAdChain(ctx, c.publisher, dagsync.WithHeadAdCid(adCid), dagsync.ScopedDepthLimit(1))
	}
	var attempt uint64
	var err error
	for {
		adCid, err = sub.SyncAdChain(ctx, c.publisher, dagsync.WithHeadAdCid(adCid), dagsync.ScopedDepthLimit(1))
		if err == nil {
			return adCid, nil
		}
		attempt++
		if attempt > c.maxSyncRetry {
			var adCidStr string
			if adCid == cid.Undef {
				adCidStr = "undef"
			} else {
				adCidStr = adCid.String()
			}
			return cid.Undef, fmt.Errorf("exceeded maximum retries syncing ad %s: %w", adCidStr, err)
		}
		fmt.Fprintf(os.Stderr, "ad sync retry %d: %s\n", attempt, err)
		time.Sleep(c.syncRetryBackoff)
	}
}

func (c *client) SyncEntriesWithRetry(ctx context.Context, id cid.Cid) error {
	store := newClientStore()
	sub, err := dagsync.NewSubscriber(c.host, store.Batching, store.LinkSystem, c.topic)
	if err != nil {
		return err
	}
	defer sub.Close()

	var attempt uint64
	recurLimit := c.entriesDepthLimit

	for {
		err := sub.SyncEntries(ctx, c.publisher, id, dagsync.ScopedDepthLimit(recurLimit))
		if err == nil {
			// Synced everything asked for by the selector.
			return nil
		}
		if strings.HasSuffix(err.Error(), "content not found") {
			return ErrContentNotFound
		}
		attempt++
		if attempt > c.maxSyncRetry {
			return fmt.Errorf("exceeded maximum retries syncing entries: %w", err)
		}
		nextMissing, visitedDepth, present := findNextMissingChunkLink(ctx, id, store)
		if !present {
			// Reached the end of the chain.
			return nil
		}
		id = nextMissing
		recurLimit -= visitedDepth
		fmt.Fprintf(os.Stderr, "entries sync retry %d: %s\n", attempt, err)
		time.Sleep(c.syncRetryBackoff)
	}
}

func findNextMissingChunkLink(ctx context.Context, next cid.Cid, store *ClientStore) (cid.Cid, int64, bool) {
	var depth int64
	for {
		if !isPresent(next) {
			return cid.Undef, depth, false
		}
		c, err := store.getNextChunkLink(ctx, next)
		if errors.Is(err, datastore.ErrNotFound) {
			return next, depth, true
		}
		next = c
		depth++
	}
}

func (c *client) Close() error {
	if !c.ownsHost {
		return nil
	}
	return c.host.Close()
}
