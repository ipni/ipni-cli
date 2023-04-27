package command

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipni/ipni-cli/command/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

var AdInfoCmd = &cli.Command{
	Name:        "ad-info",
	Usage:       "Show information about an advertisement from a specified publisher",
	ArgsUsage:   "[ad-cid]",
	Description: "Advertisement CID may optionally be specified as the first argument. If not specified the latest advertisement is used.",
	Before:      beforeGetAdvertisement,
	Action:      doGetAdvertisement,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "pub-addr-info",
			Usage: "The publisher's endpoint address in form of libp2p multiaddr info.\n" +
				"Example GraphSync: /ip4/1.2.3.4/tcp/1234/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ\n" +
				"Example HTTP:      /ip4/1.2.3.4/tcp/1234/http/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ",
			Aliases:  []string{"p"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "topic",
			Usage:   "Topic on which index advertisements are published. Only needed if connecting to provider via Graphsync endpoint.",
			Value:   "/indexer/ingest/mainnet",
			Aliases: []string{"t"},
		},
		&cli.BoolFlag{
			Name:    "print-entries",
			Usage:   "Whether to print the list of entries in advertisement",
			Aliases: []string{"e"},
		},
		adEntriesDepthLimitFlag,
	},
}

func beforeGetAdvertisement(cctx *cli.Context) error {
	if cctx.NArg() > 1 {
		return cli.Exit("At most one argument [ad-cid] must be specified. If none is specified, the current head advertisement is fetched.", 1)
	}
	return nil
}

func doGetAdvertisement(cctx *cli.Context) error {
	adCid := cid.Undef
	if cctx.Args().Present() {
		var err error
		adCid, err = cid.Decode(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("bad advertisement CID arqument: %w", err)
		}
	}

	addrInfo, err := peer.AddrInfoFromString(cctx.String("pub-addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}
	provClient, err := adpub.MakeClient(*addrInfo, cctx.String("topic"), cctx.Int64("ad-entries-depth-limit"))
	ad, err := provClient.GetAdvertisement(cctx.Context, adCid)
	if err != nil {
		if ad == nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "⚠️ Failed to fully sync advertisement %s. Output shows partially synced ad.\n  Error: %s\n", adCid, err.Error())
	}

	fmt.Printf("ID:           %s\n", ad.ID)
	fmt.Printf("PreviousID:   %s\n", ad.PreviousID)
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
	return nil
}
