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
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Client interface {
	GetAdvertisement(context.Context, cid.Cid) (*Advertisement, error)
	Close() error
	Distance(context.Context, cid.Cid, cid.Cid) (int, error)
	List(context.Context, cid.Cid, int, io.Writer) error
	SyncEntriesWithRetry(context.Context, cid.Cid) error
}

type client struct {
	entriesDepthLimit selector.RecursionLimit
	maxSyncRetry      uint64
	syncRetryBackoff  time.Duration

	sub *dagsync.Subscriber

	store     *ClientStore
	publisher peer.AddrInfo

	// adSel is the selector for a single advertisement.
	adSel ipld.Node
}

var ErrContentNotFound = errors.New("content not found at publisher")

// NewClient creates a new client for a content advertisement publisher.
func NewClient(addrInfo peer.AddrInfo, options ...Option) (Client, error) {
	opts, err := getOpts(options)
	if err != nil {
		return nil, err
	}

	h, err := libp2p.New()
	if err != nil {
		return nil, err
	}
	h.Peerstore().AddAddrs(addrInfo.ID, addrInfo.Addrs, time.Hour)

	store := newClientStore()
	sub, err := dagsync.NewSubscriber(h, store.Batching, store.LinkSystem, opts.topic, nil)
	if err != nil {
		return nil, err
	}

	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	adSel := ssb.ExploreRecursive(selector.RecursionLimitDepth(1), ssb.ExploreFields(
		func(efsb builder.ExploreFieldsSpecBuilder) {
			efsb.Insert("PreviousID", ssb.ExploreRecursiveEdge())
		})).Node()

	return &client{
		entriesDepthLimit: opts.entriesDepthLimit,
		maxSyncRetry:      opts.maxSyncRetry,
		syncRetryBackoff:  opts.syncRetryBackoff,

		sub:       sub,
		publisher: addrInfo,
		store:     store,
		adSel:     adSel,
	}, nil
}

func selectEntriesWithLimit(limit selector.RecursionLimit) datamodel.Node {
	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	return ssb.ExploreRecursive(limit, ssb.ExploreFields(
		func(efsb builder.ExploreFieldsSpecBuilder) {
			efsb.Insert("Next", ssb.ExploreRecursiveEdge())
		})).Node()
}

func (c *client) Distance(ctx context.Context, oldestCid, newestCid cid.Cid) (int, error) {
	if oldestCid == cid.Undef {
		return 0, errors.New("must specify a oldest CID")
	}

	// Sync the advertisement without entries first.
	var err error
	_, err = c.syncAdWithRetry(ctx, oldestCid)
	if err != nil {
		return 0, err
	}

	// Load the synced advertisement from local store.
	ad, err := c.store.getAdvertisement(ctx, oldestCid)
	if err != nil {
		return 0, err
	}

	// TODO: Allow a maximum depth to be specified for the ad chain.
	rLimit := selector.RecursionLimitNone()

	stopAt := cidlink.Link{Cid: ad.PreviousID}

	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	adSeqSel := ssb.ExploreFields(
		func(efsb builder.ExploreFieldsSpecBuilder) {
			efsb.Insert("PreviousID", ssb.ExploreRecursiveEdge())
		}).Node()

	sel := dagsync.ExploreRecursiveWithStopNode(rLimit, adSeqSel, stopAt)

	newestCid, err = c.sub.Sync(ctx, c.publisher, newestCid, sel)
	if err != nil {
		return 0, err
	}

	return c.store.distance(ctx, oldestCid, newestCid)
}

func (c *client) List(ctx context.Context, latestCid cid.Cid, n int, w io.Writer) error {
	var rLimit selector.RecursionLimit
	if n < 1 {
		rLimit = selector.RecursionLimitNone()
	} else {
		rLimit = selector.RecursionLimitDepth(int64(n))
	}
	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	adSeqSel := ssb.ExploreFields(
		func(efsb builder.ExploreFieldsSpecBuilder) {
			efsb.Insert("PreviousID", ssb.ExploreRecursiveEdge())
		}).Node()
	sel := dagsync.ExploreRecursiveWithStopNode(rLimit, adSeqSel, nil)

	latestCid, err := c.sub.Sync(ctx, c.publisher, latestCid, sel)
	if err != nil {
		return err
	}

	return c.store.list(ctx, latestCid, n, w)
}

func (c *client) GetAdvertisement(ctx context.Context, adCid cid.Cid) (*Advertisement, error) {
	// Sync the advertisement without entries first.
	var err error
	adCid, err = c.syncAdWithRetry(ctx, adCid)
	if err != nil {
		return nil, err
	}

	// Load the synced advertisement from local store.
	ad, err := c.store.getAdvertisement(ctx, adCid)
	if err != nil {
		return nil, err
	}

	if ad.IsRemove {
		return ad, nil
	}

	// Return the partially synced advertisement useful for output to client.
	return ad, err
}

func (c *client) syncAdWithRetry(ctx context.Context, adCid cid.Cid) (cid.Cid, error) {
	var attempt uint64
	var err error
	for {
		adCid, err = c.sub.Sync(ctx, c.publisher, adCid, c.adSel)
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
	var attempt uint64
	recurLimit := c.entriesDepthLimit
	for {
		sel := selectEntriesWithLimit(recurLimit)
		_, err := c.sub.Sync(ctx, c.publisher, id, sel)
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
		nextMissing, visitedDepth, present := c.findNextMissingChunkLink(ctx, id)
		if !present {
			// Reached the end of the chain.
			return nil
		}
		id = nextMissing
		remainingLimit := recurLimit.Depth() - visitedDepth
		recurLimit = selector.RecursionLimitDepth(remainingLimit)
		fmt.Fprintf(os.Stderr, "entries sync retry %d: %s\n", attempt, err)
		time.Sleep(c.syncRetryBackoff)
	}
}

func (c *client) findNextMissingChunkLink(ctx context.Context, next cid.Cid) (cid.Cid, int64, bool) {
	var depth int64
	for {
		if !isPresent(next) {
			return cid.Undef, depth, false
		}
		c, err := c.store.getNextChunkLink(ctx, next)
		if errors.Is(err, datastore.ErrNotFound) {
			return next, depth, true
		}
		next = c
		depth++
	}
}

func (c *client) Close() error {
	return c.sub.Close()
}
