package adpub

import (
	"io"

	"github.com/ipfs/go-datastore"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"

	// Import so these codecs get registered.
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
)

type countStore struct {
	datastore.Batching
	ipld.LinkSystem
	count int
}

func newCountStore() *countStore {
	cs := &countStore{
		Batching: datastore.NewNullDatastore(),
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.StorageReadOpener = func(lctx ipld.LinkContext, lnk ipld.Link) (io.Reader, error) {
		return nil, datastore.ErrNotFound
	}
	lsys.StorageWriteOpener = func(lctx ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		return io.Discard, func(lnk ipld.Link) error {
			cs.count++
			return nil
		}, nil
	}
	cs.LinkSystem = lsys
	return cs
}

func (s *countStore) distance() int {
	return s.count
}
