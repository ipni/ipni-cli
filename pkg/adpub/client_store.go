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
	"github.com/ipld/go-ipld-prime/multicodec"
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

func newClientStore() *ClientStore {
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
		Batching:   store,
		LinkSystem: lsys,
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

func (s *ClientStore) getAdvertisement(ctx context.Context, id cid.Cid) (*Advertisement, error) {
	val, err := s.Batching.Get(ctx, datastore.NewKey(id.String()))
	if err != nil {
		return nil, err
	}

	nb := schema.AdvertisementPrototype.NewBuilder()
	decoder, err := multicodec.LookupDecoder(id.Prefix().Codec)
	if err != nil {
		return nil, err
	}

	err = decoder(nb, bytes.NewBuffer(val))
	if err != nil {
		return nil, err
	}
	node := nb.Build()

	ad, err := schema.UnwrapAdvertisement(node)
	if err != nil {
		return nil, err
	}

	dprovid, err := peer.Decode(ad.Provider)
	if err != nil {
		return nil, err
	}

	var prevCid cid.Cid
	if ad.PreviousID != nil {
		prevCid = ad.PreviousID.(cidlink.Link).Cid
	}

	a := &Advertisement{
		ID:               id,
		ProviderID:       dprovid,
		ContextID:        ad.ContextID,
		Metadata:         ad.Metadata,
		Addresses:        ad.Addresses,
		PreviousID:       prevCid,
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

func (s *ClientStore) distance(ctx context.Context, oldestCid, newestCid cid.Cid) (int, error) {
	var count int
	for newestCid != oldestCid {
		val, err := s.Batching.Get(ctx, datastore.NewKey(newestCid.String()))
		if err != nil {
			return 0, err
		}

		nb := schema.AdvertisementPrototype.NewBuilder()
		decoder, err := multicodec.LookupDecoder(newestCid.Prefix().Codec)
		if err != nil {
			return 0, err
		}

		err = decoder(nb, bytes.NewBuffer(val))
		if err != nil {
			return 0, err
		}
		node := nb.Build()

		ad, err := schema.UnwrapAdvertisement(node)
		if err != nil {
			return 0, err
		}

		count++

		if ad.PreviousID == nil {
			break
		}
		newestCid = ad.PreviousID.(cidlink.Link).Cid
	}
	return count, nil
}
