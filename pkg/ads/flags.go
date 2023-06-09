package ads

import "github.com/urfave/cli/v2"

var addrInfoFlag = &cli.StringFlag{
	Name: "addr-info",
	Usage: "Publisher's address info in form of libp2p multiaddr info.\n" +
		"Example GraphSync: /ip4/1.2.3.4/tcp/1234/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ\n" +
		"Example HTTP:      /ip4/1.2.3.4/tcp/1234/http/p2p/12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ",
	Aliases:  []string{"ai"},
	Required: true,
}

var topicFlag = &cli.StringFlag{
	Name:    "topic",
	Usage:   "Topic on which index advertisements are published. Only needed if connecting via Graphsync with non-standard topic.",
	Value:   "/indexer/ingest/mainnet",
	Aliases: []string{"t"},
}
