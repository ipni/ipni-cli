package provider

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/pcache"
	"github.com/ipni/ipni-cli/pkg/dtrack"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

var ProviderCmd = &cli.Command{
	Name:  "provider",
	Usage: "Show information about providers known to an indexer",
	Description: `Get information about one or more providers from the specified indexer(s). An optional --distance flag calculates the distance from the last seen advertisement to the provider's current head advertisement.

The --invert flag inverts the selection of providers, and shows all that are not specified. This can be used to filter out provideres from the returned list.

Here is an example that shows using the output of one provider command to filter the output of another, to see which providers cid.contact knows about that dev.cid.contact does not:

    provider --all -i https://dev.cid.contact -id | provider -invert -i https://cid.contact -id
`,
	Flags:  providerFlags,
	Action: providerAction,
}

var providerFlags = []cli.Flag{
	&cli.StringSliceFlag{
		Name:     "indexer",
		Usage:    "Indexer URL. Specifying multiple results in a unified view of providers across all.",
		Aliases:  []string{"i"},
		Required: true,
	},
	&cli.StringSliceFlag{
		Name:  "pid",
		Usage: "Provider's peer ID, multiple allowed. Reads IDs from stdin if none are specified.",
	},
	&cli.BoolFlag{
		Name:    "all",
		Usage:   "Show all providers. Ignores any specified provider IDs",
		Aliases: []string{"a"},
	},
	&cli.BoolFlag{
		Name:  "count",
		Usage: "Count all providers and output only the count. Implies --all",
	},
	&cli.BoolFlag{
		Name:  "distance",
		Usage: "Calculate distance from last seen advertisement to provider's current head advertisement",
	},
	&cli.BoolFlag{
		Name:    "follow-dist",
		Aliases: []string{"fd"},
		Usage:   "Continue showing distance updates for providers",
	},
	&cli.BoolFlag{
		Name:  "error",
		Usage: "Only show providers that have a LastError. If --count then show count of providers with LastError.",
	},
	&cli.BoolFlag{
		Name:  "id-only",
		Usage: "Only show provider's peer ID",
	},
	&cli.BoolFlag{
		Name:  "invert",
		Usage: "Invert selection to show all providers except those specified. If used with --error shows those without LastError",
	},
	&cli.StringFlag{
		Name:    "update-interval",
		Aliases: []string{"uin"},
		Usage:   "Time to wait between distance update checks when using --follow-dist. The value is an integer string ending in s, m, h for seconds. minutes, hours. Updates will only be seen as fast as they become visible at the upstream location.",
		Value:   "2m",
	},
	&cli.StringFlag{
		Name:    "update-timeout",
		Aliases: []string{"uto"},
		Usage:   "Timeout for getting a provider distance, when using --follow-dist. The value is an integer string ending in s, m, h for seconds. minutes, hours.",
		Value:   "5m",
	},
	&cli.Int64Flag{
		Name:    "ad-depth-limit",
		Aliases: []string{"adl"},
		Usage:   "Limit on number of advertisements when finding distance. 0 for unlimited.",
		Value:   5000,
	},
	&cli.StringFlag{
		Name:  "topic",
		Usage: "Topic on which index advertisements are published. Only needed to get head advertisement via Graphsync with non-standard topic.",
		Value: "/indexer/ingest/mainnet",
	},
}

