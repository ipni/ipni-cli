package adpub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	selectorbuilder "github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

var log = logging.Logger("client")

type Client interface {
	GetAdvertisement(ctx context.Context, id cid.Cid) (*Advertisement, error)
	Close() error
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
		if attempt > c.maxSyncRetry {
			log.Errorw("Reached maximum retry attempt while syncing ad", "cid", adCid, "attempt", attempt, "err", err)
			return cid.Undef, err
		}
		attempt++
		log.Infow("retrying ad sync", "attempt", attempt, "err", err)
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
			log.Warnw("Skipping entries sync; content no longer hosted", "cid", id, "err", err)
			return cid.Undef, nil
		}
		if attempt > c.maxSyncRetry {
			log.Errorw("Reached maximum retry attempt while syncing entries", "cid", id, "attempt", attempt, "err", err)
			return cid.Undef, err
		}
		nextMissing, visitedDepth, present := c.findNextMissingChunkLink(ctx, id)
		if !present {
			return id, nil
		}
		id = nextMissing
		attempt++
		remainingLimit := recurLimit.Depth() - visitedDepth
		recurLimit = selector.RecursionLimitDepth(remainingLimit)
		log.Infow("Retrying entries sync", "recurLimit", remainingLimit, "attempt", attempt, "err", err)
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
