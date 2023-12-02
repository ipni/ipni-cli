package dtrack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/dagsync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	// Import so these codecs get registered.
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
)

// AdDistance finds the distance between advertisements on an IPNI
// advertisement chain.
type AdDistance struct {
	depthLimit int64
	p2pHost    host.Host
	ownsHost   bool
	store      *countStore
	sub        *dagsync.Subscriber
}

// NewAdDistance creates a new advertisement chain distance finder.
func NewAdDistance(options ...Option) (*AdDistance, error) {
	opts := getOpts(options)

	var ownsHost bool
	p2pHost := opts.p2pHost
	if p2pHost == nil {
		var err error
		p2pHost, err = libp2p.New()
		if err != nil {
			return nil, err
		}
		ownsHost = true
	}

	store := newCountStore()
	gsds := dssync.MutexWrap(datastore.NewMapDatastore())
	sub, err := dagsync.NewSubscriber(p2pHost, gsds, store.LinkSystem, opts.topic)
	if err != nil {
		return nil, err
	}

	return &AdDistance{
		depthLimit: opts.depthLimit,
		p2pHost:    p2pHost,
		ownsHost:   ownsHost,
		store:      store,
		sub:        sub,
	}, nil
}

// Get returns the number af advertisements from the newest to the oldest
// advertisement on an IPNI advertisement chain. If newestCid is cid.Undef,
// then it referrs to the current head of the chain, and the head CID is
// returned as the 2nd return value.
func (a *AdDistance) Get(ctx context.Context, publisher peer.AddrInfo, oldestCid, newestCid cid.Cid) (int, cid.Cid, error) {
	if oldestCid == cid.Undef {
		return 0, cid.Undef, errors.New("must specify a oldest CID")
	}

	var depthLimit int64
	if a.depthLimit != 0 {
		depthLimit = a.depthLimit + 1
	}

	newestCid, err := a.sub.SyncAdChain(ctx, publisher, dagsync.ScopedDepthLimit(depthLimit),
		dagsync.WithHeadAdCid(newestCid), dagsync.WithStopAdCid(oldestCid))
	if err != nil {
		return 0, cid.Undef, fmt.Errorf("failed to sync chain lastAd=%s depth=%d: %w", a.store.lastKey, a.store.count, err)
	}

	dist := a.store.distance()
	if int64(dist) > a.depthLimit {
		dist = -1
	}

	return dist, newestCid, nil
}

// Close closes the internal dagsync subscriber and the libp2p host if owned by
// this AdDistance instance.
func (a *AdDistance) Close() error {
	a.sub.Close()
	if a.ownsHost {
		return a.p2pHost.Close()
	}
	return nil
}

type countStore struct {
	datastore.Batching
	ipld.LinkSystem
	count   int
	lastKey string
	lastVal []byte
}

func newCountStore() *countStore {
	cs := &countStore{
		Batching: datastore.NewNullDatastore(),
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.StorageReadOpener = func(lctx ipld.LinkContext, lnk ipld.Link) (io.Reader, error) {
		c := lnk.(cidlink.Link).Cid
		if cs.lastKey != c.String() {
			return nil, datastore.ErrNotFound
		}
		return bytes.NewBuffer(cs.lastVal), nil
	}
	lsys.StorageWriteOpener = func(lctx ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := bytes.NewBuffer(nil)
		return buf, func(lnk ipld.Link) error {
			cs.count++
			c := lnk.(cidlink.Link).Cid
			cs.lastKey = c.String()
			cs.lastVal = buf.Bytes()
			return nil
		}, nil
	}
	cs.LinkSystem = lsys
	return cs
}

func (s *countStore) distance() int {
	count := s.count
	s.count = 0
	s.lastKey = ""
	s.lastVal = nil
	return count
}
