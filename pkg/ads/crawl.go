package ads

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipni/go-libipni/metadata"
	"github.com/ipni/ipni-cli/pkg/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

var adsCrawlSubCmd = &cli.Command{
	Name:  "crawl",
	Usage: "Crawl advertisements from latest to earlier from a specified publisher, printing information about each",
	Description: `Crawl an advertisement chain, stopping at a specified number of multihashes or number of advertisements.
Example Usage:

    ipni ads crawl -n 10 --ai=/ip4/38.70.220.112/tcp/10201/p2p/12D3KooWEAcRJ5fYjuavKgAhu79juR7mgaznSZxsm2RRUBiWurv9
`,
	Flags:  adsCrawlFlags,
	Action: adsCrawlAction,
}

var adsCrawlFlags = []cli.Flag{
	addrInfoFlag,
	&cli.StringFlag{
		Name:  "latest",
		Usage: "CID of latest advertisement in chain to start crawl from. If not specified, use latest advertisement in the chain",
	},
	&cli.IntFlag{
		Name:    "number",
		Usage:   "Number of advertisements to crawl. Specify 0 for all.",
		Aliases: []string{"n"},
	},
	&cli.IntFlag{
		Name:    "stop-mhs",
		Usage:   "Stop after counting total number of multihashes",
		Aliases: []string{"s"},
	},
	&cli.BoolFlag{
		Name:  "skip-entries",
		Usage: "Do not show or count entries",
	},
	&cli.BoolFlag{
		Name:  "show-metadata",
		Usage: "Show advertisement metadata",
	},
	&cli.BoolFlag{
		Name:  "show-ext-providers",
		Usage: "Show advertisement extended providers",
	},
	&cli.BoolFlag{
		Name:    "quiet",
		Usage:   "Only show advertisement ID and multihash count",
		Aliases: []string{"q"},
	},
	timeoutFlag,
	topicFlag,
}

func adsCrawlAction(cctx *cli.Context) error {
	addrInfo, err := peer.AddrInfoFromString(cctx.String("addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	provClient, err := adpub.NewClient(*addrInfo,
		adpub.WithEntriesDepthLimit(0),
		adpub.WithTopicName(cctx.String("topic")),
		adpub.WithHttpTimeout(cctx.Duration("timeout")))
	if err != nil {
		return err
	}

	var latestCid cid.Cid
	if cctx.String("latest") != "" {
		latestCid, err = cid.Decode(cctx.String("latest"))
		if err != nil {
			return fmt.Errorf("bad cid: %w", err)
		}
	}

	quiet := cctx.Bool("quiet")
	skipEntries := cctx.Bool("skip-entries")
	showMetadata := cctx.Bool("show-metadata")
	showExtProviders := cctx.Bool("show-ext-providers")
	stopMhs := cctx.Int("stop-mhs")

	if skipEntries && stopMhs != 0 {
		return errors.New("cannot use flag --skip-entries with --stop-mhs")
	}

	ctx, cancel := context.WithCancel(cctx.Context)
	defer cancel()

	ads := make(chan *adpub.Advertisement, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- provClient.Crawl(ctx, latestCid, cctx.Int("number"), ads)
		close(ads)
	}()

	var activeMhs, totalMhs int
	var removalAds, totalAds int
	removed := make(map[string]struct{})

	for ad := range ads {
		var prevCID string
		if ad.PreviousID != cid.Undef {
			prevCID = ad.PreviousID.String()
		}
		contextID := base64.StdEncoding.EncodeToString(ad.ContextID)
		if !quiet {
			fmt.Println()
			fmt.Println("ID:", ad.ID)
			fmt.Println("PreviousCID:", prevCID)
			fmt.Println("ProviderID:", ad.ProviderID)
			fmt.Println("ContextID:", contextID)
			fmt.Println("Addresses:", ad.Addresses)
			fmt.Println("Is Remove:", ad.IsRemove)
			if showMetadata {
				fmt.Print("Metadata: ")
				if len(ad.Metadata) == 0 {
					fmt.Println("none")
				} else {
					fmt.Println(base64.StdEncoding.EncodeToString(ad.Metadata))
					var mdProtos []string
					md := metadata.Default.New()
					err = md.UnmarshalBinary(ad.Metadata)
					if err == nil {
						for _, p := range md.Protocols() {
							mdProtos = append(mdProtos, p.String())
						}
					}
					if len(mdProtos) != 0 {
						fmt.Print("  Protocols: ")
						fmt.Println(strings.Join(mdProtos, " "))
					}
				}
			}
			if showExtProviders {
				fmt.Println("Extended Providers:")
				if ad.ExtendedProvider != nil {
					fmt.Printf("  Override: %v\n", ad.ExtendedProvider.Override)
					fmt.Println("  Providers:")
					if len(ad.ExtendedProvider.Providers) != 0 {
						for i, ep := range ad.ExtendedProvider.Providers {
							fmt.Printf("   %d. ID:         %v\n", i+1, ep.ID)
							fmt.Printf("       Addresses:  %v\n", ep.Addresses)
							fmt.Printf("       Metadata:   %v\n", base64.StdEncoding.EncodeToString(ep.Metadata))
						}
					} else {
						fmt.Println("     None")
					}
				} else {
					fmt.Println("  None")
				}
			}
		}
		totalAds++
		if ad.IsRemove {
			removed[contextID] = struct{}{}
			removalAds++
			continue
		}
		if !ad.HasEntries() {
			if !quiet {
				fmt.Println("No entries")
			}
			continue
		}

		_, wasRm := removed[contextID]
		if wasRm && !quiet {
			fmt.Println("Ad removed")
		}

		if skipEntries {
			continue
		}

		err = provClient.SyncEntriesWithRetry(cctx.Context, ad.Entries.Root())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync entries for advertisement %s: %s\n", ad.ID, err)
			continue
		}

		entries, err := ad.Entries.Drain()
		if err != nil {
			if !errors.Is(err, datastore.ErrNotFound) {
				return err
			}
		}
		if !wasRm {
			activeMhs += len(entries)
		}
		totalMhs += len(entries)

		if quiet {
			if wasRm {
				fmt.Println(ad.ID, "Multihashes:", len(entries), "(removed)")
			} else {
				fmt.Printf("%s Multihashes: %-15d total: %d\n", ad.ID, len(entries), totalMhs)
			}
		} else {
			fmt.Println("Entries:")
			fmt.Println("  Chunk Count:", ad.Entries.ChunkCount())
			fmt.Println("  Multihashes:", len(entries))
			fmt.Println("Active mhs:", activeMhs)
			fmt.Println("Total mhs: ", totalMhs)
		}

		if stopMhs != 0 && totalMhs >= stopMhs {
			break
		}
	}
	cancel()

	err = <-errCh
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("ads crawled:       ", totalAds)
	if totalAds == 0 {
		return nil
	}
	fmt.Printf("removal ads:        %d (%d%%)\n", removalAds, removalAds*100/totalAds)
	if !skipEntries {
		fmt.Println("active multihashes:", activeMhs)
		fmt.Println("total multihashes: ", totalMhs)
	}

	return nil
}
