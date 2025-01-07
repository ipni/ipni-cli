package ads

import (
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/ipni/ipni-cli/pkg/dtrack"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v3"
)

var adsDistSubCmd = &cli.Command{
	Name:        "dist",
	Usage:       "Determine the distance between two advertisements in a chain",
	Description: "Sepcify the start and optional end advertisement CIDs. If end CID is not specified use the latest advertisement",
	Flags:       adsDistFlags,
	Action:      adsDistAction,
}

var adsDistFlags = []cli.Flag{
	addrInfoFlag,
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
	&cli.Int64Flag{
		Name:    "dist-limit",
		Usage:   "Limit the amount of distance to traverse",
		Aliases: []string{"dl"},
		Value:   5000,
	},
}

func adsDistAction(ctx context.Context, cmd *cli.Command) error {
	addrInfo, err := peer.AddrInfoFromString(cmd.String("addr-info"))
	if err != nil {
		return fmt.Errorf("bad pub-addr-info: %w", err)
	}

	startCid, err := cid.Decode(cmd.String("start"))
	if err != nil {
		return fmt.Errorf("bad start cid: %w", err)
	}

	adDist, err := dtrack.NewAdDistance(dtrack.WithDepthLimit(cmd.Int64("dist-limit")))
	if err != nil {
		return err
	}

	var endStr string
	var endCid cid.Cid
	if cmd.String("end") != "" {
		endCid, err = cid.Decode(cmd.String("end"))
		if err != nil {
			return fmt.Errorf("bad end cid: %w", err)
		}
		endStr = endCid.String()
	} else {
		endStr = "head"
	}

	adCount, _, err := adDist.Get(ctx, *addrInfo, startCid, endCid)
	if err != nil {
		return err
	}

	if cmd.Bool("quiet") {
		fmt.Println(adCount)
	} else {
		fmt.Printf("Distance from %s to %s is %d\n", startCid, endStr, adCount)
	}
	return nil
}
