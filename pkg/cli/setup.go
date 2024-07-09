package cli

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/node-isp/node-isp/pkg/setup"
)

var SetupCommand = &cli.Command{
	Name:  "setup",
	Usage: "Run NodeISP for the first time",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return setup.Run()
	},
}
