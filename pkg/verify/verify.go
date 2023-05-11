package verify

import (
	"github.com/urfave/cli/v2"
)

var VerifyCmd = &cli.Command{
	Name:  "verify",
	Usage: "Verifies advertised content validity and queribility from an indexer",
	Subcommands: []*cli.Command{
		verifyIngestSubCmd,
	},
}
