// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package commands

import (
	"strings"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
	"github.com/omec-project/pfcpsim/pkg/pfcpsim/stress"
	"github.com/urfave/cli/v3"
)

// GetStressCommands returns the stress test command
func GetStressCommands() *cli.Command {
	return &cli.Command{
		Name:  "stress",
		Usage: "Run stress test against PFCP agent",
		Description: `The stress test command performs high-throughput testing of PFCP agents.
It creates, modifies, and deletes sessions with configurable rate limiting and load patterns.

Examples:
  # Basic stress test
  pfcpctl stress --tps 100 --duration 60s --sessions 1000

  # With ramp-up
  pfcpctl stress --tps 200 --ramp-up 30s --duration 5m --sessions 5000

  # High load test
  pfcpctl stress --tps 500 --workers 20 --sessions 10000 --modifications 3
`,
		Flags: []cli.Flag{
			// Rate control flags
			&cli.IntFlag{
				Name:    "tps",
				Aliases: []string{"t"},
				Value:   100,
				Usage:   "Target transactions per second",
			},
			&cli.DurationFlag{
				Name:    "duration",
				Aliases: []string{"d"},
				Value:   60 * time.Second,
				Usage:   "Test duration (e.g., 60s, 5m, 1h)",
			},
			&cli.DurationFlag{
				Name:    "ramp-up",
				Aliases: []string{"r"},
				Value:   0,
				Usage:   "Ramp-up time for gradual TPS increase",
			},

			// Session configuration flags
			&cli.IntFlag{
				Name:    "sessions",
				Aliases: []string{"s"},
				Value:   1000,
				Usage:   "Total number of sessions to create",
			},
			&cli.IntFlag{
				Name:  "concurrent",
				Value: 500,
				Usage: "Maximum concurrent active sessions",
			},
			&cli.IntFlag{
				Name:  "base-id",
				Value: 1,
				Usage: "Starting session ID",
			},
			&cli.IntFlag{
				Name:    "modifications",
				Aliases: []string{"m"},
				Value:   2,
				Usage:   "Number of modifications per session",
			},
			&cli.DurationFlag{
				Name:  "modify-interval",
				Value: 5 * time.Second,
				Usage: "Time between modification iterations",
			},

			// Network configuration flags
			&cli.StringFlag{
				Name:    "ue-pool",
				Aliases: []string{"u"},
				Value:   "17.0.0.0/16",
				Usage:   "UE IP address pool (CIDR notation)",
			},
			&cli.StringFlag{
				Name:    "gnb-addr",
				Aliases: []string{"g"},
				Value:   "192.168.1.1",
				Usage:   "gNodeB IP address",
			},
			&cli.StringFlag{
				Name:    "n3-addr",
				Aliases: []string{"n"},
				Value:   "192.168.1.100",
				Usage:   "N3 interface IP address (UPF side)",
			},
			&cli.StringFlag{
				Name:    "remote-peer-addr",
				Aliases: []string{"p"},
				Value:   "127.0.0.1:8805",
				Usage:   "Remote PFCP agent address (IP:PORT)",
			},

			// Application filter flags
			&cli.StringSliceFlag{
				Name:    "app-filter",
				Aliases: []string{"a"},
				Value:   []string{"ip:any:any:allow:100"},
				Usage: "Application filter (format: '{ip|udp|tcp}:{IPv4Prefix|any}:{ports|any}:{allow|deny}:{precedence}')",
			},
			&cli.UintFlag{
				Name:    "qfi",
				Aliases: []string{"q"},
				Value:   9,
				Usage:   "QoS Flow Identifier (QFI) value",
			},

			// Performance tuning flags
			&cli.IntFlag{
				Name:    "workers",
				Aliases: []string{"w"},
				Value:   10,
				Usage:   "Number of worker goroutines",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Value: 5 * time.Second,
				Usage: "PFCP request timeout",
			},

			// Local address flag
			&cli.StringFlag{
				Name:  "local-addr",
				Value: "127.0.0.1",
				Usage: "Local IP address to bind to",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return stressTestAction(ctx, c)
		},
	}
}

// stressTestAction executes the stress test
func stressTestAction(ctx context.Context, c *cli.Command) error {
	// Parse and validate flags
	config, err := parseStressConfig(c)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Display configuration
	printConfiguration(config)

	// Confirm execution for high load tests
	if config.TPS > 500 || config.TotalSessions > 10000 {
		if !confirmExecution(config) {
			logger.PfcpsimLog.Infoln("Stress test cancelled by user")
			return nil
		}
	}

	// Create async PFCP client
	localAddr := c.String("local-addr")
	client := pfcpsim.NewAsyncPFCPClient(localAddr)

	logger.PfcpsimLog.Infof("Connecting to PFCP agent at %s...", config.RemotePeerAddress)

	// Connect to remote peer
	err = client.ConnectN4Async(config.RemotePeerAddress)
	if err != nil {
		return fmt.Errorf("failed to connect to PFCP agent: %w", err)
	}

	logger.PfcpsimLog.Infoln("Connected to PFCP agent")

	// Setup association
	logger.PfcpsimLog.Infoln("Setting up PFCP association...")
	err = client.SetupAssociation()
	if err != nil {
		return fmt.Errorf("failed to setup association: %w", err)
	}

	logger.PfcpsimLog.Infoln("PFCP association established")

	// Create stress controller
	controller := stress.NewStressController(config, client)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run stress test in goroutine
	testDone := make(chan error, 1)
	go func() {
		testDone <- controller.Run()
	}()

	// Wait for completion or interruption
	select {
	case err := <-testDone:
		if err != nil {
			logger.PfcpsimLog.Errorf("Stress test failed: %v", err)
			return err
		}
		logger.PfcpsimLog.Infoln("Stress test completed successfully")

	case sig := <-sigChan:
		logger.PfcpsimLog.Infof("Received signal %v, stopping stress test...", sig)
		controller.Stop()
		
		// Wait for graceful shutdown
		select {
		case <-testDone:
			logger.PfcpsimLog.Infoln("Stress test stopped gracefully")
		case <-time.After(10 * time.Second):
			logger.PfcpsimLog.Warnln("Timeout waiting for graceful shutdown, forcing exit")
		}
	}

	// Teardown association
	logger.PfcpsimLog.Infoln("Tearing down PFCP association...")
	err = client.TeardownAssociation()
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to teardown association: %v", err)
	}

	return nil
}

