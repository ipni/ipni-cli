package dtrack

import (
	"context"
	"fmt"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/pcache"
	"github.com/ipni/ipni-cli/pkg/adpub"
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
	errTypePubClient
	errTypeUpdate
)

type distTrack struct {
	dist    int
	head    cid.Cid
	ad      cid.Cid
	err     error
	errType int
}

func RunDistanceTracker(ctx context.Context, include, exclude map[peer.ID]struct{}, provCache *pcache.ProviderCache, depthLimit int64, updateIn, timeout time.Duration) <-chan DistanceUpdate {
	updates := make(chan DistanceUpdate)
	go runTracker(ctx, include, exclude, provCache, updateIn, timeout, depthLimit, updates)

	return updates
}

func runTracker(ctx context.Context, include, exclude map[peer.ID]struct{}, provCache *pcache.ProviderCache, updateIn, timeout time.Duration, depthLimit int64, updates chan<- DistanceUpdate) {
	defer close(updates)

	var lookForNew bool
	var tracks map[peer.ID]*distTrack
	if len(include) == 0 {
		lookForNew = true
		tracks = make(map[peer.ID]*distTrack)
	} else {
		tracks = make(map[peer.ID]*distTrack, len(include))
		for pid := range include {
			if _, ok := exclude[pid]; ok {
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
			if err := provCache.Refresh(ctx); err != nil {
				return
			}
			if lookForNew {
				for _, pinfo := range provCache.List() {
					pid := pinfo.AddrInfo.ID
					if _, ok := tracks[pid]; !ok {
						if _, ok = exclude[pid]; !ok {
							tracks[pid] = &distTrack{}
						}
					}
				}
			}
			updateTracks(ctx, provCache, tracks, timeout, depthLimit, updates)
			timer.Reset(updateIn)
		case <-ctx.Done():
			return
		}
	}
}

func updateTracks(ctx context.Context, provCache *pcache.ProviderCache, tracks map[peer.ID]*distTrack, timeout time.Duration, depthLimit int64, updates chan<- DistanceUpdate) {
	for providerID, track := range tracks {
		updateTrack(ctx, providerID, track, provCache, timeout, depthLimit, updates)
	}
}

func updateTrack(ctx context.Context, pid peer.ID, track *distTrack, provCache *pcache.ProviderCache, timeout time.Duration, depthLimit int64, updates chan<- DistanceUpdate) {
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	pinfo, err := provCache.Get(ctx, pid)
	if err != nil {
		return
	}

	if pinfo == nil {
		if track.errType != errTypeNotFound {
			track.errType = errTypeNotFound
			track.err = fmt.Errorf("provider info not found")
			updates <- DistanceUpdate{
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
			updates <- DistanceUpdate{
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
			updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}

	pubClient, err := adpub.NewClient(*pinfo.Publisher, adpub.WithAdChainDepthLimit(depthLimit))
	if err != nil {
		if track.errType != errTypePubClient {
			track.errType = errTypePubClient
			track.err = fmt.Errorf("cannot create publisher client: %w", err)
			updates <- DistanceUpdate{
				ID:  pid,
				Err: track.err,
			}
		}
		return
	}
	defer pubClient.Close()

	if track.head == cid.Undef {
		dist, head, err := pubClient.Distance(ctx, pinfo.LastAdvertisement, cid.Undef)
		if err != nil {
			if track.errType != errTypeUpdate {
				track.errType = errTypeUpdate
				track.err = fmt.Errorf("cannot get distance update: %w", err)
				updates <- DistanceUpdate{
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
		updates <- DistanceUpdate{
			ID:       pid,
			Distance: dist,
		}
		return
	}

	var updated bool

	// Get distance between old head and new head.
	dist, head, err := pubClient.Distance(ctx, track.head, cid.Undef)
	if err != nil {
		if track.errType != errTypeUpdate {
			track.errType = errTypeUpdate
			track.err = fmt.Errorf("cannot get distance update: %w", err)
			updates <- DistanceUpdate{
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
		updates <- DistanceUpdate{
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
		dist, _, err := pubClient.Distance(ctx, track.ad, pinfo.LastAdvertisement)
		if err != nil {
			if track.errType != errTypeUpdate {
				track.errType = errTypeUpdate
				track.err = fmt.Errorf("cannot get distance update: %w", err)
				updates <- DistanceUpdate{
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
			updates <- DistanceUpdate{
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

	updates <- DistanceUpdate{
		ID:       pid,
		Distance: track.dist,
	}
}
