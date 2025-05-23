package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/maurl"
	"github.com/ipni/go-libipni/mautil"
	"github.com/ipni/go-libipni/pcache"
	"github.com/ipni/ipni-cli/pkg/dtrack"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2phttp "github.com/libp2p/go-libp2p/p2p/http"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

const filfoxPeerAPI = "https://filfox.info/api/v1/peer"

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
		Name:    "indexer",
		Usage:   "Indexer URL. Specifying multiple results in a unified view of providers across all.",
		Aliases: []string{"i"},
		Value:   []string{"https://cid.contact"},
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
		Name:    "distance",
		Usage:   "Calculate distance from last seen advertisement to provider's current head advertisement",
		Aliases: []string{"dist"},
	},
	&cli.BoolFlag{
		Name:  "diff-pub",
		Usage: "Only show providers whose publisher ID is different from the provider ID.",
	},
	&cli.BoolFlag{
		Name:  "error",
		Usage: "Only show providers that have a LastError. If --count then show count of providers with LastError.",
	},
	&cli.BoolFlag{
		Name:    "follow-dist",
		Aliases: []string{"fd"},
		Usage:   "Continue showing distance updates for providers",
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
	&cli.BoolFlag{
		Name:    "protocol",
		Aliases: []string{"proto"},
		Usage:   "Print publisher's protocol used to publish advertisements.",
	},
	&cli.BoolFlag{
		Name:    "publisher",
		Aliases: []string{"pub"},
		Usage:   "Only print publisher address info.",
	},
	&cli.BoolFlag{
		Name:  "spid",
		Usage: "Print the provider's Filecoin storage provider ID. Optionally usable with --id-only.",
	},
	&cli.StringFlag{
		Name:  "topic",
		Usage: "Topic on which index advertisements are published. Only needed to get head advertisement via Graphsync with non-standard topic.",
		Value: "/indexer/ingest/mainnet",
	},
}

func providerAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("count") {
		return countProviders(cmd)
	}

	if cmd.Bool("all") {
		return listProviders(ctx, cmd, nil)
	}

	pids := cmd.StringSlice("pid")
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

	if cmd.Bool("invert") {
		return listProviders(ctx, cmd, peerIDs)
	}

	var pc *pcache.ProviderCache
	var err error
	if len(peerIDs) > 1 {
		pc, err = pcache.New(pcache.WithRefreshInterval(0),
			pcache.WithSourceURL(cmd.StringSlice("indexer")...))
	} else {
		pc, err = pcache.New(pcache.WithPreload(false), pcache.WithRefreshInterval(0),
			pcache.WithSourceURL(cmd.StringSlice("indexer")...))
	}
	if err != nil {
		return err
	}

	if cmd.Bool("follow-dist") {
		return followDistance(ctx, cmd, peerIDs, nil, pc)
	}

	var errCount int
	for peerID := range peerIDs {
		err = getProvider(ctx, cmd, pc, peerID)
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

func getProvider(ctx context.Context, cmd *cli.Command, pc *pcache.ProviderCache, peerID peer.ID) error {
	prov, err := pc.Get(ctx, peerID)
	if err != nil {
		return err
	}
	if prov == nil {
		return errors.New("provider not found on indexer")
	}

	if cmd.Bool("diff-pub") && prov.AddrInfo.ID == prov.Publisher.ID {
		return nil
	}

	showProviderInfo(ctx, cmd, prov)
	return nil
}

func countProviders(cmd *cli.Command) error {
	pcache, err := pcache.New(pcache.WithRefreshInterval(0),
		pcache.WithSourceURL(cmd.StringSlice("indexer")...))
	if err != nil {
		return err
	}
	provs := pcache.List()

	if cmd.Bool("error") {
		var count int
		if cmd.Bool("invert") {
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

func listProviders(ctx context.Context, cmd *cli.Command, exclude map[peer.ID]struct{}) error {
	pc, err := pcache.New(pcache.WithSourceURL(cmd.StringSlice("indexer")...), pcache.WithRefreshInterval(0))
	if err != nil {
		return err
	}

	if cmd.Bool("follow-dist") {
		return followDistance(ctx, cmd, nil, exclude, pc)
	}

	provs := pc.List()
	if len(provs) == 0 {
		fmt.Println("No providers registered with indexer")
		return nil
	}

	var errFilter, onlyWithError bool
	if cmd.Bool("error") {
		errFilter = true
		onlyWithError = !cmd.Bool("invert")
	}

	diffPub := cmd.Bool("diff-pub")

	for _, pinfo := range provs {
		if _, ok := exclude[pinfo.AddrInfo.ID]; ok {
			continue
		}
		if errFilter && (onlyWithError == (pinfo.LastError == "")) {
			continue
		}
		if diffPub && pinfo.AddrInfo.ID == pinfo.Publisher.ID {
			continue
		}
		showProviderInfo(ctx, cmd, pinfo)
	}

	return nil
}

func followDistance(ctx context.Context, cmd *cli.Command, include, exclude map[peer.ID]struct{}, pc *pcache.ProviderCache) error {
	trackUpdateIn, err := time.ParseDuration(cmd.String("update-interval"))
	if err != nil {
		return err
	}

	var timeout time.Duration
	updateTimeout := cmd.String("update-timeout")
	if updateTimeout != "" {
		timeout, err = time.ParseDuration(updateTimeout)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "Showing provider distance updates, ctrl-c to cancel...")
	limit := cmd.Int64("ad-depth-limit")
	updates, err := dtrack.RunDistanceTracker(ctx, include, exclude, pc, trackUpdateIn, timeout,
		dtrack.WithDepthLimit(limit), dtrack.WithTopic(cmd.String("topic")))
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

func showProviderInfo(ctx context.Context, cmd *cli.Command, pinfo *model.ProviderInfo) {
	if cmd.Bool("id-only") {
		if cmd.Bool("spid") {
			fmt.Print()
			miners, err := getSPID(ctx, pinfo.AddrInfo.ID)
			if err != nil {
				miners = err.Error()
			}
			fmt.Println(pinfo.AddrInfo.ID, "   ", miners)
		} else {
			fmt.Println(pinfo.AddrInfo.ID)
		}

		return
	}
	if cmd.Bool("publisher") {
		if pinfo.Publisher != nil && len(pinfo.Publisher.Addrs) != 0 {
			fmt.Printf("%s/p2p/%s\n", pinfo.Publisher.Addrs[0], pinfo.Publisher.ID)
		}
		return
	}

	var p2pHost host.Host

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
		if cmd.Bool("protocol") {
			var proto string
			var err error
			p2pHost, err = libp2p.New()
			if err != nil {
				proto = fmt.Sprintf("Error: %s", err)
			} else {
				defer p2pHost.Close()
				proto, err = getProtocol(ctx, *pinfo.Publisher, p2pHost)
				if err != nil {
					proto = fmt.Sprintf("Error: %s", err)
				}
			}
			fmt.Println("        Publisher protocol:", proto)
		}
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

	if cmd.Bool("distance") {
		fmt.Print("    Distance to head advertisement: ")
		dist, _, err := getLastSeenDistance(ctx, cmd, pinfo, p2pHost)
		if err != nil {
			fmt.Println("error:", err)
		} else if dist == -1 {
			fmt.Printf("exceeded limit %d+", cmd.Int64("ad-depth-limit"))
		} else {
			fmt.Println(dist)
		}
	}

	if cmd.Bool("spid") {
		miners, err := getSPID(ctx, pinfo.AddrInfo.ID)
		if err != nil {
			miners = fmt.Sprint("error:", err)
		}
		fmt.Println("    SPID:", miners)
	}

	fmt.Println()
}

func getProtocol(ctx context.Context, peerInfo peer.AddrInfo, p2pHost host.Host) (string, error) {
	clientHost := &libp2phttp.Host{
		StreamHost: p2pHost,
	}

	peerInfo = mautil.CleanPeerAddrInfo(peerInfo)
	if len(peerInfo.Addrs) == 0 {
		if clientHost.StreamHost == nil {
			return "", errors.New("no peer addrs and no stream host")
		}
		peerStore := clientHost.StreamHost.Peerstore()
		if peerStore == nil {
			return "", errors.New("no peer addrs and no stream host peerstore")
		}
		peerInfo.Addrs = peerStore.Addrs(peerInfo.ID)
		if len(peerInfo.Addrs) == 0 {
			return "", errors.New("no peer addrs and none found in peertore")
		}
	}

	_, err := clientHost.NamespacedClient(ipnisync.ProtocolID, peerInfo)
	clientHost.Close()
	if err != nil {
		httpAddrs := mautil.FindHTTPAddrs(peerInfo.Addrs)
		if len(httpAddrs) == 0 {
			return "data-transfer/graphsync", nil
		}
		u, err := maurl.ToURL(peerInfo.Addrs[0])
		if err != nil {
			return "", err
		}
		fetchURL := u.JoinPath(ipnisync.IPNIPath, "head")
		req, err := http.NewRequestWithContext(ctx, "GET", fetchURL.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		return "http", nil
	}
	return "libp2phttp", nil
}

func getLastSeenDistance(ctx context.Context, cmd *cli.Command, pinfo *model.ProviderInfo, p2pHost host.Host) (int, cid.Cid, error) {
	if pinfo.Publisher == nil {
		return 0, cid.Undef, errors.New("no publisher listed")
	}
	if !pinfo.LastAdvertisement.Defined() {
		return 0, cid.Undef, errors.New("no last advertisement")
	}
	adDist, err := dtrack.NewAdDistance(
		dtrack.WithDepthLimit(cmd.Int64("ad-depth-limit")),
		dtrack.WithTopic(cmd.String("topic")),
		dtrack.WithP2pHost(p2pHost))
	if err != nil {
		return 0, cid.Undef, err
	}
	defer adDist.Close()

	return adDist.Get(ctx, *pinfo.Publisher, pinfo.LastAdvertisement, cid.Undef)
}

func getSPID(ctx context.Context, peerID peer.ID) (string, error) {
	apiURL, err := url.JoinPath(filfoxPeerAPI, peerID.String())
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Accept-Encoding", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		return "", errors.New(resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// {"peerId":"12D3KooWFWXbQG9x44JVauFnG7zqzfuR4eDo9iGbXUm9rTLvW7kv","miners":["f0811822"],"multiAddresses":["/ip4/3.140.191.240/tcp/7523"]}
	var spinfo struct {
		Miners []string `json:"miners"`
	}
	if err = json.Unmarshal(data, &spinfo); err != nil {
		return "", err
	}

	return strings.Join(spinfo.Miners, ", "), nil
}
