package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ipni/go-libipni/apierror"
	client "github.com/ipni/go-libipni/find/client/http"
	"github.com/ipni/go-libipni/find/model"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/urfave/cli/v2"
)

// ./provider --all -i dev.cid.contact -id | ./provider -v -i cid.contact -id
func main() {
	app := &cli.App{
		Name: "provider",
		Usage: "Show information about one or more providers known to an indexer.\n" +
			"Reads provider IDs from stdin if none are specified.",
		Description: `The -v flag inverts the selection of providers, and shows all that are not specified.
This can be used to filter out provideres from the returned list.

Here is an example that shows using the output of one provider command to filter the output of
another, to see which providers cid.contact knows about that dev.cid.contact does not:

    provider --all -i dev.cid.contact -id | provider -v -i cid.contact -id
`,
		Flags:  providerFlags,
		Action: providerAction,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var providerFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "indexer",
		Usage:   "Indexer URL",
		EnvVars: []string{"INDEXER"},
		Aliases: []string{"i"},
		Value:   "http://localhost:3000",
	},
	&cli.StringSliceFlag{
		Name:    "pid",
		Usage:   "Provider's peer ID, multiple allowed",
		Aliases: []string{"p"},
	},
	&cli.BoolFlag{
		Name:    "all",
		Usage:   "Show all providers. Ignores any specified provider IDs",
		Aliases: []string{"a"},
	},
	&cli.BoolFlag{
		Name:    "id-only",
		Usage:   "Only show provider's peer ID",
		Aliases: []string{"id"},
	},
	&cli.BoolFlag{
		Name:    "invert",
		Usage:   "Invert selection, show all providers except those specified",
		Aliases: []string{"v"},
	},
}

func providerAction(cctx *cli.Context) error {
	if cctx.Bool("all") {
		if cctx.Bool("invert") {
			return errors.New("cannot use --all with --invert")
		}
		return listProviders(cctx, nil)
	}

	pids := cctx.StringSlice("pid")
	if len(pids) == 0 {
		// Read from stdin.
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			p := strings.TrimSpace(scanner.Text())
			if p == "" {
				// Skip empty lines.
				continue
			}
			pids = append(pids, p)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	peerIDs := make([]peer.ID, 0, len(pids))
	seen := make(map[string]struct{}, len(pids))
	for _, pid := range pids {
		if _, ok := seen[pid]; ok {
			// Skip duplicates.
			continue
		}
		peerID, err := peer.Decode(pid)
		if err != nil {
			return fmt.Errorf("invalid peer ID %s: %s", pid, err)
		}
		peerIDs = append(peerIDs, peerID)
	}

	cl, err := client.New(cctx.String("indexer"))
	if err != nil {
		return err
	}

	if cctx.Bool("invert") {
		return listProviders(cctx, peerIDs)
	}

	var errCount int
	for _, peerID := range peerIDs {
		err = getProvider(cctx, cl, peerID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting provider %s: %s\n", peerID, err)
			errCount++
		}
	}

	if errCount != 0 {
		return fmt.Errorf("failed to get %d providers", errCount)
	}
	return nil
}

func getProvider(cctx *cli.Context, cl *client.Client, peerID peer.ID) error {
	prov, err := cl.GetProvider(cctx.Context, peerID)
	if err != nil {
		var ae *apierror.Error
		if errors.As(err, &ae) && ae.Status() == http.StatusNotFound {
			return errors.New("provider not found on indexer")
		}
		return err
	}
	if prov == nil {
		return errors.New("provider not found on indexer")
	}

	if cctx.Bool("id-only") {
		fmt.Println(prov.AddrInfo.ID)
		return nil
	}

	showProviderInfo(prov)
	return nil
}

func listProviders(cctx *cli.Context, peerIDs []peer.ID) error {
	cl, err := client.New(cctx.String("indexer"))
	if err != nil {
		return err
	}
	provs, err := cl.ListProviders(cctx.Context)
	if err != nil {
		return err
	}
	if len(provs) == 0 {
		fmt.Println("No providers registered with indexer")
		return nil
	}

	var exclude map[peer.ID]struct{}
	if len(peerIDs) != 0 {
		exclude = make(map[peer.ID]struct{}, len(peerIDs))
		for _, pid := range peerIDs {
			exclude[pid] = struct{}{}
		}
	}

	if cctx.Bool("id-only") {
		for _, pinfo := range provs {
			if _, ok := exclude[pinfo.AddrInfo.ID]; ok {
				continue
			}
			fmt.Println(pinfo.AddrInfo.ID)
		}
		return nil
	}

	for _, pinfo := range provs {
		if _, ok := exclude[pinfo.AddrInfo.ID]; ok {
			continue
		}
		showProviderInfo(pinfo)
	}

	return nil
}

func showProviderInfo(pinfo *model.ProviderInfo) {
	fmt.Println("Provider", pinfo.AddrInfo.ID)
	fmt.Println("    Addresses:", pinfo.AddrInfo.Addrs)
	var adCidStr string
	var timeStr string
	if pinfo.LastAdvertisement.Defined() {
		adCidStr = pinfo.LastAdvertisement.String()
		timeStr = pinfo.LastAdvertisementTime
	}
	fmt.Println("    LastAdvertisement:", adCidStr)
	fmt.Println("    LastAdvertisementTime:", timeStr)
	if adCidStr != "" {
		fmt.Println("    Lag:", pinfo.Lag)
	}
	if pinfo.Publisher != nil {
		fmt.Println("    Publisher:", pinfo.Publisher.ID)
		fmt.Println("        Publisher Addrs:", pinfo.Publisher.Addrs)
		if pinfo.FrozenAt.Defined() {
			fmt.Println("    FrozenAt:", pinfo.FrozenAt.String())
		}
	} else {
		fmt.Println("    Publisher: none")
	}
	// Provider is still frozen even if there is no FrozenAt CID.
	if pinfo.FrozenAtTime != "" {
		fmt.Println("    FrozenAtTime:", pinfo.FrozenAtTime)
	}
	fmt.Println("    IndexCount:", pinfo.IndexCount)
	if pinfo.Inactive {
		fmt.Println("    Inactive: true")
	}
}