// parseStressConfig parses CLI flags into StressConfig
func parseStressConfig(c *cli.Command) (*stress.StressConfig, error) {
	config := &stress.StressConfig{
		// Rate control
		TPS:        c.Int("tps"),
		Duration:   c.Duration("duration"),
		RampUpTime: c.Duration("ramp-up"),

		// Session configuration
		TotalSessions:           c.Int("sessions"),
		ConcurrentSessions:      c.Int("concurrent"),
		BaseID:                  c.Int("base-id"),
		ModificationsPerSession: c.Int("modifications"),
		ModificationInterval:    c.Duration("modify-interval"),

		// Network configuration
		UEPoolCIDR:        c.String("ue-pool"),
		GnbAddress:        c.String("gnb-addr"),
		N3Address:         c.String("n3-addr"),
		RemotePeerAddress: c.String("remote-peer-addr"),

		// Application filters
		AppFilters: c.StringSlice("app-filter"),
		QFI:        uint8(c.Uint("qfi")),

		// Performance tuning
		NumWorkers:     c.Int("workers"),
		RequestTimeout: c.Duration("timeout"),
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// printConfiguration displays the test configuration
func printConfiguration(config *stress.StressConfig) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("STRESS TEST CONFIGURATION")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Rate Control:\n")
	fmt.Printf("  Target TPS:           %d\n", config.TPS)
	fmt.Printf("  Test Duration:        %v\n", config.Duration)
	if config.RampUpTime > 0 {
		fmt.Printf("  Ramp-up Time:         %v\n", config.RampUpTime)
	}
	fmt.Println()
	fmt.Printf("Session Configuration:\n")
	fmt.Printf("  Total Sessions:       %d\n", config.TotalSessions)
	fmt.Printf("  Concurrent Sessions:  %d\n", config.ConcurrentSessions)
	fmt.Printf("  Base Session ID:      %d\n", config.BaseID)
	fmt.Printf("  Modifications/Session: %d\n", config.ModificationsPerSession)
	if config.ModificationsPerSession > 0 {
		fmt.Printf("  Modification Interval: %v\n", config.ModificationInterval)
	}
	fmt.Println()
	fmt.Printf("Network Configuration:\n")
	fmt.Printf("  UE Pool:              %s\n", config.UEPoolCIDR)
	fmt.Printf("  gNodeB Address:       %s\n", config.GnbAddress)
	fmt.Printf("  N3 Address:           %s\n", config.N3Address)
	fmt.Printf("  Remote Peer:          %s\n", config.RemotePeerAddress)
	fmt.Printf("  QFI:                  %d\n", config.QFI)
	fmt.Println()
	fmt.Printf("Performance Tuning:\n")
	fmt.Printf("  Worker Threads:       %d\n", config.NumWorkers)
	fmt.Printf("  Request Timeout:      %v\n", config.RequestTimeout)
	fmt.Printf("  App Filters:          %d configured\n", len(config.AppFilters))
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()
}

// confirmExecution prompts user to confirm high-load test execution
func confirmExecution(config *stress.StressConfig) bool {
	fmt.Println()
	fmt.Println("WARNING: This is a high-load stress test!")
	fmt.Printf("  Target TPS: %d\n", config.TPS)
	fmt.Printf("  Total Sessions: %d\n", config.TotalSessions)
	fmt.Printf("  Estimated peak operations: %d ops/sec\n", 
		config.TPS*config.ModificationsPerSession)
	fmt.Println()
	fmt.Print("Are you sure you want to continue? (yes/no): ")

	var response string
	fmt.Scanln(&response)

	return response == "yes" || response == "y" || response == "Y"
}