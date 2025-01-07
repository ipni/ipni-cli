package verify

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/index"
	"github.com/ipni/go-libipni/apierror"
	"github.com/ipni/go-libipni/find/client"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/pcache"
	"github.com/ipni/ipni-cli/pkg/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/urfave/cli/v3"
)

var (
	include           adpub.Sampler
	provId            string
	samplingProb      float64
	rngSeed           int64
	printUnindexedMhs bool
)

var verifyIngestSubCmd = &cli.Command{
	Name: "ingest",
	Usage: "Verifies an indexer's ingestion of multihashes. " +
		"Multihashes can be read from a publisher, from a CAR file, or from a CARv2 Index",
	Description: `This command verifies whether a list of multihashes are ingested by an indexer and have the 
expected provider Peer ID. The multihashes to verify can be supplied from one of the following 
sources:
- Provider's GraphSync or HTTP publisher endpoint.
- Path to a CAR file (i.e. --from-car)
- Path to a CARv2 index file in iterable multihash format (i.e. --from-car-index)

The user may optionally specify an advertisement CID, or to use the latestadvertisement seen by the
indexer, as the source of multihash entries. If a CID is not specified then the latest advertisement
is fetched from the publisher and its entries are used as the source of multihashes.

The path to CAR files may point to any CAR version (CARv1 or CARv2). The list of multihashes are
generated automatically from the CAR payload if no suitable index is present.

The path to a CARv2 index file must point to an index in iterable multihash format, i.e. have
'multicodec.CarMultihashIndexSorted'. See: https://github.com/ipld/go-car

The user must also specify the address or URL of the indexer node. If not specified, the default
value of 'http://localhost:3000' is used.

By default, all multihashes from the source are verified with the indexer. The user may specify a
sampling probability to define the chance that each multihash is verified. Selection uses a uniform
random distribution. The random number generator seed may be specified to make the selection
deterministic for debugging purposes.

Example usage:

* Verify ingest from provider's GraphSync publisher endpoint for a specific advertisement CID,
  selecting 50% of available multihashes using deterministic random number generator, seeded with '1413':
	verify ingest --provider-id 12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ \
		--indexer https://cid.contact \
		--ad-cid baguqeeqqcbuegh2hzk7sukqpsz24wg3tk4 \
		--sampling-prob 0.5 --rng-seed 1413

* Verify ingestion from CAR file, selecting 50% of available multihashes using a deterministic 
  random number generator, seeded with '1413':
	verify ingest --provider-id 12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ \
		--from-car my-dag.car \
		--indexer http://192.168.2.100:3000 \
		--sampling-prob 0.5 --rng-seed 1413

* Verify ingestion from CARv2 index file using all available multihashes:
	verify ingest --provider-id 12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ \
		--from-car my-idx.idx \
		--indexer http://192.168.2.100:3000

The output respectively prints:
- The number of multihashes the tool failed to verify, e.g. due to communication error.
- The number of multihashes not indexed by the indexer.
- The number of multihashes known by the indexer but not associated to the given provider Peer ID.
- The number of multihashes known with expected provider Peer ID.
- And finally, total number of multihashes verified.

A verification is considered as passed when the total number of multihashes checked matches the 
number of multihashes that are indexed with the expected provider Peer ID.`,
	Flags:  verifyIngestFlags,
	Before: beforeVerifyIngest,
	Action: verifyIngestAction,
}

var verifyIngestFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "provider-id",
		Aliases: []string{"pid"},
		Usage: "The peer ID of the provider associated with multihashes. " +
			"If neither from-car nor from-car-index are specified, then get multihashes from this provider's publisher. " +
			"The advertisement publisher address is automatically discovered by querying the indexer with provider's peer ID.",
		Required:    true,
		Destination: &provId,
	},
	&cli.StringFlag{
		Name:    "from-car",
		Usage:   "Path to the CAR file from which to extract the list of multihash for verification.",
		Aliases: []string{"fc"},
	},
	&cli.StringFlag{
		Name:    "from-car-index",
		Usage:   "Path to the CAR index file from which to extract the list of multihash for verification.",
		Aliases: []string{"fci"},
	},
	&cli.StringSliceFlag{
		Name:    "indexer",
		Usage:   "URL of indexer to query. Multiple OK to specify providers info sources for dhstore.",
		Aliases: []string{"i"},
	},
	&cli.StringFlag{
		Name:    "dhstore",
		Usage:   "URL of double-hashed (reader-private) store, if different from indexer",
		Aliases: []string{"dhs"},
	},
	&cli.Float64Flag{
		Name:        "sampling-prob",
		Aliases:     []string{"sp"},
		Usage:       "The uniform random probability of selecting a multihash for verification specified as a value between 0.0 and 1.0.",
		DefaultText: "'1.0' i.e. 100% of multihashes will be checked for verification.",
		Value:       1.0,
		Destination: &samplingProb,
	},
	&cli.Int64Flag{
		Name:    "rng-seed",
		Aliases: []string{"rs"},
		Usage: "The seed to use for the random number generator that selects verification samples. " +
			"This flag has no impact if sampling probability is set to 1.0.",
		DefaultText: "Non-deterministic.",
		Destination: &rngSeed,
	},
	&cli.StringFlag{
		Name:        "ad-cid",
		Aliases:     []string{"a"},
		Usage:       "The advertisement CID to start fetching the chain at. Only takes effect if multihashes read from publisher.",
		DefaultText: "Dynamically fetch the latest advertisement CID",
	},
	&cli.BoolFlag{
		Name: "ad-last-seen",
		Usage: "Start fetching advertisement chain at the last one seen by the indexer. " +
			"This is an alternative to ad-cid and only takes effect if multihashes read from publisher.",
		DefaultText: "Dynamically fetch the latest advertisement CID",
	},
	&cli.IntFlag{
		Name:        "ad-depth-limit",
		Aliases:     []string{"adl"},
		Usage:       "The number of advertisements to verify. Only takes effect if multihashes read from publisher.",
		Value:       1,
		DefaultText: "Verify a single advertisement only.",
	},
	&cli.Int64Flag{
		Name:        "entries-depth-limit",
		Aliases:     []string{"edl"},
		Usage:       "Maximum depth (number of blocks of multihashes) to fetch from advertisement entries chains.",
		Value:       100,
		DefaultText: "100 (set to '0' for unlimited)",
	},
	&cli.IntFlag{
		Name:    "batch-size",
		Aliases: []string{"bs"},
		Usage: "The number multihashes in each lookup request to the indexer. " +
			"A smaller batch size will increase the number of requests to the indexer but may avoid timing out waiting for a response.",
		Value: 4096,
	},
	&cli.BoolFlag{
		Name:        "print-unindexed-mhs",
		Usage:       "Print multihashes that are not indexed by the indexer. Only printed if the indexer is successfully contacted.",
		Aliases:     []string{"pum"},
		Destination: &printUnindexedMhs,
	},
	&cli.BoolFlag{
		Name:  "private",
		Usage: "Use reader-privacy for queries.",
	},
}

func beforeVerifyIngest(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	if len(cmd.StringSlice("indexer")) == 0 {
		if !cmd.Bool("private") {
			return ctx, cli.Exit("missing value for --indexer", 1)
		}
		if cmd.String("dhstore") == "" {
			return ctx, cli.Exit("missing value for --dhstore and --indexer", 1)
		}
	}

	if samplingProb <= 0 || samplingProb > 1 {
		return ctx, cli.Exit("Sampling probability must be larger than 0.0 and smaller or equal to 1.0.", 1)
	}

	if samplingProb == 1 {
		include = func() bool {
			return true
		}
	} else {
		if rngSeed == 0 {
			rngSeed = time.Now().UnixNano()
		}
		rng := rand.New(rand.NewSource(rngSeed))
		include = func() bool {
			return rng.Float64() <= samplingProb
		}
	}

	return ctx, nil
}

