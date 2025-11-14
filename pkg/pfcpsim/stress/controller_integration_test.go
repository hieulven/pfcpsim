// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
	ieLib "github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// TestStressControllerFullRun tests a complete stress test run with mock UPF
func TestStressControllerFullRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create mock UPF server
	serverAddr := "127.0.0.1:18810"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock UPF that responds to all requests
	go func() {
		buf := make([]byte, 3000)
		for {
			n, addr, err := serverConn.ReadFrom(buf)
			if err != nil {
				return
			}

			req, err := message.Parse(buf[:n])
			if err != nil {
				continue
			}

			var resp message.Message
			switch req.MessageType() {
			case message.MsgTypeSessionEstablishmentRequest:
				resp = message.NewSessionEstablishmentResponse(
					0, 0, 0,
					req.Sequence(),
					0,
					ieLib.NewNodeID("127.0.0.1", "", ""),
					ieLib.NewCause(ieLib.CauseRequestAccepted),
					ieLib.NewFSEID(uint64(req.Sequence())+1000, net.ParseIP("127.0.0.1"), nil),
				)
			case message.MsgTypeSessionModificationRequest:
				resp = message.NewSessionModificationResponse(
					0, 0, 0,
					req.Sequence(),
					0,
					ieLib.NewCause(ieLib.CauseRequestAccepted),
				)
			case message.MsgTypeSessionDeletionRequest:
				resp = message.NewSessionDeletionResponse(
					0, 0, 0,
					req.Sequence(),
					0,
					ieLib.NewCause(ieLib.CauseRequestAccepted),
				)
			default:
				continue
			}

			respBuf := make([]byte, resp.MarshalLen())
			if err := resp.MarshalTo(respBuf); err != nil {
				continue
			}
			serverConn.WriteTo(respBuf, addr)
		}
	}()

	// Create stress test configuration
	config := &StressConfig{
		TPS:                     50,
		Duration:                5 * time.Second,
		RampUpTime:              1 * time.Second,
		TotalSessions:           20,
		ConcurrentSessions:      10,
		BaseID:                  1,
		ModificationsPerSession: 1,
		ModificationInterval:    1 * time.Second,
		UEPoolCIDR:              "17.0.0.0/24",
		GnbAddress:              "192.168.1.1",
		N3Address:               "192.168.1.100",
		RemotePeerAddress:       "127.0.0.1:18810",
		AppFilters:              []string{"ip:any:any:allow:100"},
		QFI:                     9,
		RequestTimeout:          2 * time.Second,
		NumWorkers:              3,
	}

	// Create async client
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	client.SetAssociationStatus(true) // Mark as associated for testing

	ctx, cancel := context.WithCancel(context.Background())
	client.SetContext(ctx)
	defer cancel()

	// Connect
	err = client.ConnectN4Async("127.0.0.1:18810")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Give receiver time to start
	time.Sleep(100 * time.Millisecond)

	// Create controller
	controller := NewStressController(config, client)

	// Run stress test
	err = controller.Run()
	if err != nil {
		t.Fatalf("Stress test failed: %v", err)
	}

	// Verify results
	snapshot := controller.GetMetrics()

	t.Logf("Test Results:")
	t.Logf("  Created: %d", snapshot.TotalCreated)
	t.Logf("  Modified: %d", snapshot.TotalModified)
	t.Logf("  Deleted: %d", snapshot.TotalDeleted)
	t.Logf("  Failed: %d", snapshot.TotalFailed)
	t.Logf("  Success Rate: %.2f%%", snapshot.SuccessRate)
	t.Logf("  Average TPS: %.2f", snapshot.AverageTPS)

	// Verify all sessions were created
	if snapshot.TotalCreated != int64(config.TotalSessions) {
		t.Errorf("Expected %d creates, got %d", config.TotalSessions, snapshot.TotalCreated)
	}

	// Verify modifications happened (at least some)
	if snapshot.TotalModified == 0 {
		t.Error("Expected some modifications, got 0")
	}

	// Verify deletions happened (at least some)
	if snapshot.TotalDeleted == 0 {
		t.Error("Expected some deletions, got 0")
	}

	// Success rate should be high
	if snapshot.SuccessRate < 80.0 {
		t.Errorf("Success rate too low: %.2f%%", snapshot.SuccessRate)
	}
}

