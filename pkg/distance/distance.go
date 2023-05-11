package distance

import (
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/ipni/ipni-cli/pkg/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

var DistanceCmd = &cli.Command{
	Name:        "distance",
	Usage:       "Determine the distance between two advertisements in a chain",
	Description: "Sepcify the oldest and newest advertisement CIDs. If newest is not specified use the latest advertisement",
	Flags:       distanceFlags,
	Action:      distanceAction,
}

var distanceFlags = []cli.Flag{
	&cli.StringFlag{
		Name: "addr-info",
		Usage: "Publisher's address info in form of libp2p multiaddr info.\n" +
			"Example GraphSync: /ip4/1.2.3.4/tcp/1234/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ\n" +
			"Example HTTP:      /ip4/1.2.3.4/tcp/1234/http/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ",
		Aliases:  []string{"ai"},
		Required: true,
	},
	&cli.StringFlag{
		Name:     "old",
		Usage:    "CID of earliest advertisement in chain",
		Required: true,
	},
	&cli.StringFlag{
		Name:  "new",
		Usage: "CID of latest advertisement in chain. If not specified, use the latest advertisement",
	},
	&cli.BoolFlag{
		Name:    "quiet",
		Usage:   "Output only the distance",
		Aliases: []string{"q"},
	},
	&cli.StringFlag{
		Name:    "topic",
		Usage:   "Topic on which index advertisements are published. Only needed if connecting via Graphsync with non-standard topic.",
		Value:   "/indexer/ingest/mainnet",
		Aliases: []string{"t"},
	},
}

func distanceAction(cctx *cli.Context) error {
	addrInfo, err := peer.AddrInfoFromString(cctx.String("addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	oldCid, err := cid.Decode(cctx.String("old"))
	if err != nil {
		return fmt.Errorf("bad old cid: %w", err)
	}
	provClient, err := adpub.NewClient(*addrInfo, adpub.WithTopicName(cctx.String("topic")))
	if err != nil {
		return err
	}

	var newStr string
	var newCid cid.Cid
	if cctx.String("new") != "" {
		newCid, err := cid.Decode(cctx.String("new"))
		if err != nil {
			return fmt.Errorf("bad new for: %w", err)
		}
		newStr = newCid.String()
	} else {
		newStr = "head"
	}

	adCount, err := provClient.Distance(cctx.Context, oldCid, newCid)
	if err != nil {
		return err
	}

	if cctx.Bool("quiet") {
		fmt.Println(adCount)
	} else {
		fmt.Printf("Distance from %s to %s is %d\n", oldCid, newStr, adCount)
	}
	return nil
}
