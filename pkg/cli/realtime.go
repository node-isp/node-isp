package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/apex/log"
	"github.com/urfave/cli/v3"

	"github.com/node-isp/node-isp/pkg/server/realtime"
)

var backendDomain string

var RealtimeServerCommand = &cli.Command{
	Name:        "realtime",
	Usage:       "NodeISP Standalone Realtime Server",
	Description: "The RealTime server is mainly needed for Radius development, and run's only the RealTime server from NodeISP.",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "backend-domain",
			Usage:       "The URL of the backend server",
			Destination: &backendDomain,
			Required:    true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		rt := &realtime.RealTime{
			BackendUrl: fmt.Sprintf("https://%s/api/centrifugo", backendDomain),
			Log:        log.WithField("component", "realtime"),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/_internal/realtime", rt.Handler)

		go func() {
			if err := http.ListenAndServe("127.0.0.1:9998", mux); err != nil {
				rt.Log.WithError(err).Fatal("Failed to start RealTime HTTP server")
			}
		}()

		if err := rt.Run(); err != nil {
			rt.Log.WithError(err).Fatal("Failed to start RealTime WS server")
		}

		rt.Log.Info("RealTime server started")

		select {}
	},
}
