package cli

import (
	"github.com/urfave/cli/v3"

	"github.com/node-isp/node-isp/pkg/config"
	"github.com/node-isp/node-isp/pkg/version"
)

var RootCommand = &cli.Command{
	Name:    "node-isp",
	Usage:   "Building blocks for your own ISP",
	Version: version.Version,
	Flags: []cli.Flag{
		ConfigFlag,
	},
	Commands: append([]*cli.Command{
		SetupCommand,
		ServerCommand,
		RealtimeServerCommand,
		RadiusCommand,
	}, ClientCommands...),
}

var ConfigFlag = &cli.StringFlag{
	Name:        "config",
	Aliases:     []string{"c"},
	Usage:       "Load configuration from `FILE`",
	Sources:     cli.EnvVars("NODEISP_CONFIG"),
	Value:       "/etc/node-isp/config.yaml",
	Destination: &config.File,
}
