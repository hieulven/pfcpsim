// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"net"
	"testing"
	"time"

	ieLib "github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

func TestIsExpectedResponseType(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	tests := []struct {
		name         string
		requestType  uint8
		responseType uint8
		expected     bool
	}{
		{
			name:         "Association Setup - Correct",
			requestType:  message.MsgTypeAssociationSetupRequest,
			responseType: message.MsgTypeAssociationSetupResponse,
			expected:     true,
		},
		{
			name:         "Association Setup - Incorrect",
			requestType:  message.MsgTypeAssociationSetupRequest,
			responseType: message.MsgTypeSessionEstablishmentResponse,
			expected:     false,
		},
		{
			name:         "Session Establishment - Correct",
			requestType:  message.MsgTypeSessionEstablishmentRequest,
			responseType: message.MsgTypeSessionEstablishmentResponse,
			expected:     true,
		},
		{
			name:         "Session Modification - Correct",
			requestType:  message.MsgTypeSessionModificationRequest,
			responseType: message.MsgTypeSessionModificationResponse,
			expected:     true,
		},
		{
			name:         "Session Deletion - Correct",
			requestType:  message.MsgTypeSessionDeletionRequest,
			responseType: message.MsgTypeSessionDeletionResponse,
			expected:     true,
		},
		{
			name:         "Heartbeat - Correct",
			requestType:  message.MsgTypeHeartbeatRequest,
			responseType: message.MsgTypeHeartbeatResponse,
			expected:     true,
		},
		{
			name:         "Association Release - Correct",
			requestType:  message.MsgTypeAssociationReleaseRequest,
			responseType: message.MsgTypeAssociationReleaseResponse,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isExpectedResponseType(tt.requestType, tt.responseType)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for request type %d and response type %d",
					tt.expected, result, tt.requestType, tt.responseType)
			}
		})
	}
}

