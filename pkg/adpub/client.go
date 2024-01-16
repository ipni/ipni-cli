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
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

const syncSegmentSize = 2048

type Client interface {
	GetAdvertisement(context.Context, cid.Cid) (*Advertisement, error)
	Close() error
	List(context.Context, cid.Cid, int, io.Writer) error
	SyncEntriesWithRetry(context.Context, cid.Cid) error
}

type client struct {
	entriesDepthLimit int64
	maxSyncRetry      uint64
	syncRetryBackoff  time.Duration

	publisher peer.AddrInfo
	host      host.Host
	ownsHost  bool
	topic     string

	store *ClientStore
	sub   *dagsync.Subscriber
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

	c := &client{
		entriesDepthLimit: opts.entriesDepthLimit,
		maxSyncRetry:      opts.maxSyncRetry,
		syncRetryBackoff:  opts.syncRetryBackoff,

		publisher: addrInfo,
		host:      opts.p2pHost,
		ownsHost:  ownsHost,
		topic:     opts.topic,

		store: newClientStore(),
	}

	c.sub, err = dagsync.NewSubscriber(c.host, c.store.Batching, c.store.LinkSystem, c.topic, dagsync.HttpTimeout(opts.httpTimeout))
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *client) List(ctx context.Context, latestCid cid.Cid, n int, w io.Writer) error {
	opts := []dagsync.SyncOption{dagsync.WithHeadAdCid(latestCid), dagsync.ScopedDepthLimit(int64(n))}
	if n > syncSegmentSize {
		prevAdCid := func(adCid cid.Cid) (cid.Cid, error) {
			ad, err := c.store.loadAd(ctx, adCid)
			if err != nil {
				return cid.Undef, err
			}
			return ad.PreviousCid(), nil
		}
		opts = append(opts, dagsync.ScopedSegmentDepthLimit(syncSegmentSize))
		opts = append(opts, dagsync.ScopedBlockHook(dagsync.MakeGeneralBlockHook(prevAdCid)))
	}
	latestCid, err := c.sub.SyncAdChain(ctx, c.publisher, opts...)
	if err != nil {
		return err
	}

	return c.store.list(ctx, latestCid, n, w)
}

func (c *client) GetAdvertisement(ctx context.Context, adCid cid.Cid) (*Advertisement, error) {
	// Sync the advertisement without entries first.
	adCid, err := c.syncAdWithRetry(ctx, adCid, c.sub)
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

func (c *client) syncAdWithRetry(ctx context.Context, adCid cid.Cid, sub *dagsync.Subscriber) (cid.Cid, error) {
	if c.maxSyncRetry == 0 {
		adCid, err := sub.SyncAdChain(ctx, c.publisher, dagsync.WithHeadAdCid(adCid), dagsync.ScopedDepthLimit(1))
		if err != nil {
			if errors.Is(err, ipld.ErrNotExists{}) || strings.Contains(err.Error(), "content not found") {
				err = ErrContentNotFound
			}
		}
		return adCid, err
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
	var attempt uint64
	recurLimit := c.entriesDepthLimit

	for {
		err := c.sub.SyncEntries(ctx, c.publisher, id, dagsync.ScopedDepthLimit(recurLimit))
		if err == nil {
			// Synced everything asked for.
			return nil
		}
		if errors.Is(err, ipld.ErrNotExists{}) || strings.Contains(err.Error(), "content not found") {
			return ErrContentNotFound
		}
		attempt++
		if attempt > c.maxSyncRetry {
			return fmt.Errorf("exceeded maximum retries syncing entries: %w", err)
		}
		nextMissing, visitedDepth, present := findNextMissingChunkLink(ctx, id, c.store)
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
	c.sub.Close()
	if !c.ownsHost {
		return nil
	}
	return c.host.Close()
}
