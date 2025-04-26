package random

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipni/go-libipni/apierror"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/pcache"
	"github.com/ipni/ipni-cli/pkg/adpub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

var RandomCmd = &cli.Command{
	Name:        "random",
	Usage:       "Show random multihashes from a random advertisement",
	Description: "For specified providers, choose an advertisement with undeleted content from a random depth between 1 and n in the chain and return m random multihashs from the first entries block.",
	Flags:       randomFlags,
	Action:      randomAction,
}

var randomFlags = []cli.Flag{
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
	&cli.IntFlag{
		Name:    "number",
		Usage:   "Number of advertisements in chain to make random selection from.",
		Aliases: []string{"n"},
		Value:   10,
	},
	&cli.IntFlag{
		Name:    "multihashes",
		Usage:   "Number of multihashes to randomly select from advertisement.",
		Aliases: []string{"m"},
		Value:   5,
	},
	&cli.BoolFlag{
		Name:    "quiet",
		Usage:   "Only print multihashes and do not print descriptive output.",
		Aliases: []string{"q"},
	},
	&cli.StringFlag{
		Name:    "topic",
		Usage:   "Topic on which index advertisements are published. Only needed if connecting via Graphsync with non-standard topic.",
		Value:   "/indexer/ingest/mainnet",
		Aliases: []string{"t"},
	},
}

