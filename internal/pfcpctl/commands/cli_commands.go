// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation
package commands
import (
	"context"
	pb "github.com/omec-project/pfcpsim/api"
	"github.com/omec-project/pfcpsim/logger"
	"github.com/urfave/cli/v3"
)
// GetServiceCommands returns the service commands for CLI v3
func GetServiceCommands() *cli.Command {
	return &cli.Command{
	Name:  "service",
	Usage: "Configure pfcpsim service",
	Commands: []*cli.Command{
	{
		Name:  "configure",
		Usage: "Configure remote addresses",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "remote-peer-addr",
				Aliases: []string{"r"},
				Usage:   "The remote PFCP agent address",
			},
			&cli.StringFlag{
				Name:    "n3-addr",
				Aliases: []string{"n"},
				Usage:   "UPF's N3 IP address",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
		client := connect()
		defer disconnect()
			res, err := client.Configure(ctx, &pb.ConfigureRequest{
				UpfN3Address:      c.String("n3-addr"),
				RemotePeerAddress: c.String("remote-peer-addr"),
			})
			if err != nil {
				logger.PfcpsimLog.Fatalf("error while configuring remote addresses: %v", err)
			}

			logger.PfcpsimLog.Infoln(res.Message)
			return nil
		},
	},
	{
		Name:  "associate",
		Usage: "Establish PFCP association",
		Action: func(ctx context.Context, c *cli.Command) error {
			client := connect()
			defer disconnect()

			res, err := client.Associate(ctx, &pb.EmptyRequest{})
			if err != nil {
				logger.PfcpsimLog.Fatalf("error while associating: %v", err)
			}

			logger.PfcpsimLog.Infoln(res.Message)
			return nil
		},
	},
	{
		Name:  "disassociate",
		Usage: "Teardown PFCP association",
		Action: func(ctx context.Context, c *cli.Command) error {
			client := connect()
			defer disconnect()

			res, err := client.Disassociate(ctx, &pb.EmptyRequest{})
			if err != nil {
				logger.PfcpsimLog.Fatalf("error while disassociating: %v", err)
			}

			logger.PfcpsimLog.Infoln(res.Message)
			return nil
		},
	},
	},
	}
}
// GetSessionCommands returns the session commands for CLI v3
func GetSessionCommands() *cli.Command {
	return &cli.Command{
		Name:  "session",
		Usage: "Handle PFCP sessions",
		Commands: []*cli.Command{
		{
			Name:  "create",
			Usage: "Create PFCP sessions",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "count",
					Aliases: []string{"c"},
					Value:   1,
					Usage:   "The number of sessions to create",
				},
				&cli.IntFlag{
					Name:    "baseID",
					Aliases: []string{"i"},
					Value:   1,
					Usage:   "The base ID to use",
				},
				&cli.StringFlag{
					Name:    "ue-pool",
					Aliases: []string{"u"},
					Value:   "17.0.0.0/24",
					Usage:   "The UE pool address",
				},
				&cli.StringFlag{
					Name:    "gnb-addr",
					Aliases: []string{"g"},
					Usage:   "The gNB address",
				},
				&cli.StringSliceFlag{
					Name:    "app-filter",
					Aliases: []string{"a"},
					Usage:   "Application filter (can be specified multiple times)",
				},
				&cli.IntFlag{
					Name:    "qfi",
					Aliases: []string{"q"},
					Value:   0,
					Usage:   "The QFI value for QERs (max 64)",
				},
			},
			Action: func(ctx context.Context, c *cli.Command) error {
				if c.Int("qfi") > 64 {
					logger.PfcpsimLog.Fatalf("qfi cannot be greater than 64. Provided qfi: %v", c.Int("qfi"))
				}
				client := connect()
				defer disconnect()

				appFilters := c.StringSlice("app-filter")
				if len(appFilters) == 0 {
					appFilters = []string{"ip:any:any:allow:100"}
				}

				res, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
					Count:         int32(c.Int("count")),
					BaseID:        int32(c.Int("baseID")),
					NodeBAddress:  c.String("gnb-addr"),
					UeAddressPool: c.String("ue-pool"),
					AppFilters:    appFilters,
					Qfi:           int32(c.Int("qfi")),
				})
				if err != nil {
					logger.PfcpsimLog.Fatalf("error while creating sessions: %v", err)
				}

				logger.PfcpsimLog.Infoln(res.Message)
				return nil
			},
		},
		{
			Name:  "modify",
			Usage: "Modify PFCP sessions",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "count",
					Aliases: []string{"c"},
					Value:   1,
					Usage:   "The number of sessions to modify",
				},
				&cli.IntFlag{
					Name:    "baseID",
					Aliases: []string{"i"},
					Value:   1,
					Usage:   "The base ID to use",
				},
				&cli.StringFlag{
					Name:    "ue-pool",
					Aliases: []string{"u"},
					Value:   "17.0.0.0/24",
					Usage:   "The UE pool address",
				},
				&cli.StringFlag{
					Name:    "gnb-addr",
					Aliases: []string{"g"},
					Usage:   "The gNB address",
				},
				&cli.BoolFlag{
					Name:    "buffer",
					Aliases: []string{"b"},
					Usage:   "Set buffer flag for downlink FARs",
				},
				&cli.BoolFlag{
					Name:    "notifycp",
					Aliases: []string{"n"},
					Usage:   "Set notifyCP flag for downlink FARs",
				},
				&cli.StringSliceFlag{
					Name:    "app-filter",
					Aliases: []string{"a"},
					Usage:   "Application filter (can be specified multiple times)",
				},
			},
			Action: func(ctx context.Context, c *cli.Command) error {
				client := connect()
				defer disconnect()

				appFilters := c.StringSlice("app-filter")
				if len(appFilters) == 0 {
					appFilters = []string{"ip:any:any:allow:100"}
				}

				res, err := client.ModifySession(ctx, &pb.ModifySessionRequest{
					Count:         int32(c.Int("count")),
					BaseID:        int32(c.Int("baseID")),
					NodeBAddress:  c.String("gnb-addr"),
					UeAddressPool: c.String("ue-pool"),
					BufferFlag:    c.Bool("buffer"),
					NotifyCPFlag:  c.Bool("notifycp"),
					AppFilters:    appFilters,
				})
				if err != nil {
					logger.PfcpsimLog.Fatalf("error while modifying sessions: %v", err)
				}

				logger.PfcpsimLog.Infof(res.Message)
				return nil
			},
		},
		{
			Name:  "delete",
			Usage: "Delete PFCP sessions",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "count",
					Aliases: []string{"c"},
					Value:   1,
					Usage:   "The number of sessions to delete",
				},
				&cli.IntFlag{
					Name:    "baseID",
					Aliases: []string{"i"},
					Value:   1,
					Usage:   "The base ID to use",
				},
			},
			Action: func(ctx context.Context, c *cli.Command) error {
				client := connect()
				defer disconnect()

				res, err := client.DeleteSession(ctx, &pb.DeleteSessionRequest{
					Count:  int32(c.Int("count")),
					BaseID: int32(c.Int("baseID")),
				})
				if err != nil {
					logger.PfcpsimLog.Fatalf("error while deleting sessions: %v", err)
				}

				logger.PfcpsimLog.Infoln(res.Message)
				return nil
			},
		},
		},
	}
}