// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/message"
)

func TestCancelPendingRequest(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add a pending request
	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[100] = &PendingRequest{
		SeqNum:      100,
		MessageType: message.MsgTypeSessionEstablishmentRequest,
		SentAt:      time.Now(),
		ResponseCh:  responseCh,
		Timeout:     time.NewTimer(10 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Cancel it
	cancelled := client.CancelPendingRequest(100)
	if !cancelled {
		t.Error("Expected cancellation to succeed")
	}

	// Verify it was removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}

	// Verify error was sent
	select {
	case result := <-responseCh:
		if result.Error == nil {
			t.Error("Expected error on cancellation")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for cancellation error")
	}

	// Try to cancel non-existent request
	cancelled = client.CancelPendingRequest(999)
	if cancelled {
		t.Error("Expected cancellation to fail for non-existent request")
	}
}

func TestGetOldestPendingRequest(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// No pending requests
	oldest := client.GetOldestPendingRequest()
	if oldest != nil {
		t.Error("Expected nil for no pending requests")
	}

	// Add requests with different times
	now := time.Now()
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum: 1,
		SentAt: now.Add(-5 * time.Second),
		ResponseCh: make(chan ResponseResult, 1),
	}
	client.pendingRequests[2] = &PendingRequest{
		SeqNum: 2,
		SentAt: now.Add(-10 * time.Second), // Oldest
		ResponseCh: make(chan ResponseResult, 1),
	}
	client.pendingRequests[3] = &PendingRequest{
		SeqNum: 3,
		SentAt: now.Add(-2 * time.Second),
		ResponseCh: make(chan ResponseResult, 1),
	}
	client.pendingRequestsLock.Unlock()

	// Get oldest
	oldest = client.GetOldestPendingRequest()
	if oldest == nil {
		t.Fatal("Expected oldest request info")
	}

	if oldest.SeqNum != 2 {
		t.Errorf("Expected oldest SeqNum 2, got %d", oldest.SeqNum)
	}

	if oldest.Age < 9*time.Second || oldest.Age > 11*time.Second {
		t.Errorf("Expected age around 10s, got %v", oldest.Age)
	}
}

func TestGetPendingRequestsInfo(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add some requests
	now := time.Now()
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum:      1,
		MessageType: message.MsgTypeSessionEstablishmentRequest,
		SentAt:      now.Add(-1 * time.Second),
		ResponseCh:  make(chan ResponseResult, 1),
	}
	client.pendingRequests[2] = &PendingRequest{
		SeqNum:      2,
		MessageType: message.MsgTypeSessionModificationRequest,
		SentAt:      now.Add(-2 * time.Second),
		ResponseCh:  make(chan ResponseResult, 1),
	}
	client.pendingRequestsLock.Unlock()

	// Get info
	infos := client.GetPendingRequestsInfo()

	if len(infos) != 2 {
		t.Errorf("Expected 2 infos, got %d", len(infos))
	}

	// Verify info contains expected data
	foundSeq1 := false
	foundSeq2 := false
	for _, info := range infos {
		if info.SeqNum == 1 {
			foundSeq1 = true
			if info.MessageType != message.MsgTypeSessionEstablishmentRequest {
				t.Error("Wrong message type for SeqNum 1")
			}
		}
		if info.SeqNum == 2 {
			foundSeq2 = true
			if info.MessageType != message.MsgTypeSessionModificationRequest {
				t.Error("Wrong message type for SeqNum 2")
			}
		}
	}

	if !foundSeq1 || !foundSeq2 {
		t.Error("Missing expected sequence numbers in info")
	}
}

func TestCleanupStalePendingRequests(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add requests with different ages
	now := time.Now()
	
	// Fresh request
	freshCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum:     1,
		SentAt:     now.Add(-1 * time.Second),
		ResponseCh: freshCh,
		Timeout:    time.NewTimer(10 * time.Second),
	}
	
	// Stale request (6 seconds old)
	staleCh := make(chan ResponseResult, 1)
	client.pendingRequests[2] = &PendingRequest{
		SeqNum:     2,
		SentAt:     now.Add(-6 * time.Second),
		ResponseCh: staleCh,
		Timeout:    time.NewTimer(10 * time.Second),
	}
	
	// Very stale request (11 seconds old)
	veryStaleCh := make(chan ResponseResult, 1)
	client.pendingRequests[3] = &PendingRequest{
		SeqNum:     3,
		SentAt:     now.Add(-11 * time.Second),
		ResponseCh: veryStaleCh,
		Timeout:    time.NewTimer(10 * time.Second),
	}
	client.pendingRequestsLock.Unlock()

	// Cleanup requests older than 5 seconds
	cleaned := client.CleanupStalePendingRequests(5 * time.Second)

	if cleaned != 2 {
		t.Errorf("Expected 2 stale requests cleaned, got %d", cleaned)
	}

	// Verify only fresh request remains
	if count := client.GetPendingRequestCount(); count != 1 {
		t.Errorf("Expected 1 remaining request, got %d", count)
	}

	// Verify fresh request is still there
	c.pendingRequestsLock.RLock()
	_, exists := client.pendingRequests[1]
	client.pendingRequestsLock.RUnlock()
	if !exists {
		t.Error("Fresh request was incorrectly removed")
	}

	// Verify stale requests received errors
	select {
	case result := <-staleCh:
		if result.Error == nil {
			t.Error("Expected error for stale request")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for stale request error")
	}

	select {
	case result := <-veryStaleCh:
		if result.Error == nil {
			t.Error("Expected error for very stale request")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for very stale request error")
	}
}

func TestTimeoutActuallyFires(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add a pending request with very short timeout
	responseCh := make(chan ResponseResult, 1)
	timeout := 100 * time.Millisecond
	
	client.pendingRequestsLock.Lock()
	pending := &PendingRequest{
		SeqNum:     999,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
	}
	pending.Timeout = time.AfterFunc(timeout, func() {
		client.handleTimeout(999)
	})
	client.pendingRequests[999] = pending
	client.pendingRequestsLock.Unlock()

	// Wait for timeout to fire
	select {
	case result := <-responseCh:
		if result.Error == nil {
			t.Error("Expected timeout error")
		}
		if result.SeqNum != 999 {
			t.Errorf("Expected SeqNum 999, got %d", result.SeqNum)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timeout didn't fire within expected time")
	}

	// Verify request was removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after timeout, got %d", count)
	}

	// Verify stats
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalTimeout != 1 {
		t.Errorf("Expected TotalTimeout 1, got %d", snapshot.TotalTimeout)
	}
}