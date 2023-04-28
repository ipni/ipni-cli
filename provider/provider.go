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

func main() {
	app := &cli.App{
		Name: "provider",
		Usage: "Show information about one or more providers known to an indexer.\n" +
			"Reads provider IDs from stdin if none are specified.",
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
		Usage:   "Provider's peer ID, multiple allowed. '*' lists all providers.",
		Aliases: []string{"p"},
	},
	&cli.BoolFlag{
		Name:    "id-only",
		Usage:   "Only show provider's peer ID",
		Aliases: []string{"id"},
	},
}

func providerAction(cctx *cli.Context) error {
	peerIDs := cctx.StringSlice("pid")
	if len(peerIDs) == 0 {
		// Read from stdin.
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			p := strings.TrimSpace(scanner.Text())
			if p == "" {
				// Skip empty lines.
				continue
			}
			peerIDs = append(peerIDs, p)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	uniquePeerIDs := make(map[string]struct{})
	for _, pid := range peerIDs {
		if pid == "*" {
			return listProviders(cctx)
		}
		uniquePeerIDs[pid] = struct{}{}
	}

	cl, err := client.New(cctx.String("indexer"))
	if err != nil {
		return err
	}

	var errCount int
	for pid := range uniquePeerIDs {
		err = getProvider(cctx, cl, pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting provider %s: %s\n", pid, err)
			errCount++
		}
	}

	if errCount != 0 {
		return fmt.Errorf("failed to get %d providers", errCount)
	}
	return nil
}

func getProvider(cctx *cli.Context, cl *client.Client, peerIDStr string) error {
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return err
	}
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

func listProviders(cctx *cli.Context) error {
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

	if cctx.Bool("id-only") {
		for _, pinfo := range provs {
			fmt.Println(pinfo.AddrInfo.ID)
		}
		return nil
	}

	for _, pinfo := range provs {
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
