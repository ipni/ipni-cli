package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	logging "github.com/ipfs/go-log/v2"
	"github.com/ipni/ipni-cli"
	"github.com/ipni/ipni-cli/pkg/ads"
	"github.com/ipni/ipni-cli/pkg/find"
	"github.com/ipni/ipni-cli/pkg/provider"
	"github.com/ipni/ipni-cli/pkg/spaddr"
	"github.com/ipni/ipni-cli/pkg/verify"
	"github.com/urfave/cli/v2"
	"golang.org/x/mod/semver"
)

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
			versionCmd,
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var versionCmd = &cli.Command{
	Name:   "version",
	Usage:  "Show current version and check if newer version available",
	Action: versionAction,
}

func versionAction(cctx *cli.Context) error {
	const githubURL = "https://api.github.com/repos/ipni/ipni-cli/releases/latest"
	fmt.Println(cctx.App.Version)
	resp, err := http.Get(githubURL)
	if err != nil {
		return fmt.Errorf("cannot check for newer version: %s", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read newer version information: %s", err)
	}
	releaseInfo := make(map[string]any)
	if err = json.Unmarshal(body, &releaseInfo); err != nil {
		return fmt.Errorf("cannot read newer version information: %s", err)
	}
	relName, ok := releaseInfo["name"].(string)
	if !ok {
		return fmt.Errorf("version information not available from %s", githubURL)
	}
	switch semver.Compare(ipnicli.Release, relName) {
	case 0:
		fmt.Fprintln(os.Stderr, cctx.App.Name, "is up to date")
	case -1:
		fmt.Fprintln(os.Stderr, "a newer version of", cctx.App.Name, "is available:", relName)
	case 1:
		fmt.Fprintln(os.Stderr, "this is the newest version of", cctx.App.Name)
	}
	return nil
}