func providerAction(cctx *cli.Context) error {
	if cctx.Bool("count") {
		return countProviders(cctx)
	}

	if cctx.Bool("all") {
		return listProviders(cctx, nil)
	}

	pids := cctx.StringSlice("pid")
	if len(pids) == 0 {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Fprintln(os.Stderr, "Reading provider IDs from stdin. Enter one per line, or Ctrl-D to finish.")
		}
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

	peerIDs := make(map[peer.ID]struct{}, len(pids))
	for _, pid := range pids {
		peerID, err := peer.Decode(pid)
		if err != nil {
			return fmt.Errorf("invalid peer ID %s: %s", pid, err)
		}
		peerIDs[peerID] = struct{}{}
	}

	if cctx.Bool("invert") {
		return listProviders(cctx, peerIDs)
	}

	var pc *pcache.ProviderCache
	var err error
	if len(peerIDs) > 1 {
		pc, err = pcache.New(pcache.WithRefreshInterval(0),
			pcache.WithSourceURL(cctx.StringSlice("indexer")...))
	} else {
		pc, err = pcache.New(pcache.WithPreload(false), pcache.WithRefreshInterval(0),
			pcache.WithSourceURL(cctx.StringSlice("indexer")...))
	}
	if err != nil {
		return err
	}

	if cctx.Bool("follow-dist") {
		return followDistance(cctx, peerIDs, nil, pc)
	}

	var errCount int
	for peerID := range peerIDs {
		err = getProvider(cctx, pc, peerID)
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

func getProvider(cctx *cli.Context, pc *pcache.ProviderCache, peerID peer.ID) error {
	prov, err := pc.Get(cctx.Context, peerID)
	if err != nil {
		return err
	}
	if prov == nil {
		return errors.New("provider not found on indexer")
	}

	if cctx.Bool("id-only") {
		fmt.Println(prov.AddrInfo.ID)
		return nil
	}

	showProviderInfo(cctx, prov)
	return nil
}

func countProviders(cctx *cli.Context) error {
	pcache, err := pcache.New(pcache.WithRefreshInterval(0),
		pcache.WithSourceURL(cctx.StringSlice("indexer")...))
	if err != nil {
		return err
	}
	provs := pcache.List()

	if cctx.Bool("error") {
		var count int
		if cctx.Bool("invert") {
			for _, pinfo := range provs {
				if pinfo.LastError == "" {
					count++
				}
			}
		} else {
			for _, pinfo := range provs {
				if pinfo.LastError != "" {
					count++
				}
			}
		}
		fmt.Println(count)
		return nil
	}

	fmt.Println(len(provs))
	return nil
}

func listProviders(cctx *cli.Context, exclude map[peer.ID]struct{}) error {
	pc, err := pcache.New(pcache.WithSourceURL(cctx.StringSlice("indexer")...), pcache.WithRefreshInterval(0))
	if err != nil {
		return err
	}

	if cctx.Bool("follow-dist") {
		return followDistance(cctx, nil, exclude, pc)
	}

	provs := pc.List()
	if len(provs) == 0 {
		fmt.Println("No providers registered with indexer")
		return nil
	}

	var errFilter, onlyWithError bool
	if cctx.Bool("error") {
		errFilter = true
		onlyWithError = !cctx.Bool("invert")
	}

	if cctx.Bool("id-only") {
		for _, pinfo := range provs {
			if _, ok := exclude[pinfo.AddrInfo.ID]; ok {
				continue
			}
			if errFilter && (onlyWithError == (pinfo.LastError == "")) {
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
		if errFilter && (onlyWithError == (pinfo.LastError == "")) {
			continue
		}
		showProviderInfo(cctx, pinfo)
	}

	return nil
}

func followDistance(cctx *cli.Context, include, exclude map[peer.ID]struct{}, pc *pcache.ProviderCache) error {
	trackUpdateIn, err := time.ParseDuration(cctx.String("update-interval"))
	if err != nil {
		return err
	}

	var timeout time.Duration
	updateTimeout := cctx.String("update-timeout")
	if updateTimeout != "" {
		timeout, err = time.ParseDuration(updateTimeout)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "Showing provider distance updates, ctrl-c to cancel...")
	limit := cctx.Int64("ad-depth-limit")
	updates, err := dtrack.RunDistanceTracker(cctx.Context, include, exclude, pc, trackUpdateIn, timeout,
		dtrack.WithDepthLimit(limit), dtrack.WithTopic(cctx.String("topic")))
	if err != nil {
		return err
	}
	for update := range updates {
		if update.Err != nil {
			fmt.Fprintln(os.Stderr, "Provider", update.ID, "distance error:", update.Err)
			continue
		}
		var dist string
		if update.Distance == -1 {
			dist = fmt.Sprintf("exceeded limit %d+", limit)
		} else {
			dist = fmt.Sprintf("%d", update.Distance)
		}
		fmt.Println("Provider", update.ID, "distance to head advertisement:", dist)
	}
	return nil
}

func showProviderInfo(cctx *cli.Context, pinfo *model.ProviderInfo) {
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
	if adCidStr != "" && pinfo.Lag != 0 {
		fmt.Println("    Sync-in-progress lag:", pinfo.Lag)
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

	if pinfo.Inactive {
		fmt.Println("    Inactive: true")
	}

	if pinfo.LastError != "" {
		fmt.Println("    LastError:", pinfo.LastError)
		fmt.Println("    LastErrorTime:", pinfo.LastErrorTime)
	}

	if cctx.Bool("distance") {
		fmt.Print("    Distance to head advertisement: ")
		dist, _, err := getLastSeenDistance(cctx, pinfo)
		if err != nil {
			fmt.Println("error:", err)
		} else if dist == -1 {
			fmt.Printf("exceeded limit %d+", cctx.Int64("ad-depth-limit"))
		} else {
			fmt.Println(dist)
		}
	}

	fmt.Println()
}

func getLastSeenDistance(cctx *cli.Context, pinfo *model.ProviderInfo) (int, cid.Cid, error) {
	if pinfo.Publisher == nil {
		return 0, cid.Undef, errors.New("no publisher listed")
	}
	if !pinfo.LastAdvertisement.Defined() {
		return 0, cid.Undef, errors.New("no last advertisement")
	}
	adDist, err := dtrack.NewAdDistance(
		dtrack.WithDepthLimit(cctx.Int64("ad-depth-limit")),
		dtrack.WithTopic(cctx.String("topic")))
	if err != nil {
		return 0, cid.Undef, err
	}
	defer adDist.Close()

	return adDist.Get(cctx.Context, *pinfo.Publisher, pinfo.LastAdvertisement, cid.Undef)
}
