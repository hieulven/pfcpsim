// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"context"
	"net"
	"testing"
	"time"

	ieLib "github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// TestAsyncRequestResponseFlow tests the complete async flow
func TestAsyncRequestResponseFlow(t *testing.T) {
	// Create mock UDP server to simulate UPF
	serverAddr := "127.0.0.1:18805"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock server that echoes responses
	go func() {
		buf := make([]byte, 3000)
		for {
			n, addr, err := serverConn.ReadFrom(buf)
			if err != nil {
				return
			}

			// Parse request
			req, err := message.Parse(buf[:n])
			if err != nil {
				continue
			}

			// Create response based on request type
			var resp message.Message
			switch req.MessageType() {
			case message.MsgTypeSessionEstablishmentRequest:
				resp = message.NewSessionEstablishmentResponse(
					0, 0, 0,
					req.Sequence(),
					0,
					ieLib.NewNodeID("127.0.0.1", "", ""),
					ieLib.NewCause(ieLib.CauseRequestAccepted),
					ieLib.NewFSEID(1, net.ParseIP("127.0.0.1"), nil),
				)
			default:
				continue
			}

			// Send response
			respBuf := make([]byte, resp.MarshalLen())
			if err := resp.MarshalTo(respBuf); err != nil {
				continue
			}
			serverConn.WriteTo(respBuf, addr)
		}
	}()

	// Create async client
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect to mock server
	err = client.ConnectN4Async("127.0.0.1:18805")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Give receiver time to start
	time.Sleep(50 * time.Millisecond)

	// Send async request
	req := message.NewSessionEstablishmentRequest(
		0, 0, 0,
		0, // Will be set by SendAsync
		0,
		ieLib.NewNodeID("127.0.0.1", "", ""),
		ieLib.NewFSEID(1, net.ParseIP("127.0.0.1"), nil),
		ieLib.NewPDNType(ieLib.PDNTypeIPv4),
	)

	resultCh, err := client.SendAsync(req, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to send async: %v", err)
	}

	// Wait for response
	select {
	case result := <-resultCh:
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if result.Message == nil {
			t.Error("Expected message, got nil")
		}
		if result.Latency <= 0 {
			t.Error("Expected positive latency")
		}
		t.Logf("Received response after %v", result.Latency)
	case <-time.After(3 * time.Second):
		t.Error("Timeout waiting for response")
	}

	// Verify stats
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalSent != 1 {
		t.Errorf("Expected TotalSent 1, got %d", snapshot.TotalSent)
	}
	if snapshot.TotalReceived != 1 {
		t.Errorf("Expected TotalReceived 1, got %d", snapshot.TotalReceived)
	}
}