// TestStressControllerHighLoad tests controller under high load
func TestStressControllerHighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high load test in short mode")
	}

	// Create mock UPF server
	serverAddr := "127.0.0.1:18811"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Fast responding mock UPF
	go func() {
		buf := make([]byte, 3000)
		for {
			n, addr, err := serverConn.ReadFrom(buf)
			if err != nil {
				return
			}

			req, err := message.Parse(buf[:n])
			if err != nil {
				continue
			}

			var resp message.Message
			switch req.MessageType() {
			case message.MsgTypeSessionEstablishmentRequest:
				resp = message.NewSessionEstablishmentResponse(
					0, 0, 0, req.Sequence(), 0,
					ieLib.NewNodeID("127.0.0.1", "", ""),
					ieLib.NewCause(ieLib.CauseRequestAccepted),
					ieLib.NewFSEID(uint64(req.Sequence())+1000, net.ParseIP("127.0.0.1"), nil),
				)
			case message.MsgTypeSessionModificationRequest:
				resp = message.NewSessionModificationResponse(
					0, 0, 0, req.Sequence(), 0,
					ieLib.NewCause(ieLib.CauseRequestAccepted),
				)
			case message.MsgTypeSessionDeletionRequest:
				resp = message.NewSessionDeletionResponse(
					0, 0, 0, req.Sequence(), 0,
					ieLib.NewCause(ieLib.CauseRequestAccepted),
				)
			default:
				continue
			}

			respBuf := make([]byte, resp.MarshalLen())
			if err := resp.MarshalTo(respBuf); err != nil {
				continue
			}
			serverConn.WriteTo(respBuf, addr)
		}
	}()

	// High load configuration
	config := &StressConfig{
		TPS:                     200, // High TPS
		Duration:                10 * time.Second,
		RampUpTime:              2 * time.Second,
		TotalSessions:           500,
		ConcurrentSessions:      200,
		BaseID:                  1,
		ModificationsPerSession: 2,
		ModificationInterval:    2 * time.Second,
		UEPoolCIDR:              "17.0.0.0/16",
		GnbAddress:              "192.168.1.1",
		N3Address:               "192.168.1.100",
		RemotePeerAddress:       "127.0.0.1:18811",
		AppFilters:              []string{"ip:any:any:allow:100"},
		QFI:                     9,
		RequestTimeout:          5 * time.Second,
		NumWorkers:              10,
	}

	// Create client
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	client.SetAssociationStatus(true)

	ctx, cancel := context.WithCancel(context.Background())
	client.SetContext(ctx)
	defer cancel()

	err = client.ConnectN4Async("127.0.0.1:18811")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create controller
	controller := NewStressController(config, client)

	// Run stress test
	startTime := time.Now()
	err = controller.Run()
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("Stress test failed: %v", err)
	}

	// Verify results
	snapshot := controller.GetMetrics()

	t.Logf("High Load Test Results (elapsed: %v):", elapsed.Round(time.Second))
	t.Logf("  Total Operations: %d", 
		snapshot.TotalCreated+snapshot.TotalModified+snapshot.TotalDeleted)
	t.Logf("  Created: %d", snapshot.TotalCreated)
	t.Logf("  Modified: %d", snapshot.TotalModified)
	t.Logf("  Deleted: %d", snapshot.TotalDeleted)
	t.Logf("  Failed: %d", snapshot.TotalFailed)
	t.Logf("  Success Rate: %.2f%%", snapshot.SuccessRate)
	t.Logf("  Average TPS: %.2f", snapshot.AverageTPS)
	t.Logf("  Create Latency P95: %v", snapshot.CreateLatency.P95)

	// Should have created many sessions
	if snapshot.TotalCreated < int64(config.TotalSessions)*80/100 {
		t.Errorf("Expected at least 80%% of sessions created, got %d/%d",
			snapshot.TotalCreated, config.TotalSessions)
	}

	// Success rate should still be reasonable
	if snapshot.SuccessRate < 70.0 {
		t.Errorf("Success rate too low under high load: %.2f%%", snapshot.SuccessRate)
	}

	// Average TPS should be reasonable
	if snapshot.AverageTPS < float64(config.TPS)*0.5 {
		t.Errorf("Average TPS too low: %.2f (target: %d)", snapshot.AverageTPS, config.TPS)
	}
}