func randomAction(ctx context.Context, cmd *cli.Command) error {
	adCount := cmd.Int("number")
	if adCount <= 0 {
		return errors.New("number must be at least 1")
	}
	mhsCount := cmd.Int("multihashes")
	if mhsCount <= 0 {
		return errors.New("multihashes must be at least 1")
	}

	peerIDs, err := readPeerIDs(cmd)
	if err != nil {
		return err
	}

	var pc *pcache.ProviderCache
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

	for peerID := range peerIDs {
		prov, err := getProvider(ctx, pc, peerID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting provider %s: %s\n", peerID, err)
			continue
		}
		if prov.Publisher == nil {
			fmt.Fprintf(os.Stderr, "Provider %s has no publisher\n", peerID)
			continue
		}
		err = RandomMultihashes(ctx, *prov.Publisher, cmd.String("topic"), adCount, mhsCount, cmd.Bool("quiet"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get random multihashes from provider %s: %s\n", peerID, err)
			continue
		}
	}

	return nil
}

func getProvider(ctx context.Context, pc *pcache.ProviderCache, peerID peer.ID) (*model.ProviderInfo, error) {
	prov, err := pc.Get(ctx, peerID)
	if err != nil {
		var ae *apierror.Error
		if errors.As(err, &ae) && ae.Status() == http.StatusNotFound {
			return nil, errors.New("provider not found on indexer")
		}
		return nil, err
	}
	if prov == nil {
		return nil, errors.New("provider not found on indexer")
	}

	return prov, nil
}

func RandomMultihashes(ctx context.Context, addrInfo peer.AddrInfo, topic string, adCount, mhsCount int, quiet bool) error {
	provClient, err := adpub.NewClient(addrInfo,
		adpub.WithTopicName(topic),
		adpub.WithEntriesDepthLimit(1),
	)
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "ipni-rand-ad")
	if err != nil {
		return err
	}
	defer func() {
		tempFile.Close()
		os.RemoveAll(tempFile.Name())
	}()

	err = provClient.List(ctx, cid.Undef, adCount, tempFile)
	if err != nil {
		return err
	}
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	lines := make([]string, 0, adCount)
	scanner := bufio.NewScanner(tempFile)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSpace(scanner.Text()))
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if !quiet {
		fmt.Fprintf(os.Stderr, "Read %d advertisements, checking for deleted content", len(lines))
	}

	// Filter ads
	removed := make(map[string]struct{})
	var ads []*adpub.Advertisement
	var delCount, rmCount, tooFewCount, failCount int
	for _, cidStr := range lines {
		if !quiet {
			fmt.Fprint(os.Stderr, ".")
		}
		adCid, err := cid.Decode(cidStr)
		if err != nil {
			return fmt.Errorf("bad advertisement cid: %w", err)
		}
		ad, err := provClient.GetAdvertisement(ctx, adCid)
		if err != nil {
			failCount++
			fmt.Fprintf(os.Stderr, "\n⚠️ Failed to fully sync advertisement %s. Error: %s\n", cidStr, err.Error())
			continue
		}
		ctxID := string(ad.ContextID)
		if ad.IsRemove {
			rmCount++
			removed[ctxID] = struct{}{}
			continue
		}
		if !ad.HasEntries() {
			tooFewCount++
			continue
		}
		if _, rm := removed[ctxID]; rm {
			delCount++
			continue
		}
		ads = append(ads, ad)
	}
	if !quiet {
		fmt.Println()
		if rmCount != 0 {
			fmt.Fprintln(os.Stderr, "  Removal ads skipped:", rmCount)
		}
		if delCount != 0 {
			fmt.Fprintln(os.Stderr, "  Deleted ads skipped:", delCount)
		}
		if tooFewCount != 0 {
			fmt.Fprintln(os.Stderr, "  Ads with too few multihashes:", tooFewCount)
		}
		if failCount != 0 {
			fmt.Fprintln(os.Stderr, "  Ads failed to sync entries:", failCount)
		}
	}
	if len(ads) == 0 {
		return errors.New("no suitable advertisements")
	}
	if !quiet {
		fmt.Fprintln(os.Stderr, "Choosing", mhsCount, "multihashes from 1 random out of", len(ads), "suitable advertisements")
	}

	rand.Shuffle(len(ads), func(i, j int) {
		ads[i], ads[j] = ads[j], ads[i]
	})

	failCount = 0
	tooFewCount = 0
	for _, ad := range ads {
		err = provClient.SyncEntriesWithRetry(ctx, ad.Entries.Root())
		if err != nil {
			failCount++
			fmt.Fprintf(os.Stderr, "⚠️ Failed to sync entries for advertisement %s. Error: %s\n", ad.ID, err)
			continue
		}
		entries, err := ad.Entries.Drain()
		if err != nil {
			if !errors.Is(err, datastore.ErrNotFound) {
				return err
			}
		}
		if len(entries) < mhsCount {
			tooFewCount++
			continue
		}

		if quiet {
			for _, mh := range entries[:mhsCount] {
				fmt.Println(mh.B58String())
			}
			return nil
		}

		fmt.Println("Advertisement:", ad.ID)
		var prevCID string
		if ad.PreviousID != cid.Undef {
			prevCID = ad.PreviousID.String()
		}
		fmt.Println("Previous:     ", prevCID)
		fmt.Println("Provider:     ", ad.ProviderID)
		fmt.Println("ContextID:    ", base64.StdEncoding.EncodeToString(ad.ContextID))

		fmt.Println("Random Multihashes:")
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})

		for _, mh := range entries[:mhsCount] {
			fmt.Println(" ", mh.B58String())
		}
		return nil
	}
	if !quiet {
		fmt.Fprintln(os.Stderr, "⚠️ Failed to get multihashs from", len(ads), "suitable advertisements")
		if tooFewCount != 0 {
			fmt.Fprintln(os.Stderr, "  Ads with too few multihashes:", tooFewCount)
		}
		if failCount != 0 {
			fmt.Fprintln(os.Stderr, "  Ads failed to sync entries:", failCount)
		}
	}
	return errors.New("no multihashes")
}

func readPeerIDs(cmd *cli.Command) (map[peer.ID]struct{}, error) {
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
			return nil, err
		}
	}

	peerIDs := make(map[peer.ID]struct{}, len(pids))
	for _, pid := range pids {
		peerID, err := peer.Decode(pid)
		if err != nil {
			return nil, fmt.Errorf("invalid peer ID %s: %s", pid, err)
		}
		peerIDs[peerID] = struct{}{}
	}

	return peerIDs, nil
}
