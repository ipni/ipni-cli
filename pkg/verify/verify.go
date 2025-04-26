package verify

import (
	"github.com/urfave/cli/v3"
)

var VerifyCmd = &cli.Command{
	Name:  "verify",
	Usage: "Verifies advertised content validity and queryability from an indexer",
	Commands: []*cli.Command{
		verifyIngestSubCmd,
	},
}
