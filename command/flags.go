package command

import (
	"github.com/urfave/cli/v2"
)

var indexerURLFlag = &cli.StringFlag{
	Name:    "indexer",
	Usage:   "Indexer URL",
	EnvVars: []string{"INDEXER"},
	Aliases: []string{"i"},
	Value:   "http://localhost:3000",
}

var fileFlag = &cli.StringFlag{
	Name:     "file",
	Usage:    "Source file for import",
	Aliases:  []string{"f"},
	Required: true,
}

var providerFlag = &cli.StringFlag{
	Name:     "provider",
	Usage:    "Provider's peer ID",
	Aliases:  []string{"p"},
	Required: true,
}

var (
	topic string
)

var adEntriesDepthLimitFlag = &cli.Int64Flag{
	Name:        "ad-entries-depth-limit",
	Aliases:     []string{"edl"},
	Usage:       "Maximum depth (number of blocks of multihashes) to fetch from advertisement entries chains.",
	Value:       100,
	DefaultText: "100 (set to '0' for unlimited)",
}