func verifyIngestAction(ctx context.Context, cmd *cli.Command) error {
	providerID := cmd.String("provider-id")
	provID, err := peer.Decode(providerID)
	if err != nil {
		return err
	}

	carPath := cmd.String("from-car")
	carIndexPath := cmd.String("from-car-index")

	// If car path specified, then ingest from car.
	if carPath != "" {
		if carIndexPath != "" {
			return errVerifyIngestMultipleSources()
		}
		return verifyIngestFromCar(ctx, cmd, provID, carPath)
	}

	// If car index path specified, then ingest from car index.
	if carIndexPath != "" {
		if carPath != "" {
			return errVerifyIngestMultipleSources()
		}
		return verifyIngestFromCarIndex(ctx, cmd, provID, carIndexPath)
	}

	// If neither car nor car index path specified, then ingest from provider.
	return verifyIngestFromProvider(ctx, cmd, provID)
}

func verifyIngestFromProvider(ctx context.Context, cmd *cli.Command, provID peer.ID) error {
	startAt := "at head of chain from publisher"
	adCid := cid.Undef
	if cmd.String("ad-cid") != "" {
		if cmd.Bool("last-seen") {
			return cli.Exit("Cannot specify both ad-cid and ad-last-seen.", 1)
		}
		var err error
		adCid, err = cid.Decode(cmd.String("ad-cid"))
		if err != nil {
			return err
		}
		startAt = "specified: " + cmd.String("ad-cid")
	}

	adDepthLimit := cmd.Int("ad-depth-limit")
	adDepthLimitStr := "∞"
	if adDepthLimit != 0 {
		if adDepthLimit < 0 {
			return fmt.Errorf("advertiserment recursion limit cannot be less than zero; got %d", adDepthLimit)
		}
		adDepthLimitStr = fmt.Sprintf("%d", adDepthLimit)
	}

	var dhFind *client.DHashClient
	var clearFind *client.Client
	var provCache *pcache.ProviderCache
	var err error

	if cmd.Bool("private") {
		dhFind, err = client.NewDHashClient(
			client.WithProvidersURL(cmd.StringSlice("indexer")...),
			client.WithDHStoreURL(cmd.String("dhstore")),
			client.WithPcacheTTL(0),
		)
		if err != nil {
			return err
		}
		provCache = dhFind.PCache()
	} else {
		idxr := cmd.String("dhstore")
		if idxr == "" {
			idxr = cmd.StringSlice("indexer")[0]
		}
		clearFind, err = client.New(idxr)
		if err != nil {
			return err
		}
		provCache, err = pcache.New(pcache.WithSourceURL(idxr),
			pcache.WithRefreshInterval(0))
		if err != nil {
			return err
		}
	}

	// Get publisher address, for specified provider, from indexer.
	provInfo, err := provCache.Get(ctx, provID)
	if err != nil {
		var ae *apierror.Error
		if errors.As(err, &ae) && ae.Status() == http.StatusNotFound {
			return fmt.Errorf("provider %s not found on indexer", provID)
		}
		return fmt.Errorf("cannot get provider info: %s", err.Error())
	}
	if provInfo == nil {
		return fmt.Errorf("provider %s not found on indexer", provID)
	}
	if provInfo.Publisher == nil {
		return fmt.Errorf("provider %s has no publisher", provID)
	}

	pubAddrInfo := peer.AddrInfo{
		ID:    provInfo.Publisher.ID,
		Addrs: provInfo.Publisher.Addrs,
	}
	fmt.Println("Publisher:", pubAddrInfo.String())
	fmt.Printf("Ads/Entries depth: %s/%d\n", adDepthLimitStr, cmd.Int64("entries-depth-limit"))
	fmt.Println("Last ad seen by indexer:", provInfo.LastAdvertisement.String())

	pubClient, err := adpub.NewClient(pubAddrInfo,
		adpub.WithEntriesDepthLimit(cmd.Int64("entries-depth-limit")))
	if err != nil {
		return err
	}

	stats := adpub.NewAdStats(include)

	// If ad-last-seen specified, then use last advertisement seen by indexer.
	if cmd.Bool("ad-last-seen") {
		adCid = provInfo.LastAdvertisement
		startAt = "last seen by indexer: " + adCid.String()
	}

	fmt.Println("Verification starting at advertisement", startAt)
	fmt.Println()
	var aggResult verifyResult
	for i := 1; i <= adDepthLimit; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ad, err := pubClient.GetAdvertisement(ctx, adCid)
		if err != nil {
			if ad == nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "⚠️ Failed to fully sync advertisement %s. Output shows partially synced ad.\n  Error: %s\n", adCid, err.Error())
		}

		fmt.Printf("Advertisement ID:          %s\n", ad.ID)
		fmt.Printf("Previous Advertisement ID: %s\n", ad.PreviousID)
		fmt.Printf("Verifying ingest... (%d/%s)\n", i, adDepthLimitStr)
		if ad.IsRemove {
			fmt.Println("✂️ Removal advertisement; skipping verification.")
		} else if !ad.HasEntries() {
			fmt.Println("Has no entries; skipping verification.")
		} else {
			err = pubClient.SyncEntriesWithRetry(ctx, ad.Entries.Root())
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠️ Failed to sync entries for advertisement %s: %s\n", ad.ID, err)
			}

			ads := stats.Sample(ad)
			if ads.NoLongerProvided {
				fmt.Println("🧹 Removed in later advertisements; skipping verification.")
			} else {
				var entriesOutput string
				if ads.PartiallySynced {
					entriesOutput = "; ad entries are partially synced due to: " + ads.SyncErr.Error()
				}

				fmt.Printf("Total Entries:             %d over %d chunk(s)%s\n", ads.MhCount, ads.ChunkCount, entriesOutput)
				fmt.Print("Verification: ")
				if len(ads.MhSample) == 0 {
					fmt.Println("🔘 Skipped; sampling did not include any multihashes.")
				} else {
					result, err := verifyIngestFromMhs(ctx, cmd, clearFind, dhFind, provID, ads.MhSample)
					if err != nil {
						return err
					}
					aggResult.add(result)
					if result.passedVerification() {
						fmt.Println("✅ Pass")
					} else {
						fmt.Println("❌ Fail")
					}
				}
			}
		}
		fmt.Println("-----------------------")

		// Stop verification if there is no link to previous advertisement.
		if ad.PreviousID == cid.Undef {
			break
		}

		adCid = ad.PreviousID
	}

	aggResult.print(samplingProb, rngSeed, printUnindexedMhs)
	stats.Print()
	return nil
}

