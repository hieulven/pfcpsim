// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"fmt"
	"time"
)

// StressConfig holds the configuration for a stress test run
type StressConfig struct {
	// TPS (Transactions Per Second) is the target rate
	TPS int

	// Duration is how long to run the test
	Duration time.Duration

	// RampUpTime is the period to gradually increase to target TPS
	RampUpTime time.Duration

	// TotalSessions is the total number of sessions to create
	TotalSessions int

	// ConcurrentSessions is the maximum concurrent active sessions
	ConcurrentSessions int

	// BaseID is the starting session ID
	BaseID int

	// ModificationsPerSession is how many times to modify each session
	ModificationsPerSession int

	// ModificationInterval is the time to wait between modifications
	ModificationInterval time.Duration

	// Network configuration
	UEPoolCIDR        string
	GnbAddress        string
	N3Address         string
	RemotePeerAddress string

	// Application filters (SDF filters)
	AppFilters []string

	// QFI (QoS Flow Identifier)
	QFI uint8

	// RequestTimeout is the timeout for each PFCP request
	RequestTimeout time.Duration

	// NumWorkers is the number of worker goroutines
	NumWorkers int
}

// Validate checks if the configuration is valid
func (c *StressConfig) Validate() error {
	if c.TPS <= 0 {
		return fmt.Errorf("TPS must be positive, got %d", c.TPS)
	}

	if c.Duration <= 0 {
		return fmt.Errorf("duration must be positive, got %v", c.Duration)
	}

	if c.TotalSessions <= 0 {
		return fmt.Errorf("total sessions must be positive, got %d", c.TotalSessions)
	}

	if c.ModificationsPerSession < 0 {
		return fmt.Errorf("modifications per session cannot be negative, got %d", c.ModificationsPerSession)
	}

	if c.UEPoolCIDR == "" {
		return fmt.Errorf("UE pool CIDR is required")
	}

	if c.GnbAddress == "" {
		return fmt.Errorf("gNB address is required")
	}

	if c.N3Address == "" {
		return fmt.Errorf("N3 address is required")
	}

	if c.RemotePeerAddress == "" {
		return fmt.Errorf("remote peer address is required")
	}

	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 5 * time.Second
	}

	if c.NumWorkers <= 0 {
		c.NumWorkers = 10
	}

	if len(c.AppFilters) == 0 {
		c.AppFilters = []string{"ip:any:any:allow:100"}
	}

	if c.QFI == 0 {
		c.QFI = 9
	}

	return nil
}

// NewDefaultStressConfig creates a configuration with sensible defaults
func NewDefaultStressConfig() *StressConfig {
	return &StressConfig{
		TPS:                     100,
		Duration:                60 * time.Second,
		RampUpTime:              10 * time.Second,
		TotalSessions:           1000,
		ConcurrentSessions:      500,
		BaseID:                  1,
		ModificationsPerSession: 2,
		ModificationInterval:    5 * time.Second,
		UEPoolCIDR:              "17.0.0.0/16",
		GnbAddress:              "192.168.1.1",
		N3Address:               "192.168.1.100",
		RemotePeerAddress:       "127.0.0.1:8805",
		AppFilters:              []string{"ip:any:any:allow:100"},
		QFI:                     9,
		RequestTimeout:          5 * time.Second,
		NumWorkers:              10,
	}
}