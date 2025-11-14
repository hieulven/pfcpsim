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

// TestAsyncSessionEstablishmentFlow tests the complete async session establishment flow
func TestAsyncSessionEstablishmentFlow(t *testing.T) {
	// Create mock UPF server
	serverAddr := "127.0.0.1:18806"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock UPF that responds to session requests
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
					ieLib.NewFSEID(9999, net.ParseIP("127.0.0.1"), nil),
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

	// Create async client
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect
	err = client.ConnectN4Async("127.0.0.1:18806")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Mark association as active (normally done by association setup)
	client.isAssociationActive = true

	// Give receiver time to start
	time.Sleep(50 * time.Millisecond)

	// Send async session establishment
	pdrs := []*ieLib.IE{ieLib.NewCreatePDR(ieLib.NewPDRID(1))}
	fars := []*ieLib.IE{ieLib.NewCreateFAR(ieLib.NewFARID(1))}
	qers := []*ieLib.IE{ieLib.NewCreateQER(ieLib.NewQERID(1))}

	op, err := client.EstablishSessionAsync(1, pdrs, fars, qers, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to send establishment request: %v", err)
	}

	// Process result
	session, err := client.ProcessSessionResult(op)
	if err != nil {
		t.Fatalf("Failed to process result: %v", err)
	}

	if session == nil {
		t.Fatal("Expected session, got nil")
	}

	if session.GetPeerSEID() != 9999 {
		t.Errorf("Expected peer SEID 9999, got %d", session.GetPeerSEID())
	}

	t.Logf("Session established: %s", session.String())
}

// TestAsyncSessionModificationFlow tests the async session modification flow
func TestAsyncSessionModificationFlow(t *testing.T) {
	// Create mock UPF server
	serverAddr := "127.0.0.1:18807"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock UPF
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
			case message.MsgTypeSessionModificationRequest:
				resp = message.NewSessionModificationResponse(
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

	// Create async client
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect
	err = client.ConnectN4Async("127.0.0.1:18807")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	client.isAssociationActive = true
	time.Sleep(50 * time.Millisecond)

	// Send async session modification
	fars := []*ieLib.IE{ieLib.NewUpdateFAR(ieLib.NewFARID(1))}

	op, err := client.ModifySessionAsync(1, 9999, nil, fars, nil, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to send modification request: %v", err)
	}

	// Process result
	session, err := client.ProcessSessionResult(op)
	if err != nil {
		t.Fatalf("Failed to process result: %v", err)
	}

	if session != nil {
		t.Error("Expected nil session for modify operation")
	}

	t.Log("Session modified successfully")
}

// TestAsyncSessionDeletionFlow tests the async session deletion flow
func TestAsyncSessionDeletionFlow(t *testing.T) {
	// Create mock UPF server
	serverAddr := "127.0.0.1:18808"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock UPF
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

	// Create async client
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect
	err = client.ConnectN4Async("127.0.0.1:18808")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	client.isAssociationActive = true
	time.Sleep(50 * time.Millisecond)

	// Send async session deletion
	op, err := client.DeleteSessionAsync(1, 1234, 9999, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to send deletion request: %v", err)
	}

	// Process result
	session, err := client.ProcessSessionResult(op)
	if err != nil {
		t.Fatalf("Failed to process result: %v", err)
	}

	if session != nil {
		t.Error("Expected nil session for delete operation")
	}

	t.Log("Session deleted successfully")
}

// TestAsyncMultipleConcurrentSessions tests multiple sessions concurrently
func TestAsyncMultipleConcurrentSessions(t *testing.T) {
	// Create mock UPF server
	serverAddr := "127.0.0.1:18809"
	serverConn, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer serverConn.Close()

	// Start mock UPF
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

	// Create async client
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect
	err = client.ConnectN4Async("127.0.0.1:18809")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	client.isAssociationActive = true
	time.Sleep(50 * time.Millisecond)

	// Send multiple session establishments concurrently
	numSessions := 10
	ops := make([]*SessionOperation, numSessions)

	for i := 0; i < numSessions; i++ {
		pdrs := []*ieLib.IE{ieLib.NewCreatePDR(ieLib.NewPDRID(uint16(i + 1)))}
		fars := []*ieLib.IE{ieLib.NewCreateFAR(ieLib.NewFARID(uint32(i + 1)))}
		qers := []*ieLib.IE{ieLib.NewCreateQER(ieLib.NewQERID(uint32(i + 1)))}

		op, err := client.EstablishSessionAsync(i+1, pdrs, fars, qers, nil, 2*time.Second)
		if err != nil {
			t.Fatalf("Failed to send establishment request %d: %v", i, err)
		}
		ops[i] = op
	}

	// Batch process results
	sessions, errors := client.BatchProcessSessionResults(ops)

	// Verify all succeeded
	successCount := 0
	for i, err := range errors {
		if err != nil {
			t.Errorf("Session %d failed: %v", i, err)
		} else {
			successCount++
			if sessions[i] == nil {
				t.Errorf("Session %d: expected non-nil session", i)
			}
		}
	}

	t.Logf("Successfully established %d/%d sessions concurrently", successCount, numSessions)
}