func verifyIngestFromCar(ctx context.Context, cmd *cli.Command, provID peer.ID, carPath string) error {
	carPath = path.Clean(carPath)

	idx, err := getOrGenerateCarIndex(carPath)
	if err != nil {
		return err
	}

	var dhFind *client.DHashClient
	var clearFind *client.Client
	if cmd.Bool("private") {
		dhFind, err = client.NewDHashClient(
			client.WithProvidersURL(cmd.StringSlice("indexer")...),
			client.WithDHStoreURL(cmd.String("dhstore")),
			client.WithPcacheTTL(0),
		)
		if err != nil {
			return err
		}
	} else {
		idxr := cmd.String("dhstore")
		if idxr == "" {
			idxr = cmd.StringSlice("indexer")[0]
		}
		clearFind, err = client.New(idxr)
		if err != nil {
			return err
		}
	}

	result, err := verifyIngestFromCarIterableIndex(ctx, cmd, clearFind, dhFind, provID, idx)
	if err != nil {
		return err
	}

	result.print(samplingProb, rngSeed, printUnindexedMhs)
	return nil
}

func getOrGenerateCarIndex(carPath string) (index.IterableIndex, error) {
	cr, err := car.OpenReader(carPath)
	if err != nil {
		return nil, err
	}
	idxReader, err := cr.IndexReader()
	if err != nil {
		return nil, err
	}

	if idxReader == nil {
		return generateIterableIndex(cr)
	}

	idx, err := index.ReadFrom(idxReader)
	if err != nil {
		return nil, err
	}
	if idx.Codec() != multicodec.CarMultihashIndexSorted {
		// Index doesn't contain full multihashes; generate it.
		return generateIterableIndex(cr)
	}
	return idx.(index.IterableIndex), nil
}

