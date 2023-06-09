package ads

import (
	"github.com/urfave/cli/v2"
)

var AdsCmd = &cli.Command{
	Name:  "ads",
	Usage: "Show advertisements on a chain from a specified publisher",
	Subcommands: []*cli.Command{
		adsGetSubCmd,
		adsListSubCmd,
		adsDistSubCmd,
	},
}
