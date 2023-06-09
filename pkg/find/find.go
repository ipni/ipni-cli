package find

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/find/client"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/urfave/cli/v2"
)

var FindCmd = &cli.Command{
	Name:  "find",
	Usage: "Lookup storage provider data by CID or multihash at indexer",
	Description: `The find command queries an indexer, using the supplied CIDs or multihashes as lookup keys, for the storage provider data needed to retrieve the content identified by the keys.

Example usage:
	ipni find -i cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy`,
	Flags:  findFlags,
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
	&cli.StringFlag{
		Name:    "indexer",
		Usage:   "URL of indexer to query",
		EnvVars: []string{"INDEXER"},
		Aliases: []string{"i"},
		Value:   "http://localhost:3000",
	},
	&cli.BoolFlag{
		Name:  "id-only",
		Usage: "Only show provider's peer ID from each result",
	},
}

func findAction(cctx *cli.Context) error {
	mhArgs := cctx.StringSlice("mh")
	cidArgs := cctx.StringSlice("cid")
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

	cl, err := client.New(cctx.String("indexer"))
	if err != nil {
		return err
	}

	var resp *model.FindResponse
	if len(mhs) == 1 {
		resp, err = cl.Find(cctx.Context, mhs[0])
	} else {
		resp, err = cl.FindBatch(cctx.Context, mhs)
	}
	if err != nil {
		return err
	}

	if len(resp.MultihashResults) == 0 {
		fmt.Println("index not found")
		return nil
	}

	if cctx.Bool("id-only") {
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
				fmt.Println("      Metadata:", decodeMetadata(pr.Metadata))
			}
		}
	}
	return nil
}

func decodeMetadata(metaBytes []byte) string {
	if len(metaBytes) == 0 {
		return "nil"
	}
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