func generateIterableIndex(cr *car.Reader) (index.IterableIndex, error) {
	idx := index.NewMultihashSorted()
	dr, err := cr.DataReader()
	if err != nil {
		return nil, err
	}
	if err := car.LoadIndex(idx, dr); err != nil {
		return nil, err
	}
	return idx, nil
}

func verifyIngestFromCarIndex(ctx context.Context, cmd *cli.Command, provID peer.ID, carIndexPath string) error {
	carIndexPath = path.Clean(carIndexPath)

	idxFile, err := os.Open(carIndexPath)
	if err != nil {
		return err
	}
	idx, err := index.ReadFrom(idxFile)
	if err != nil {
		return err
	}

	iterIdx, ok := idx.(index.IterableIndex)
	if !ok {
		return errInvalidCarIndexFormat()
	}

	var dhFind *client.DHashClient
	var clearFind *client.Client
	if cmd.Bool("private") {
		dhFind, err = client.NewDHashClient(
			client.WithProvidersURL(cmd.StringSlice("indexer")...),
			client.WithDHStoreURL(cmd.String("dhstore")),
			client.WithPcacheTTL(0),
		)
		if err != nil {
			return err
		}
	} else {
		idxr := cmd.String("dhstore")
		if idxr == "" {
			idxr = cmd.StringSlice("indexer")[0]
		}
		clearFind, err = client.New(idxr)
		if err != nil {
			return err
		}
	}

	result, err := verifyIngestFromCarIterableIndex(ctx, cmd, clearFind, dhFind, provID, iterIdx)
	if err != nil {
		return err
	}

	result.print(samplingProb, rngSeed, printUnindexedMhs)
	return nil
}

func errInvalidCarIndexFormat() cli.ExitCoder {
	return cli.Exit("CAR index must be in iterable multihash format; see: multicodec.CarMultihashIndexSorted", 1)
}

func errVerifyIngestMultipleSources() error {
	return cli.Exit("Multiple multihash sources are specified. Only a single source at a time is supported.", 1)
}

