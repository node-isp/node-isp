package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/node-isp/node-isp/pkg/radius"
)

var domain string
var token string
var insecure bool
var cacheDir string

var RadiusCommand = &cli.Command{
	Name:  "radius",
	Usage: "Radius server management",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "domain",
			Usage:       "The domain of the Node ISP Instance",
			Destination: &domain,
			Required:    true,
			Sources:     cli.EnvVars("NODE_ISP_DOMAIN"),
		},

		&cli.StringFlag{
			Name:        "token",
			Usage:       "The Radius Server Token from the Node ISP Instance",
			Destination: &token,
			Required:    true,
			Sources:     cli.EnvVars("NODE_ISP_RADIUS_TOKEN"),
		},

		&cli.StringFlag{
			Name:        "cache-dir",
			Usage:       "The directory to store the cache file in",
			Destination: &cacheDir,
			Value:       "/var/lib/nodeisp/radius",
			Sources:     cli.EnvVars("NODE_ISP_RADIUS_CACHE_DIR"),
		},

		&cli.BoolFlag{
			Name:        "insecure",
			Usage:       "Allow insecure connections",
			Destination: &insecure,
			Value:       false,
			Sources:     cli.EnvVars("NODE_ISP_RADIUS_INSECURE"),
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		url := fmt.Sprintf("wss://%s/_internal/realtime", domain)
		if insecure {
			url = fmt.Sprintf("ws://%s/_internal/realtime", domain)
		}

		return radius.Run(
			url,
			token,
			cacheDir,
		)
	},
}
