package adpub

import (
	"bytes"
	"context"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"

	// Import so these codecs get registered.
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
)

type ClientStore struct {
	datastore.Batching
	ipld.LinkSystem

	delAfterRead bool
}

// Advertisement contains information about a schema.Advertisement
type Advertisement struct {
	ID               cid.Cid
	PreviousID       cid.Cid
	ProviderID       peer.ID
	ContextID        []byte
	Metadata         []byte
	Addresses        []string
	Entries          *EntriesIterator
	IsRemove         bool
	ExtendedProvider *schema.ExtendedProvider
	// SigErr is the signature validation error. Nil if signature is valid.
	SigErr error
	// SignerID is the peer.ID of the of the signer.
	SignerID peer.ID
}

func (a *Advertisement) HasEntries() bool {
	return a.Entries != nil && a.Entries.IsPresent()
}

func newClientStore(delAfterRead bool) *ClientStore {
	store := dssync.MutexWrap(datastore.NewMapDatastore())
	lsys := cidlink.DefaultLinkSystem()
	lsys.StorageReadOpener = func(lctx ipld.LinkContext, lnk ipld.Link) (io.Reader, error) {
		c := lnk.(cidlink.Link).Cid
		val, err := store.Get(lctx.Ctx, datastore.NewKey(c.String()))
		if err != nil {
			return nil, err
		}
		return bytes.NewBuffer(val), nil
	}
	lsys.StorageWriteOpener = func(lctx ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := bytes.NewBuffer(nil)
		return buf, func(lnk ipld.Link) error {
			c := lnk.(cidlink.Link).Cid
			return store.Put(lctx.Ctx, datastore.NewKey(c.String()), buf.Bytes())
		}, nil
	}
	return &ClientStore{
		Batching:     store,
		LinkSystem:   lsys,
		delAfterRead: delAfterRead,
	}
}

func (s *ClientStore) getNextChunkLink(ctx context.Context, target cid.Cid) (cid.Cid, error) {
	n, err := s.LinkSystem.Load(linking.LinkContext{Ctx: ctx}, cidlink.Link{Cid: target}, schema.EntryChunkPrototype)
	if err != nil {
		return cid.Undef, err
	}

	chunk, err := schema.UnwrapEntryChunk(n)
	if err != nil {
		return cid.Undef, err
	}
	if chunk.Next == nil {
		return cid.Undef, nil
	}
	return chunk.Next.(cidlink.Link).Cid, nil
}

func (s *ClientStore) getEntriesChunk(ctx context.Context, target cid.Cid) (cid.Cid, []multihash.Multihash, error) {
	n, err := s.LinkSystem.Load(linking.LinkContext{Ctx: ctx}, cidlink.Link{Cid: target}, schema.EntryChunkPrototype)
	if err != nil {
		return cid.Undef, nil, err
	}
	if s.delAfterRead {
		s.Batching.Delete(ctx, datastore.NewKey(target.String()))
	}

	chunk, err := schema.UnwrapEntryChunk(n)
	if err != nil {
		return cid.Undef, nil, err
	}
	var next cid.Cid
	if chunk.Next == nil {
		next = cid.Undef
	} else {
		next = chunk.Next.(cidlink.Link).Cid
	}

	return next, chunk.Entries, nil
}

func (s *ClientStore) loadAd(ctx context.Context, id cid.Cid) (schema.Advertisement, error) {
	dsKey := datastore.NewKey(id.String())
	val, err := s.Batching.Get(ctx, dsKey)
	if err != nil {
		return schema.Advertisement{}, err
	}
	if s.delAfterRead {
		s.Batching.Delete(ctx, dsKey)
	}
	return schema.BytesToAdvertisement(id, val)
}

func (s *ClientStore) getAdvertisement(ctx context.Context, id cid.Cid) (*Advertisement, error) {
	ad, err := s.loadAd(ctx, id)
	if err != nil {
		return nil, err
	}

	dprovid, err := peer.Decode(ad.Provider)
	if err != nil {
		return nil, err
	}

	a := &Advertisement{
		ID:               id,
		ProviderID:       dprovid,
		ContextID:        ad.ContextID,
		Metadata:         ad.Metadata,
		Addresses:        ad.Addresses,
		PreviousID:       ad.PreviousCid(),
		IsRemove:         ad.IsRm,
		ExtendedProvider: ad.ExtendedProvider,
	}

	if ad.Entries != nil {
		entriesCid := ad.Entries.(cidlink.Link).Cid
		if entriesCid != cid.Undef {
			a.Entries = &EntriesIterator{
				root:  entriesCid,
				next:  entriesCid,
				ctx:   ctx,
				store: s,
			}
		}
	}

	a.SignerID, a.SigErr = ad.VerifySignature()

	return a, nil
}

func (s *ClientStore) list(ctx context.Context, nextCid cid.Cid, n int, w io.Writer) error {
	for i := 0; i < n; i++ {
		ad, err := s.loadAd(ctx, nextCid)
		if err != nil {
			return err
		}
		if _, err = io.WriteString(w, nextCid.String()); err != nil {
			return err
		}
		if _, err = io.WriteString(w, "\n"); err != nil {
			return err
		}

		if ad.PreviousID == nil {
			break
		}
		nextCid = ad.PreviousID.(cidlink.Link).Cid
	}
	return nil
}

func (s *ClientStore) crawl(ctx context.Context, nextCid cid.Cid, n int, ads chan<- *Advertisement) (cid.Cid, error) {
	for i := 0; i < n; i++ {
		ad, err := s.loadAd(ctx, nextCid)
		if err != nil {
			return cid.Undef, err
		}

		dprovid, err := peer.Decode(ad.Provider)
		if err != nil {
			return cid.Undef, err
		}

		a := &Advertisement{
			ID:               nextCid,
			ProviderID:       dprovid,
			ContextID:        ad.ContextID,
			Metadata:         ad.Metadata,
			Addresses:        ad.Addresses,
			PreviousID:       ad.PreviousCid(),
			IsRemove:         ad.IsRm,
			ExtendedProvider: ad.ExtendedProvider,
		}

		if ad.Entries != nil {
			entriesCid := ad.Entries.(cidlink.Link).Cid
			if entriesCid != cid.Undef {
				a.Entries = &EntriesIterator{
					root:  entriesCid,
					next:  entriesCid,
					ctx:   ctx,
					store: s,
				}
			}
		}

		select {
		case ads <- a:
		case <-ctx.Done():
			return cid.Undef, nil
		}

		if ad.PreviousID == nil {
			return cid.Undef, nil
		}

		nextCid = ad.PreviousID.(cidlink.Link).Cid
	}
	return nextCid, nil
}
