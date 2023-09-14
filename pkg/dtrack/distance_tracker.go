package dtrack

import (
	"context"
	"fmt"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/pcache"
	"github.com/libp2p/go-libp2p/core/peer"
)

type DistanceUpdate struct {
	ID       peer.ID
	Distance int
	Err      error
}

const (
	errTypeNone = iota
	errTypeNoPublisher
	errTypeNoSync
	errTypeNotFound
	errTypeUpdate
)

type distTrack struct {
	dist    int
	head    cid.Cid
	ad      cid.Cid
	err     error
	errType int
}

type tracker struct {
	adDist   *AdDistance
	include  map[peer.ID]struct{}
	exclude  map[peer.ID]struct{}
	pcache   *pcache.ProviderCache
	updateIn time.Duration
	timeout  time.Duration
	updates  chan<- DistanceUpdate
}

func RunDistanceTracker(ctx context.Context, include, exclude map[peer.ID]struct{}, provCache *pcache.ProviderCache, updateIn, timeout time.Duration, options ...Option) (<-chan DistanceUpdate, error) {
	adDist, err := NewAdDistance(options...)
	if err != nil {
		return nil, err
	}

	updates := make(chan DistanceUpdate)

	tkr := &tracker{
		adDist:   adDist,
		include:  include,
		exclude:  exclude,
		pcache:   provCache,
		updateIn: updateIn,
		timeout:  timeout,
		updates:  updates,
	}

	go tkr.run(ctx)

	return updates, nil
}

func (tkr *tracker) run(ctx context.Context) {
	defer close(tkr.updates)
	defer tkr.adDist.Close()

	var lookForNew bool
	var tracks map[peer.ID]*distTrack
	if len(tkr.include) == 0 {
		lookForNew = true
		tracks = make(map[peer.ID]*distTrack)
	} else {
		tracks = make(map[peer.ID]*distTrack, len(tkr.include))
		for pid := range tkr.include {
			if _, ok := tkr.exclude[pid]; ok {
				continue
			}
			tracks[pid] = &distTrack{}
		}
	}

	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			if err := tkr.pcache.Refresh(ctx); err != nil {
				return
			}
			if lookForNew {
				for _, pinfo := range tkr.pcache.List() {
					pid := pinfo.AddrInfo.ID
					if _, ok := tracks[pid]; !ok {
						if _, ok = tkr.exclude[pid]; !ok {
							tracks[pid] = &distTrack{}
						}
					}
				}
			}
			tkr.updateTracks(ctx, tracks)
			timer.Reset(tkr.updateIn)
		case <-ctx.Done():
			return
		}
	}
}

func (tkr *tracker) updateTracks(ctx context.Context, tracks map[peer.ID]*distTrack) {
	for providerID, track := range tracks {
		tkr.updateTrack(ctx, providerID, track)
	}
}

func (tkr *tracker) updateTrack(ctx context.Context, pid peer.ID, track *distTrack) {
	if tkr.timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, tkr.timeout)
		defer cancel()
	}

	pinfo, err := tkr.pcache.Get(ctx, pid)
	if err != nil {
		return
	}

	if pinfo == nil {
		if track.errType != errTypeNotFound {
			track.errType = errTypeNotFound
			track.err = fmt.Errorf("provider info not found")
			tkr.updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}

	if pinfo.LastAdvertisement == cid.Undef {
		if track.errType != errTypeNoSync {
			track.errType = errTypeNoSync
			track.err = fmt.Errorf("provider never synced")
			tkr.updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}

	if pinfo.Publisher == nil || pinfo.Publisher.ID.Validate() != nil || len(pinfo.Publisher.Addrs) == 0 {
		if track.errType != errTypeNoPublisher {
			track.errType = errTypeNoPublisher
			track.err = fmt.Errorf("no advertisement publisher")
			tkr.updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}

	if track.head == cid.Undef {
		dist, head, err := tkr.adDist.Get(ctx, *pinfo.Publisher, pinfo.LastAdvertisement, cid.Undef)
		if err != nil {
			if track.errType != errTypeUpdate {
				track.errType = errTypeUpdate
				track.err = fmt.Errorf("cannot get distance from chain head to last seen ad: %w", err)
				tkr.updates <- DistanceUpdate{
					ID:  pid,
					Err: track.err,
				}
			}
			return
		}
		track.err = nil
		track.errType = errTypeNone
		track.ad = pinfo.LastAdvertisement
		track.dist = dist
		if dist != -1 {
			track.head = head
		}
		tkr.updates <- DistanceUpdate{
			ID:       pid,
			Distance: dist,
		}
		return
	}

	var updated bool

	// Get distance between old head and new head.
	dist, head, err := tkr.adDist.Get(ctx, *pinfo.Publisher, track.head, cid.Undef)
	if err != nil {
		if track.errType != errTypeUpdate {
			track.errType = errTypeUpdate
			track.err = fmt.Errorf("cannot get distance from chain head to last seen head: %w", err)
			tkr.updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}
	track.err = nil
	track.errType = errTypeNone
	if dist == -1 {
		track.dist = -1
		track.head = cid.Undef
		tkr.updates <- DistanceUpdate{
			ID:       pid,
			Distance: -1,
		}
		return
	}
	if head != track.head {
		track.dist += dist
		track.head = head
		updated = true
	}

	if pinfo.LastAdvertisement != track.ad {
		// If the last seen advertisement has changed, then get the distance it has moved.
		dist, _, err := tkr.adDist.Get(ctx, *pinfo.Publisher, track.ad, pinfo.LastAdvertisement)
		if err != nil {
			if track.errType != errTypeUpdate {
				track.errType = errTypeUpdate
				track.err = fmt.Errorf("cannot get distance distance last as has moved: %w", err)
				tkr.updates <- DistanceUpdate{
					ID:  pid,
					Err: track.err,
				}
			}
			return
		}
		track.err = nil
		track.errType = errTypeNone
		if dist == -1 {
			track.dist = -1
			track.head = cid.Undef
			tkr.updates <- DistanceUpdate{
				ID:       pid,
				Distance: -1,
			}
			return
		}
		track.ad = pinfo.LastAdvertisement
		track.dist -= dist
		updated = true
	}

	if !updated {
		return
	}

	tkr.updates <- DistanceUpdate{
		ID:       pid,
		Distance: track.dist,
	}
}
