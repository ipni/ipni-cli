package ads

import (
	"fmt"
	"os"

	"github.com/ipfs/go-cid"
	"github.com/ipni/ipni-cli/pkg/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

var adsListSubCmd = &cli.Command{
	Name:  "list",
	Usage: "List advertisements from latest to earlier from a specified publisher",
	Description: `Sepcify an optional latest advertisement CID, and list the requested number of advertisements from latest to earliest.
Example Usage:

    ipni ads list -n 10 --ai=/ip4/38.70.220.112/tcp/10201/p2p/12D3KooWEAcRJ5fYjuavKgAhu79juR7mgaznSZxsm2RRUBiWurv9
`,
	Flags:  adsListFlags,
	Action: adsListAction,
}

var adsListFlags = []cli.Flag{
	addrInfoFlag,
	&cli.StringFlag{
		Name:  "latest",
		Usage: "CID of latest advertisement in chain to start list from. If not specified, use latest advertisement in the chain",
	},
	&cli.IntFlag{
		Name:     "number",
		Usage:    "Number of advertisements to list. Specify 0 for all.",
		Aliases:  []string{"n"},
		Required: true,
	},
	timeoutFlag,
	topicFlag,
}

func adsListAction(cctx *cli.Context) error {
	addrInfo, err := peer.AddrInfoFromString(cctx.String("addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	provClient, err := adpub.NewClient(*addrInfo,
		adpub.WithDeleteAfterRead(true),
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

	return provClient.List(cctx.Context, latestCid, cctx.Int("number"), os.Stdout)
}
