package main

import (
	"fmt"
	"os"

	"github.com/ipni/ipni-cli/advert"
	"github.com/ipni/ipni-cli/find"
	"github.com/ipni/ipni-cli/provider"
	"github.com/ipni/ipni-cli/spaddr"
	"github.com/ipni/ipni-cli/verifyingest"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:    "ipni",
		Usage:   "Commands to interact with IPNI indexers and index providers",
		Version: version,
		Commands: []*cli.Command{
			advert.AdvertCmd,
			find.FindCmd,
			provider.ProviderCmd,
			spaddr.SPAddrCmd,
			verifyingest.VerifyIngestCmd,
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