// TestStressControllerCancellation tests graceful cancellation
func TestStressControllerCancellation(t *testing.T) {
	// Create mock UPF server
	serverAddr := "127.0.0.1:18812"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Slow responding mock UPF (to allow cancellation)
	go func() {
		buf := make([]byte, 3000)
		for {
			n, addr, err := serverConn.ReadFrom(buf)
			if err != nil {
				return
			}

			req, err := message.Parse(buf[:n])
			if err != nil {
				continue
			}

			// Delay response
			time.Sleep(100 * time.Millisecond)

			var resp message.Message
			switch req.MessageType() {
			case message.MsgTypeSessionEstablishmentRequest:
				resp = message.NewSessionEstablishmentResponse(
					0, 0, 0, req.Sequence(), 0,
					ieLib.NewNodeID("127.0.0.1", "", ""),
					ieLib.NewCause(ieLib.CauseRequestAccepted),
					ieLib.NewFSEID(uint64(req.Sequence())+1000, net.ParseIP("127.0.0.1"), nil),
				)
			default:
				continue
			}

			respBuf := make([]byte, resp.MarshalLen())
			if err := resp.MarshalTo(respBuf); err != nil {
				continue
			}
			serverConn.WriteTo(respBuf, addr)
		}
	}()

	// Configuration
	config := &StressConfig{
		TPS:                     100,
		Duration:                30 * time.Second, // Long duration
		RampUpTime:              0,
		TotalSessions:           1000, // Many sessions
		BaseID:                  1,
		ModificationsPerSession: 0,
		ModificationInterval:    0,
		UEPoolCIDR:              "17.0.0.0/16",
		GnbAddress:              "192.168.1.1",
		N3Address:               "192.168.1.100",
		RemotePeerAddress:       "127.0.0.1:18812",
		AppFilters:              []string{"ip:any:any:allow:100"},
		QFI:                     9,
		RequestTimeout:          2 * time.Second,
		NumWorkers:              5,
	}

	// Create client
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	client.SetAssociationStatus(true)

	ctx, cancel := context.WithCancel(context.Background())
	client.SetContext(ctx)
	defer cancel()

	err = client.ConnectN4Async("127.0.0.1:18812")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create controller
	controller := NewStressController(config, client)

	// Run in goroutine
	done := make(chan struct{})
	go func() {
		controller.Run()
		close(done)
	}()

	// Wait a bit for test to start
	time.Sleep(1 * time.Second)

	// Cancel the test
	t.Log("Cancelling stress test...")
	controller.Stop()

	// Wait for completion
	select {
	case <-done:
		t.Log("Test cancelled successfully")
	case <-time.After(5 * time.Second):
		t.Error("Test didn't stop within timeout after cancellation")
	}

	// Check that some operations were performed
	snapshot := controller.GetMetrics()
	totalOps := snapshot.TotalCreated + snapshot.TotalModified + snapshot.TotalDeleted

	if totalOps == 0 {
		t.Error("No operations were performed before cancellation")
	}

	t.Logf("Operations completed before cancellation: %d", totalOps)
}