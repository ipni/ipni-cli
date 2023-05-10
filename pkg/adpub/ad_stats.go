package adpub

import (
	"errors"
	"fmt"

	"github.com/ipfs/go-datastore"
	"github.com/montanaflynn/stats"
	"github.com/multiformats/go-multihash"
)

type Sampler func() bool

type AdStats struct {
	sampler                 Sampler
	NonRmCount              int
	RmCount                 int
	AdNoLongerProvidedCount int

	ctxIDRm map[string]bool
	samples []*AdSample

	mhCountDist    []interface{}
	chunkCountDist []interface{}
}

type AdSample struct {
	IsRemove         bool
	NoLongerProvided bool
	ctxID            string
	PartiallySynced  bool
	SyncErr          error
	ChunkCount       int
	MhCount          int
	MhSample         []multihash.Multihash
}

func NewAdStats(s Sampler) *AdStats {
	if s == nil {
		s = func() bool { return true }
	}
	return &AdStats{
		ctxIDRm: make(map[string]bool),
		sampler: s,
	}
}

func (a *AdStats) Sample(ad *Advertisement) *AdSample {
	sample := &AdSample{
		IsRemove: ad.IsRemove,
		ctxID:    string(ad.ContextID),
	}

	if sample.IsRemove {
		a.RmCount++
		a.ctxIDRm[sample.ctxID] = true

		a.samples = append(a.samples, sample)
		return sample
	}

	a.NonRmCount++
	removed, seen := a.ctxIDRm[sample.ctxID]
	if seen && removed {
		sample.NoLongerProvided = true
		a.AdNoLongerProvidedCount++

		a.samples = append(a.samples, sample)
		return sample
	}

	a.ctxIDRm[sample.ctxID] = false
	if !ad.HasEntries() {
		a.samples = append(a.samples, sample)
		return sample
	}

	allMhs, err := ad.Entries.Drain()
	if err != nil {
		sample.PartiallySynced = true
		// Most likely caused by entries recursion limit reached.
		if errors.Is(err, datastore.ErrNotFound) {
			err = errors.New("recursion limit reached")
		}
		sample.SyncErr = err
	}
	sample.MhCount = len(allMhs)
	sample.ChunkCount = ad.Entries.ChunkCount()

	for _, mh := range allMhs {
		if a.sampler() {
			sample.MhSample = append(sample.MhSample, mh)
		}
	}
	a.samples = append(a.samples, sample)

	a.mhCountDist = append(a.mhCountDist, sample.MhCount)
	a.chunkCountDist = append(a.chunkCountDist, sample.ChunkCount)
	return sample
}

func (a *AdStats) TotalAdCount() int {
	return a.NonRmCount + a.RmCount
}

func (a *AdStats) UniqueContextIDCount() int {
	return len(a.ctxIDRm)
}

func (a *AdStats) NonRmMhStats() stats.Float64Data {
	return stats.LoadRawData(a.mhCountDist)
}

func (a *AdStats) NonRmChunkStats() stats.Float64Data {
	return stats.LoadRawData(a.chunkCountDist)
}

func (a *AdStats) Print() {
	fmt.Println()
	fmt.Println("Advertisement chain a:")
	fmt.Printf("  # rm ads:                             %d\n", a.RmCount)
	fmt.Printf("  # non-rm ads:                         %d\n", a.NonRmCount)
	fmt.Printf("     # of which had ctx id removed:     %d\n", a.AdNoLongerProvidedCount)
	fmt.Printf("  # unique context IDs:                 %d\n", a.UniqueContextIDCount())

	mhA := a.NonRmMhStats()
	sum, _ := mhA.Sum()
	max, _ := mhA.Max()
	min, _ := mhA.Min()
	mean, _ := mhA.Mean()
	std, _ := mhA.StandardDeviation()
	fmt.Printf("  # max mhs per ad:                     %.0f\n", max)
	fmt.Printf("  # min mhs per ad:                     %.0f\n", min)
	fmt.Printf("  # mean ± std mhs per ad:              %.2f ± %.2f\n", mean, std)

	cA := a.NonRmChunkStats()
	cSum, _ := cA.Sum()
	cMax, _ := cA.Max()
	cMin, _ := cA.Min()
	cMean, _ := cA.Mean()
	cStd, _ := cA.StandardDeviation()
	fmt.Printf("  # max chunks per ad:                  %.0f\n", cMax)
	fmt.Printf("  # min chunks per ad:                  %.0f\n", cMin)
	fmt.Printf("  # mean ± std chunks per ad:           %.2f ± %.2f\n", cMean, cStd)
	fmt.Println("--------------------------------------------")
	fmt.Printf("total ads:                              %d\n", a.TotalAdCount())
	fmt.Printf("total mhs:                              %.0f\n", sum)
	fmt.Printf("total chunks:                           %.0f\n", cSum)
	fmt.Println()
}
