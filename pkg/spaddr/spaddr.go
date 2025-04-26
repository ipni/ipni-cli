package spaddr

import (
	"context"
	"errors"
	"fmt"

	"github.com/ipni/ipni-cli/pkg/spaddr/spinfo"
	"github.com/urfave/cli/v3"
)

var SPAddrCmd = &cli.Command{
	Name:   "spaddr",
	Usage:  "Get storage provider p2p ID and address from lotus gateway",
	Flags:  spAddrFlags,
	Action: spAddrAction,
}

var spAddrFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "spid",
		Usage:    "Service Provider ID (example: t01000)",
		Aliases:  []string{"s"},
		Required: true,
	},
	&cli.StringFlag{
		Name:     "gateway",
		Usage:    "Specified lotus gateway host",
		Aliases:  []string{"g"},
		Required: false,
		Value:    "api.chain.love",
	},
}

func spAddrAction(ctx context.Context, cmd *cli.Command) error {
	gateway := cmd.String("gateway")
	if gateway == "" {
		return errors.New("lotus gateway not specified")
	}

	addrInfo, err := spinfo.SPAddrInfo(ctx, gateway, cmd.String("spid"))
	if err != nil {
		return err
	}

	fmt.Println("ID:", addrInfo.ID)
	fmt.Println("Addrs:", addrInfo.Addrs)
	return nil
}
