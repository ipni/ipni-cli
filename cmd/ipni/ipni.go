package main

import (
	"fmt"
	"os"

	version "github.com/ipni/ipni-cli"
	"github.com/ipni/ipni-cli/pkg/advert"
	"github.com/ipni/ipni-cli/pkg/distance"
	"github.com/ipni/ipni-cli/pkg/find"
	"github.com/ipni/ipni-cli/pkg/provider"
	"github.com/ipni/ipni-cli/pkg/spaddr"
	"github.com/ipni/ipni-cli/pkg/verifyingest"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:    "ipni",
		Usage:   "Commands to interact with IPNI indexers and index providers",
		Version: version.Version,
		Commands: []*cli.Command{
			advert.AdvertCmd,
			distance.DistanceCmd,
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
