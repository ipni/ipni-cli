package main

import (
	"fmt"
	"os"

	logging "github.com/ipfs/go-log/v2"
	"github.com/ipni/ipni-cli"
	"github.com/ipni/ipni-cli/pkg/ads"
	"github.com/ipni/ipni-cli/pkg/find"
	"github.com/ipni/ipni-cli/pkg/provider"
	"github.com/ipni/ipni-cli/pkg/spaddr"
	"github.com/ipni/ipni-cli/pkg/verify"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("ipni-cli")

func main() {
	// Disable logging that happens in packages such as data-transfer.
	_ = logging.SetLogLevel("*", "fatal")

	app := &cli.App{
		Name:    "ipni",
		Usage:   "Commands to interact with IPNI indexers and index providers",
		Version: ipnicli.Version,
		Commands: []*cli.Command{
			ads.AdsCmd,
			find.FindCmd,
			provider.ProviderCmd,
			spaddr.SPAddrCmd,
			verify.VerifyCmd,
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
