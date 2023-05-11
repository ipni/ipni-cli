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
	Description: "Sepcify the start and optional end advertisement CIDs. If end CID is not specified use the latest advertisement",
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
		Name:     "start",
		Usage:    "CID of earliest advertisement in chain",
		Required: true,
	},
	&cli.StringFlag{
		Name:  "end",
		Usage: "CID of advertisement later in chain. If not specified, use the latest advertisement",
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

	startCid, err := cid.Decode(cctx.String("start"))
	if err != nil {
		return fmt.Errorf("bad start cid: %w", err)
	}
	provClient, err := adpub.NewClient(*addrInfo, adpub.WithTopicName(cctx.String("topic")))
	if err != nil {
		return err
	}

	var endStr string
	var endCid cid.Cid
	if cctx.String("end") != "" {
		endCid, err = cid.Decode(cctx.String("end"))
		if err != nil {
			return fmt.Errorf("bad end cid: %w", err)
		}
		endStr = endCid.String()
	} else {
		endStr = "head"
	}

	adCount, err := provClient.Distance(cctx.Context, startCid, endCid)
	if err != nil {
		return err
	}

	if cctx.Bool("quiet") {
		fmt.Println(adCount)
	} else {
		fmt.Printf("Distance from %s to %s is %d\n", startCid, endStr, adCount)
	}
	return nil
}
