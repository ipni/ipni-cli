package ads

import (
	"bufio"
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
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

var adsGetSubCmd = &cli.Command{
	Name:  "get",
	Usage: "Show information about an advertisement from a specified publisher",
	Description: `Advertisement CIDs may be specified using the -cid flag, or --head to get the latest advertisement.
Multiple CIDs may be specified to fetch multiple advertisements. Example Usage:

    ipni ads get \
		-ai /dns4/sp.example.com/tcp/17162/p2p/12D3KooWLjeDyvuv7rbfG2wWNvWn7ybmmU88PirmSckuqCgXBAph \
        -cid baguqeeradjagxlgpsy3xn2jrx52us5tl3mp5n5kq6kkg2ul3i6xzyrujbhbq \
        -cid baguqeerazru3iegjkmjj45xfrheasxfxm4vwxotydl6mpt52zvnv5rx42ssq

If no CIDs are specified then CIDs are read from stdin, one per line.

    cat cids.txt | ipni ads get -ai /dns4/sp.example.com/tcp/17162/p2p/12D3KooWLjeDyvuv7rbfG2wWNvWn7ybmmU88PirmSckuqCgXBAph
`,
	Flags:  adsGetFlags,
	Action: adsGetAction,
}

var adsGetFlags = []cli.Flag{
	addrInfoFlag,
	&cli.StringSliceFlag{
		Name:     "cid",
		Usage:    "Specify advertisement CID to fetch, multiple OK",
		Required: false,
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
	&cli.BoolFlag{
		Name:    "skip-entries",
		Usage:   "Skip syncing entries",
		Aliases: []string{"se"},
	},
	&cli.Int64Flag{
		Name:        "entries-depth-limit",
		Aliases:     []string{"edl"},
		Usage:       "Maximum depth (number of blocks of multihashes) to fetch from advertisement entries chains.",
		Value:       100,
		DefaultText: "100 (set to '0' for unlimited)",
	},
	timeoutFlag,
	topicFlag,
}

func adsGetAction(ctx context.Context, cmd *cli.Command) error {
	addrInfo, err := peer.AddrInfoFromString(cmd.String("addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	var adCids []cid.Cid
	cidArgs := cmd.StringSlice("cid")
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
				return fmt.Errorf("bad advertisement CID: %w", err)
			}
			adCids = append(adCids, cid)
		}
	}
	if cmd.Bool("head") {
		// Fetch latest advertisement
		adCids = append(adCids, cid.Undef)
	}

	// If no CIDs specified, read from stdin.
	if len(adCids) == 0 {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Fprintln(os.Stderr, "Reading advertisement CIDs from stdin. Enter one per line, or Ctrl-D to finish.")
		}
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
				return fmt.Errorf("bad advertisement CID: %w", err)
			}
			adCids = append(adCids, cid)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	pubClient, err := adpub.NewClient(*addrInfo,
		adpub.WithTopicName(cmd.String("topic")),
		adpub.WithEntriesDepthLimit(cmd.Int64("entries-depth-limit")),
		adpub.WithHttpTimeout(cmd.Duration("timeout")))
	if err != nil {
		return err
	}

	for _, adCid := range adCids {
		fmt.Println()

		ad, err := pubClient.GetAdvertisement(ctx, adCid)
		if err != nil {
			if ad == nil {
				if errors.Is(err, adpub.ErrContentNotFound) {
					err = errors.New("advertisement not found at publisher")
				}
				return err
			}
			fmt.Fprintf(os.Stderr, "⚠️  Failed to fully sync advertisement %s. Output shows partially synced ad.\n  Error: %s\n", adCid, err.Error())
		}

		fmt.Println("CID:", ad.ID)
		var prevCID string
		if ad.PreviousID != cid.Undef {
			prevCID = ad.PreviousID.String()
		}

		fmt.Println("PreviousCID:", prevCID)
		fmt.Println("ProviderID:", ad.ProviderID)
		fmt.Println("ContextID:", base64.StdEncoding.EncodeToString(ad.ContextID))
		fmt.Println("Addresses:", ad.Addresses)
		fmt.Println("Is Remove:", ad.IsRemove)
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
		fmt.Print("Signature: ")
		if ad.SigErr != nil {
			fmt.Println("❌ invalid:", ad.SigErr)
		} else {
			fmt.Println("✅ valid")
			fmt.Print("Signed by: ")
			switch ad.SignerID {
			case ad.ProviderID:
				fmt.Println("content provider")
			case addrInfo.ID:
				fmt.Println("advertisement publisher")
			default:
				fmt.Println("⚠️  Unknown:", ad.SignerID)
			}
		}

		if ad.IsRemove {
			if ad.HasEntries() {
				fmt.Println("Entries: sync skipped")
				fmt.Printf("  ⚠️  Removal advertisement with non-empty entries root cid: %s\n", ad.Entries.Root())
			} else {
				fmt.Println("Entries: None")
			}
			continue
		}

		if !ad.HasEntries() {
			fmt.Println("No entries")
			continue
		}

		if cmd.Bool("skip-entries") {
			continue
		}

		// Sync entries if not a removal advertisement and has entries.
		err = pubClient.SyncEntriesWithRetry(ctx, ad.Entries.Root())
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Failed to sync entries for advertisement %s: %s\n", ad.ID, err)
			continue
		}

		fmt.Println("Entries:")
		var entriesOutput string
		entries, err := ad.Entries.Drain()
		if err != nil {
			if !errors.Is(err, datastore.ErrNotFound) {
				return err
			}
			entriesOutput = "⚠️  Note: More entries were available but not synced due to the configured entries recursion limit or error during traversal."
		}

		if cmd.Bool("print-entries") {
			for _, mh := range entries {
				fmt.Printf("  %s\n", mh.B58String())
			}
			fmt.Println("  ---------------------")
		}
		fmt.Printf("  Chunk Count: %d\n", ad.Entries.ChunkCount())
		fmt.Printf("  Multihashes: %d\n", len(entries))
		if entriesOutput != "" {
			fmt.Println(entriesOutput)
		}
	}
	return nil
}
