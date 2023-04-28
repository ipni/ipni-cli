package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ipni/ipni-cli/spaddr/spinfo"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:   "spaddr",
		Usage:  "Get storage provider p2p ID and address from lotus gateway",
		Flags:  spAddrFlags,
		Action: spAddrAction,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

func spAddrAction(cctx *cli.Context) error {
	gateway := cctx.String("gateway")
	if gateway == "" {
		return errors.New("lotus gateway not specified")
	}

	addrInfo, err := spinfo.SPAddrInfo(cctx.Context, gateway, cctx.String("spid"))
	if err != nil {
		return err
	}

	fmt.Println("ID:", addrInfo.ID)
	fmt.Println("Addrs:", addrInfo.Addrs)
	return nil
}