func TestRouteResponse(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create a mock response message
	response := message.NewSessionEstablishmentResponse(
		0, 0, 0,
		123, // sequence number
		0,
		ieLib.NewNodeID("192.168.1.1", "", ""),
		ieLib.NewCause(ieLib.CauseRequestAccepted),
		ieLib.NewFSEID(1, net.ParseIP("192.168.1.1"), nil),
	)

	// Add a pending request
	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[123] = &PendingRequest{
		SeqNum:      123,
		MessageType: message.MsgTypeSessionEstablishmentRequest,
		SentAt:      time.Now(),
		ResponseCh:  responseCh,
		Timeout:     time.NewTimer(5 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Route the response
	client.routeResponse(response)

	// Check that response was delivered
	select {
	case result := <-responseCh:
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if result.Message == nil {
			t.Error("Expected message, got nil")
		}
		if result.SeqNum != 123 {
			t.Errorf("Expected SeqNum 123, got %d", result.SeqNum)
		}
		if result.Message.MessageType() != message.MsgTypeSessionEstablishmentResponse {
			t.Errorf("Expected SessionEstablishmentResponse, got %d", result.Message.MessageType())
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for response")
	}

	// Verify pending request was removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}

	// Verify stats were updated
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalReceived != 1 {
		t.Errorf("Expected TotalReceived 1, got %d", snapshot.TotalReceived)
	}
}

func TestRouteResponseUnexpected(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create a mock response with no pending request
	response := message.NewSessionEstablishmentResponse(
		0, 0, 0,
		999, // sequence number not in pending map
		0,
		ieLib.NewNodeID("192.168.1.1", "", ""),
		ieLib.NewCause(ieLib.CauseRequestAccepted),
		ieLib.NewFSEID(1, net.ParseIP("192.168.1.1"), nil),
	)

	// Route the response (should not panic)
	client.routeResponse(response)

	// Verify error was counted
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors 1, got %d", snapshot.TotalErrors)
	}

	// Verify no crash occurred
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}
}

func TestRouteResponseTypeMismatch(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create a response that doesn't match the request type
	response := message.NewSessionDeletionResponse(
		0, 0, 0,
		123, // sequence number
		0,
		ieLib.NewCause(ieLib.CauseRequestAccepted),
	)

	// Add a pending request with different type
	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[123] = &PendingRequest{
		SeqNum:      123,
		MessageType: message.MsgTypeSessionEstablishmentRequest, // Mismatch!
		SentAt:      time.Now(),
		ResponseCh:  responseCh,
		Timeout:     time.NewTimer(5 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Route the response (should still deliver despite mismatch)
	client.routeResponse(response)

	// Check that response was still delivered
	select {
	case result := <-responseCh:
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if result.Message == nil {
			t.Error("Expected message, got nil")
		}
		// Message should be delivered despite type mismatch
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for response")
	}
}

func TestRouteMessage(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")
	client.heartbeatsChan = make(chan *message.HeartbeatResponse, 10)

	// Test heartbeat response routing
	t.Run("Heartbeat Response", func(t *testing.T) {
		hbResponse := message.NewHeartbeatResponse(
			1,
			ieLib.NewRecoveryTimeStamp(time.Now()),
		)

		client.routeMessage(hbResponse)

		// Check heartbeat channel
		select {
		case msg := <-client.heartbeatsChan:
			if msg == nil {
				t.Error("Expected heartbeat response, got nil")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("Timeout waiting for heartbeat response")
		}
	})

	// Test regular response routing
	t.Run("Regular Response", func(t *testing.T) {
		response := message.NewSessionEstablishmentResponse(
			0, 0, 0,
			456,
			0,
			ieLib.NewNodeID("192.168.1.1", "", ""),
			ieLib.NewCause(ieLib.CauseRequestAccepted),
			ieLib.NewFSEID(1, net.ParseIP("192.168.1.1"), nil),
		)

		// Add pending request
		responseCh := make(chan ResponseResult, 1)
		client.pendingRequestsLock.Lock()
		client.pendingRequests[456] = &PendingRequest{
			SeqNum:      456,
			MessageType: message.MsgTypeSessionEstablishmentRequest,
			SentAt:      time.Now(),
			ResponseCh:  responseCh,
			Timeout:     time.NewTimer(5 * time.Second),
		}
		client.pendingRequestsLock.Unlock()

		client.routeMessage(response)

		// Check response was delivered
		select {
		case result := <-responseCh:
			if result.Error != nil {
				t.Errorf("Expected no error, got %v", result.Error)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("Timeout waiting for response")
		}
	})
}

func TestRouteResponseLatencyTracking(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create a response
	response := message.NewSessionEstablishmentResponse(
		0, 0, 0,
		789,
		0,
		ieLib.NewNodeID("192.168.1.1", "", ""),
		ieLib.NewCause(ieLib.CauseRequestAccepted),
		ieLib.NewFSEID(1, net.ParseIP("192.168.1.1"), nil),
	)

	// Add pending request with known send time
	sentTime := time.Now().Add(-100 * time.Millisecond) // 100ms ago
	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[789] = &PendingRequest{
		SeqNum:      789,
		MessageType: message.MsgTypeSessionEstablishmentRequest,
		SentAt:      sentTime,
		ResponseCh:  responseCh,
		Timeout:     time.NewTimer(5 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Route the response
	client.routeResponse(response)

	// Check latency was recorded
	select {
	case result := <-responseCh:
		if result.Latency <= 0 {
			t.Error("Expected positive latency")
		}
		// Latency should be close to 100ms (with some tolerance)
		if result.Latency < 50*time.Millisecond || result.Latency > 150*time.Millisecond {
			t.Errorf("Expected latency around 100ms, got %v", result.Latency)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for response")
	}

	// Check stats were updated with latency
	snapshot := client.stats.GetSnapshot()
	if snapshot.AverageLatency <= 0 {
		t.Error("Expected positive average latency in stats")
	}
}

func TestRouteResponseChannelClosed(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create a response
	response := message.NewSessionEstablishmentResponse(
		0, 0, 0,
		555,
		0,
		ieLib.NewNodeID("192.168.1.1", "", ""),
		ieLib.NewCause(ieLib.CauseRequestAccepted),
		ieLib.NewFSEID(1, net.ParseIP("192.168.1.1"), nil),
	)

	// Add pending request with closed channel
	responseCh := make(chan ResponseResult, 1)
	close(responseCh) // Close it before routing

	client.pendingRequestsLock.Lock()
	client.pendingRequests[555] = &PendingRequest{
		SeqNum:      555,
		MessageType: message.MsgTypeSessionEstablishmentRequest,
		SentAt:      time.Now(),
		ResponseCh:  responseCh,
		Timeout:     time.NewTimer(5 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Route the response (should not panic)
	client.routeResponse(response)

	// Verify error was counted
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors 1, got %d", snapshot.TotalErrors)
	}
}