func verifyIngestFromCarIterableIndex(ctx context.Context, cmd *cli.Command, find *client.Client, dhFind *client.DHashClient, provID peer.ID, idx index.IterableIndex) (*verifyResult, error) {
	var mhs []multihash.Multihash
	if err := idx.ForEach(func(mh multihash.Multihash, _ uint64) error {
		if include() {
			mhs = append(mhs, mh)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return verifyIngestFromMhs(ctx, cmd, find, dhFind, provID, mhs)
}

type verifyResult struct {
	TotalMhChecked   int
	ProviderMismatch int
	Present          int
	Absent           int
	FailedToVerify   int
	Errs             []error
	AbsentMhs        []multihash.Multihash
}

func (r *verifyResult) passedVerification() bool {
	return r.Present == r.TotalMhChecked
}

func (r *verifyResult) add(other *verifyResult) {
	r.TotalMhChecked += other.TotalMhChecked
	r.ProviderMismatch += other.ProviderMismatch
	r.Present += other.Present
	r.Absent += other.Absent
	r.FailedToVerify += other.FailedToVerify
	r.Errs = append(r.Errs, other.Errs...)
	r.AbsentMhs = append(r.AbsentMhs, other.AbsentMhs...)
}

func (r *verifyResult) print(samplingProb float64, rngSeed int64, printUnindexedMhs bool) {
	fmt.Println()
	fmt.Println("Verification result:")
	fmt.Printf("  # failed to verify:                   %d\n", r.FailedToVerify)
	fmt.Printf("  # unindexed:                          %d\n", r.Absent)
	fmt.Printf("  # indexed with another provider ID:   %d\n", r.ProviderMismatch)
	fmt.Printf("  # indexed with expected provider ID:  %d\n", r.Present)
	fmt.Println("--------------------------------------------")
	fmt.Printf("total Multihashes checked:              %d\n", r.TotalMhChecked)
	fmt.Println()
	fmt.Printf("sampling probability:                   %.2f\n", samplingProb)
	fmt.Printf("RNG seed:                               %d\n", rngSeed)
	fmt.Println()

	if printUnindexedMhs && len(r.AbsentMhs) != 0 {
		fmt.Println("Un-indexed Multihash(es):")
		for _, mh := range r.AbsentMhs {
			fmt.Printf("  %s\n", mh.B58String())
		}
		fmt.Println()
	}

	if r.TotalMhChecked == 0 {
		fmt.Println("⚠️ Inconclusive; no multihashes were verified.")
	} else if r.passedVerification() {
		fmt.Println("🎉 Passed verification check.")
	} else {
		fmt.Println("❌ Failed verification check.")
	}

	if len(r.Errs) != 0 {
		fmt.Println("Verification Error(s):")
		for _, err := range r.Errs {
			fmt.Printf("  %s\n", err)
		}
		fmt.Println()
	}
}

func verifyIngestFromMhs(ctx context.Context, cmd *cli.Command, find *client.Client, dhFind *client.DHashClient, wantProvID peer.ID, mhs []multihash.Multihash) (*verifyResult, error) {
	chunkSize := cmd.Int("batch-size")
	aggResult := &verifyResult{}
	for len(mhs) >= chunkSize {
		result, err := verifyIngest(ctx, find, dhFind, wantProvID, mhs[:chunkSize])
		if err != nil {
			return nil, err
		}
		aggResult.add(result)
		mhs = mhs[chunkSize:]
		os.Stdout.WriteString(".")
	}
	if len(mhs) != 0 {
		result, err := verifyIngest(ctx, find, dhFind, wantProvID, mhs)
		if err != nil {
			return nil, err
		}
		aggResult.add(result)
	}
	return aggResult, nil
}

func verifyIngest(ctx context.Context, find *client.Client, dhFind *client.DHashClient, wantProvID peer.ID, mhs []multihash.Multihash) (*verifyResult, error) {
	result := &verifyResult{
		TotalMhChecked: len(mhs),
	}

	var response *model.FindResponse
	var err error
	if dhFind != nil {
		response, err = client.FindBatch(ctx, dhFind, mhs)
		fmt.Println("🔒 Reader privacy enabled")
	} else {
		response, err = client.FindBatch(ctx, find, mhs)
	}
	if err != nil {
		result.FailedToVerify = len(mhs)
		err = fmt.Errorf("failed to connect to indexer: %w", err)
		result.Errs = append(result.Errs, err)
		return result, nil
	}

	if response == nil || len(response.MultihashResults) == 0 {
		result.Absent = len(mhs)
		return result, nil
	}

	resultsByMh := make(map[string]model.MultihashResult, len(response.MultihashResults))
	for _, mr := range response.MultihashResults {
		resultsByMh[mr.Multihash.String()] = mr
	}

	for _, mh := range mhs {
		gotResult, ok := resultsByMh[mh.String()]
		if !ok || len(gotResult.ProviderResults) == 0 {
			result.Absent++
			result.AbsentMhs = append(result.AbsentMhs, mh)
			continue
		}

		var provMatched bool
		for _, p := range gotResult.ProviderResults {
			if p.Provider.ID == wantProvID {
				result.Present++
				provMatched = true
				break
			}
		}
		if !provMatched {
			result.ProviderMismatch++
		}
	}
	return result, nil
}
