package adpub

import (
	"context"
	"errors"
	"fmt"
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
	selectorbuilder "github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Client interface {
	GetAdvertisement(context.Context, cid.Cid) (*Advertisement, error)
	Close() error
	Distance(context.Context, cid.Cid, cid.Cid) (int, error)
}

type client struct {
	*options
	sub *dagsync.Subscriber

	store     *ClientStore
	publisher peer.AddrInfo

	adSel ipld.Node

	removed map[string]struct{}
}

func MakeClient(addrInfo peer.AddrInfo, topic string, entriesDepth int64) (Client, error) {
	if topic == "" {
		return nil, errors.New("topic must be configured when graphsync endpoint is specified")
	}

	if entriesDepth < 0 {
		return nil, fmt.Errorf("ad entries recursion depth limit cannot be less than zero; got %d", entriesDepth)
	}

	var entRecurLim selector.RecursionLimit
	if entriesDepth == 0 {
		entRecurLim = selector.RecursionLimitNone()
	} else {
		entRecurLim = selector.RecursionLimitDepth(entriesDepth)
	}

	return NewClient(addrInfo, WithTopicName(topic), WithEntriesRecursionLimit(entRecurLim))
}

func NewClient(provAddr peer.AddrInfo, o ...Option) (Client, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}

	h, err := libp2p.New()
	if err != nil {
		return nil, err
	}
	h.Peerstore().AddAddrs(provAddr.ID, provAddr.Addrs, time.Hour)

	store := newClientStore()
	sub, err := dagsync.NewSubscriber(h, store.Batching, store.LinkSystem, opts.topic, nil)
	if err != nil {
		return nil, err
	}

	ssb := selectorbuilder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	adSel := ssb.ExploreRecursive(selector.RecursionLimitDepth(1), ssb.ExploreFields(
		func(efsb selectorbuilder.ExploreFieldsSpecBuilder) {
			efsb.Insert("PreviousID", ssb.ExploreRecursiveEdge())
		})).Node()

	return &client{
		options:   opts,
		sub:       sub,
		publisher: provAddr,
		store:     store,
		adSel:     adSel,
		removed:   make(map[string]struct{}),
	}, nil
}

func selectEntriesWithLimit(limit selector.RecursionLimit) datamodel.Node {
	ssb := selectorbuilder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	return ssb.ExploreRecursive(limit, ssb.ExploreFields(
		func(efsb selectorbuilder.ExploreFieldsSpecBuilder) {
			efsb.Insert("Next", ssb.ExploreRecursiveEdge())
		})).Node()
}

// recursionLimit returns the recursion limit for the given depth.
func recursionLimit(depth int) selector.RecursionLimit {
	if depth < 1 {
		return selector.RecursionLimitNone()
	}
	return selector.RecursionLimitDepth(int64(depth))
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

	rLimit := recursionLimit(0)
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

	ctxIDStr := string(ad.ContextID)
	if ad.IsRemove {
		c.removed[ctxIDStr] = struct{}{}
		return ad, nil
	}

	if _, ok := c.removed[ctxIDStr]; ok {
		ad.Entries = nil
		return ad, nil
	}

	// Only sync its entries recursively if it is not a removal advertisement and has entries.
	if ad.HasEntries() {
		_, err = c.syncEntriesWithRetry(ctx, ad.Entries.root)
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

func (c *client) syncEntriesWithRetry(ctx context.Context, id cid.Cid) (cid.Cid, error) {
	var attempt uint64
	recurLimit := c.entriesRecurLimit
	for {
		sel := selectEntriesWithLimit(recurLimit)
		_, err := c.sub.Sync(ctx, c.publisher, id, sel)
		if err == nil {
			return id, nil
		}
		if strings.HasSuffix(err.Error(), "content not found") {
			fmt.Fprintln(os.Stderr, "skipping entries sync; content no longer hosted:", err)
			return cid.Undef, nil
		}
		attempt++
		if attempt > c.maxSyncRetry {
			return cid.Undef, fmt.Errorf("exceeded maximum retries syncing entries: %w", err)
		}
		nextMissing, visitedDepth, present := c.findNextMissingChunkLink(ctx, id)
		if !present {
			return id, nil
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
