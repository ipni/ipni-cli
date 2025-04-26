package find

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/find/client"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/urfave/cli/v3"
)

var FindCmd = &cli.Command{
	Name:  "find",
	Usage: "Lookup storage provider data by CID or multihash at indexer",
	Description: `The find command queries an indexer, using the supplied CIDs or multihashes as lookup keys, for the storage provider data needed to retrieve the content identified by the keys.

Example usage:
	ipni find -i https://cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy`,
	Flags:  findFlags,
	Before: beforeFind,
	Action: findAction,
}

var findFlags = []cli.Flag{
	&cli.StringSliceFlag{
		Name:     "mh",
		Usage:    "Specify multihash to use as indexer key, multiple OK",
		Required: false,
	},
	&cli.StringSliceFlag{
		Name:     "cid",
		Usage:    "Specify CID to use as indexer key, multiple OK",
		Required: false,
	},
	&cli.StringSliceFlag{
		Name:    "indexer",
		Usage:   "URL of indexer to query. Multiple OK to specify providers info sources for dhstore.",
		Aliases: []string{"i"},
		Value:   []string{"https://cid.contact"},
	},
	&cli.StringFlag{
		Name:    "dhstore",
		Usage:   "URL of double-hashed (reader-private) store, if different from indexer - assumes --priv.",
		Aliases: []string{"dhs"},
	},
	&cli.BoolFlag{
		Name:  "id-only",
		Usage: "Only show provider's peer ID from each result",
	},
	&cli.BoolFlag{
		Name:  "priv",
		Usage: "Use reader-privacy for queries",
	},
	&cli.BoolFlag{
		Name:  "fallback",
		Usage: "Do non-private query only if the indexer does not support reader-privacy",
	},
}

func beforeFind(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	if len(cmd.StringSlice("indexer")) == 0 {
		if !cmd.Bool("priv") {
			return ctx, cli.Exit("missing value for --indexer", 1)
		}
		if cmd.String("dhstore") == "" {
			return ctx, cli.Exit("missing value for --dhstore and --indexer", 1)
		}
	}
	if cmd.String("dhstore") != "" {
		cmd.Set("priv", "true")
	}

	return ctx, nil
}

func findAction(ctx context.Context, cmd *cli.Command) error {
	mhArgs := cmd.StringSlice("mh")
	cidArgs := cmd.StringSlice("cid")
	if len(mhArgs) == 0 && len(cidArgs) == 0 {
		return fmt.Errorf("must specify at least one multihash or CID")
	}

	mhs := make([]multihash.Multihash, 0, len(mhArgs)+len(cidArgs))
	for i := range mhArgs {
		m, err := multihash.FromB58String(mhArgs[i])
		if err != nil {
			return err
		}
		mhs = append(mhs, m)
	}
	for i := range cidArgs {
		c, err := cid.Decode(cidArgs[i])
		if err != nil {
			return err
		}
		mhs = append(mhs, c.Hash())
	}

	if cmd.Bool("priv") {
		return dhFind(ctx, cmd, mhs)
	}
	return clearFind(ctx, cmd, mhs)
}

func dhFind(ctx context.Context, cmd *cli.Command, mhs []multihash.Multihash) error {
	cl, err := client.NewDHashClient(
		client.WithProvidersURL(cmd.StringSlice("indexer")...),
		client.WithDHStoreURL(cmd.String("dhstore")),
		client.WithPcacheTTL(0),
	)
	if err != nil {
		return err
	}

	resp, err := client.FindBatch(ctx, cl, mhs)
	if err != nil {
		return err
	}
	if resp == nil && cmd.Bool("fallback") {
		return clearFind(ctx, cmd, mhs)
	}
	fmt.Println("🔒 Reader privacy enabled")
	return printResults(cmd, resp)
}

func clearFind(ctx context.Context, cmd *cli.Command, mhs []multihash.Multihash) error {
	idxr := cmd.String("dhstore")
	if idxr == "" {
		idxr = cmd.StringSlice("indexer")[0]
	}
	cl, err := client.New(idxr)
	if err != nil {
		return err
	}

	resp, err := client.FindBatch(ctx, cl, mhs)
	if err != nil {
		return err
	}
	return printResults(cmd, resp)
}

func printResults(cmd *cli.Command, resp *model.FindResponse) error {
	if resp == nil || len(resp.MultihashResults) == 0 {
		fmt.Println("index not found")
		return nil
	}

	if cmd.Bool("id-only") {
		seen := make(map[peer.ID]struct{})
		for i := range resp.MultihashResults {
			for _, pr := range resp.MultihashResults[i].ProviderResults {
				if _, ok := seen[pr.Provider.ID]; ok {
					continue
				}
				seen[pr.Provider.ID] = struct{}{}
				fmt.Println(pr.Provider.ID.String())
			}
		}
		return nil
	}

	for i := range resp.MultihashResults {
		fmt.Println("Multihash:", resp.MultihashResults[i].Multihash.B58String())
		if len(resp.MultihashResults[i].ProviderResults) == 0 {
			fmt.Println("  index not found")
			continue
		}
		// Group results by provider.
		providers := make(map[string][]model.ProviderResult)
		for _, pr := range resp.MultihashResults[i].ProviderResults {
			provStr := pr.Provider.String()
			providers[provStr] = append(providers[provStr], pr)
		}
		for provStr, prs := range providers {
			fmt.Println("  Provider:", provStr)
			for _, pr := range prs {
				fmt.Println("    ContextID:", base64.StdEncoding.EncodeToString(pr.ContextID))
				fmt.Print("      Metadata: ")
				if len(pr.Metadata) == 0 {
					fmt.Println("none")
				} else {
					fmt.Println(base64.StdEncoding.EncodeToString(pr.Metadata))
					fmt.Println("        Protocols:", decodeMetadataProtos(pr.Metadata))
				}
			}
		}
	}
	return nil
}

func decodeMetadataProtos(metaBytes []byte) string {
	meta := metadata.Default.New()
	err := meta.UnmarshalBinary(metaBytes)
	if err != nil {
		return fmt.Sprint("error: ", err.Error())
	}
	protoStrs := make([]string, meta.Len())
	for i, p := range meta.Protocols() {
		protoStrs[i] = p.String()
	}
	return strings.Join(protoStrs, ", ")
}
