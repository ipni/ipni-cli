package advert

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipni/ipni-cli/internal/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

var AdvertCmd = &cli.Command{
	Name:  "advert",
	Usage: "Show information about an advertisement from a specified publisher",
	ArgsUsage: "The publisher's endpoint address in form of libp2p multiaddr info.\n" +
		"       Example GraphSync: /ip4/1.2.3.4/tcp/1234/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ\n" +
		"       Example HTTP:      /ip4/1.2.3.4/tcp/1234/http/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ",
	Description: `Advertisement CIDs may be specified using the -cid flag, or --head to get the latest advertisement.
Multiple CIDs may be specified to fetch multiple advertisements. Example Usage:

    advert
        -cid baguqeeradjagxlgpsy3xn2jrx52us5tl3mp5n5kq6kkg2ul3i6xzyrujbhbq \
        -cid baguqeerazru3iegjkmjj45xfrheasxfxm4vwxotydl6mpt52zvnv5rx42ssq \
        /ip4/212.248.62.42/tcp/17162/p2p/12D3KooWCYL6mn3p7W5WoaC3yYfbsovaDkQcpyxMFG9PtJUmSjzF

If no CID are specidied then CIDs are read from stdin, one per line.

    cat cids.txt | advert /ip4/212.248.62.42/tcp/17162/p2p/12D3KooWCYL6mn3p7W5WoaC3yYfbsovaDkQcpyxMFG9PtJUmSjzF
`,
	Flags:  advertFlags,
	Before: beforeAdvert,
	Action: advertAction,
}

var advertFlags = []cli.Flag{
	&cli.StringSliceFlag{
		Name:     "cid",
		Usage:    "Specify advertisement CID to fetch, multiple OK",
		Required: false,
	},
	&cli.StringFlag{
		Name:    "topic",
		Usage:   "Topic on which index advertisements are published. Only needed if connecting to provider via Graphsync endpoint.",
		Value:   "/indexer/ingest/mainnet",
		Aliases: []string{"t"},
	},
	&cli.BoolFlag{
		Name:  "head",
		Usage: "Fetch the latest advertisement from the publisher",
	},
	&cli.BoolFlag{
		Name:    "print-entries",
		Usage:   "Whether to print the list of entries in advertisement",
		Aliases: []string{"e"},
	},
	&cli.Int64Flag{
		Name:        "entries-depth-limit",
		Aliases:     []string{"edl"},
		Usage:       "Maximum depth (number of blocks of multihashes) to fetch from advertisement entries chains.",
		Value:       100,
		DefaultText: "100 (set to '0' for unlimited)",
	},
}

func beforeAdvert(cctx *cli.Context) error {
	if !cctx.Args().Present() {
		return cli.Exit("The advertisement publisher address info must be specified as argument.", 1)
	}
	return nil
}

func advertAction(cctx *cli.Context) error {
	addrInfo, err := peer.AddrInfoFromString(cctx.Args().First())
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	var adCids []cid.Cid
	cidArgs := cctx.StringSlice("cid")
	if len(cidArgs) != 0 {
		seen := make(map[string]struct{}, len(cidArgs))
		adCids = make([]cid.Cid, 0, len(cidArgs))
		for _, cidStr := range cidArgs {
			if _, ok := seen[cidStr]; ok {
				// Skip duplicate CIDs.
				continue
			}
			cid, err := cid.Decode(cidStr)
			if err != nil {
				return fmt.Errorf("bad advertisement CID arqument: %w", err)
			}
			adCids = append(adCids, cid)
		}
	}
	if cctx.Bool("head") {
		// Fetch latest advertisement
		adCids = append(adCids, cid.Undef)
	}

	// If no CIDs specified, read from stdin.
	if len(adCids) == 0 {
		seen := make(map[string]struct{})
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			cidStr := strings.TrimSpace(scanner.Text())
			if cidStr == "" {
				// Skip empty lines.
				continue
			}
			if _, ok := seen[cidStr]; ok {
				// Skip duplicate CIDs.
				continue
			}
			cid, err := cid.Decode(cidStr)
			if err != nil {
				return fmt.Errorf("bad advertisement CID arqument: %w", err)
			}
			adCids = append(adCids, cid)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	provClient, err := adpub.MakeClient(*addrInfo, cctx.String("topic"), cctx.Int64("entries-depth-limit"))

	for _, adCid := range adCids {
		ad, err := provClient.GetAdvertisement(cctx.Context, adCid)
		if err != nil {
			if ad == nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "⚠️ Failed to fully sync advertisement %s. Output shows partially synced ad.\n  Error: %s\n", adCid, err.Error())
		}

		fmt.Printf("CID:          %s\n", ad.ID)
		fmt.Printf("PreviousCID:  %s\n", ad.PreviousID)
		fmt.Printf("ProviderID:   %s\n", ad.ProviderID)
		fmt.Printf("ContextID:    %s\n", base64.StdEncoding.EncodeToString(ad.ContextID))
		fmt.Printf("Addresses:    %v\n", ad.Addresses)
		fmt.Printf("Is Remove:    %v\n", ad.IsRemove)
		fmt.Printf("Metadata :    %s\n", base64.StdEncoding.EncodeToString(ad.Metadata))

		fmt.Println("Extended Providers:")
		if ad.ExtendedProvider != nil {
			fmt.Printf("   Override: %v\n", ad.ExtendedProvider.Override)
			fmt.Println("   Providers:")
			if len(ad.ExtendedProvider.Providers) != 0 {
				for i, ep := range ad.ExtendedProvider.Providers {
					fmt.Printf("    %d. ID:         %v\n", i+1, ep.ID)
					fmt.Printf("        Addresses:  %v\n", ep.Addresses)
					fmt.Printf("        Metadata:   %v\n", base64.StdEncoding.EncodeToString(ep.Metadata))
				}
			} else {
				fmt.Println("      None")
			}
		} else {
			fmt.Println("   None")
		}

		if ad.IsRemove {
			if ad.HasEntries() {
				fmt.Println("Entries: sync skipped")
				fmt.Printf("  ⚠️ Removal advertisement with non-empty entries root cid: %s\n", ad.Entries.Root())
			} else {
				fmt.Println("Entries: None")
			}
			return nil
		}
		fmt.Println("Entries:")
		var entriesOutput string
		entries, err := ad.Entries.Drain()
		if err != nil {
			if !errors.Is(err, datastore.ErrNotFound) {
				return err
			}
			entriesOutput = "⚠️ Note: More entries were available but not synced due to the configured entries recursion limit or error during traversal."
		}

		if cctx.Bool("print-entries") {
			for _, mh := range entries {
				fmt.Printf("  %s\n", mh.B58String())
			}
			fmt.Println("  ---------------------")
		}
		fmt.Printf("  Chunk Count: %d\n", ad.Entries.ChunkCount())
		fmt.Printf("  Total Count: %d\n", len(entries))
		if entriesOutput != "" {
			fmt.Println(entriesOutput)
		}
		fmt.Println()
	}
	return nil
}
