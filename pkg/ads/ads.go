package ads

import (
	"github.com/urfave/cli/v3"
)

var AdsCmd = &cli.Command{
	Name:  "ads",
	Usage: "Show advertisements on a chain from a specified publisher",
	Commands: []*cli.Command{
		adsGetSubCmd,
		adsListSubCmd,
		adsCrawlSubCmd,
		adsDistSubCmd,
	},
}
