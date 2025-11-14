// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"testing"
	"time"
)

// TestDoubleTimeout tests what happens if timeout fires twice (shouldn't happen)
func TestDoubleTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[100] = &PendingRequest{
		SeqNum:     100,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
		Timeout:    nil,
	}
	client.pendingRequestsLock.Unlock()

	// First timeout
	client.handleTimeout(100)

	// Second timeout (should be no-op)
	client.handleTimeout(100)

	// Verify only one error was sent
	errorCount := 0
	timeout := time.After(100 * time.Millisecond)
	
	for {
		select {
case <-responseCh:
			errorCount++
		case <-timeout:
			goto done
		}
	}
done:

	if errorCount != 1 {
		t.Errorf("Expected 1 error, got %d", errorCount)
	}

	// Verify request was only removed once
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}
}

// TestResponseAfterTimeout tests receiving a response after timeout fired
func TestResponseAfterTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[200] = &PendingRequest{
		SeqNum:     200,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
		Timeout:    nil,
	}
	client.pendingRequestsLock.Unlock()

	// Timeout fires first
	client.handleTimeout(200)

	// Verify timeout error received
	select {
	case result := <-responseCh:
		if result.Error == nil {
			t.Error("Expected timeout error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for error")
	}

	// Now a late response arrives
	resp := message.NewHeartbeatResponse(200, nil)
	client.routeResponse(resp)

	// Should be logged as unexpected but not crash
	// Verify stats show error
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors 1 for unexpected response, got %d", snapshot.TotalErrors)
	}
}

// TestTimeoutBeforeResponse tests race between timeout and response
func TestTimeoutBeforeResponse(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[300] = &PendingRequest{
		SeqNum:      300,
		MessageType: message.MsgTypeHeartbeatRequest,
		SentAt:      time.Now(),
		ResponseCh:  responseCh,
		Timeout:     nil,
	}
	client.pendingRequestsLock.Unlock()

	// Response arrives first
	resp := message.NewHeartbeatResponse(300, nil)
	client.routeResponse(resp)

	// Verify response received
	select {
	case result := <-responseCh:
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for response")
	}

	// Now timeout fires (should be no-op)
	client.handleTimeout(300)

	// Verify no additional error sent
	select {
	case <-responseCh:
		t.Error("Unexpected message on channel after timeout")
	case <-time.After(50 * time.Millisecond):
		// Expected - channel should be closed and empty
	}
}

// TestZeroTimeout tests SendAsync with zero timeout (should use default)
func TestZeroTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")
	client.SetDefaultTimeout(100 * time.Millisecond)

	// Mock message
	req := message.NewHeartbeatRequest(0, nil)

	// SendAsync with zero timeout (should not error on timeout value)
	// Will fail on send since not connected, but that's ok for this test
	_, err := client.SendAsync(req, 0)
	if err == nil {
		t.Error("Expected send error since not connected")
	}

	// The test is that it didn't panic on zero timeout
}

// TestVeryShortTimeout tests extremely short timeout
func TestVeryShortTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	responseCh := make(chan ResponseResult, 1)
	
	pending := &PendingRequest{
		SeqNum:     400,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
	}
	
	// Set 1 nanosecond timeout (will fire immediately)
	pending.Timeout = time.AfterFunc(1*time.Nanosecond, func() {
		client.handleTimeout(400)
	})

	client.pendingRequestsLock.Lock()
	client.pendingRequests[400] = pending
	client.pendingRequestsLock.Unlock()

	// Should timeout almost immediately
	select {
	case result := <-responseCh:
		if result.Error == nil {
			t.Error("Expected timeout error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout didn't fire")
	}
}

// TestNilResponseChannel tests handling of nil response channel (shouldn't happen)
func TestNilResponseChannel(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add pending request with nil channel (bad state)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[500] = &PendingRequest{
		SeqNum:     500,
		SentAt:     time.Now(),
		ResponseCh: nil, // Bad!
		Timeout:    nil,
	}
	client.pendingRequestsLock.Unlock()

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic occurred: %v", r)
		}
	}()

	client.handleTimeout(500)
}

// TestClosedResponseChannel tests sending to already closed channel
func TestClosedResponseChannel(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	responseCh := make(chan ResponseResult, 1)
	close(responseCh) // Close before using

	client.pendingRequestsLock.Lock()
	client.pendingRequests[600] = &PendingRequest{
		SeqNum:     600,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
		Timeout:    nil,
	}
	client.pendingRequestsLock.Unlock()

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic occurred: %v", r)
		}
	}()

	client.handleTimeout(600)

	// Should log error but not crash
	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalTimeout != 1 {
		t.Errorf("Expected timeout to be counted")
	}
}

// TestCleanupWithActiveTimeouts tests cleanup while timeouts are active
func TestCleanupWithActiveTimeouts(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add requests with long timeouts
	for i := 0; i < 10; i++ {
		responseCh := make(chan ResponseResult, 1)
		pending := &PendingRequest{
			SeqNum:     uint32(i),
			SentAt:     time.Now(),
			ResponseCh: responseCh,
			Timeout:    time.NewTimer(10 * time.Second), // Long timeout
		}
		client.pendingRequestsLock.Lock()
		client.pendingRequests[uint32(i)] = pending
		client.pendingRequestsLock.Unlock()
	}

	// Cleanup all
	client.CleanupPendingRequests()

	// Verify all cleaned
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after cleanup, got %d", count)
	}

	// Wait a bit to ensure no timeouts fire
	time.Sleep(100 * time.Millisecond)

	// Verify state is clean
	snapshot := client.stats.GetSnapshot()
	t.Logf("After cleanup: %+v", snapshot)
}

// TestSequenceNumberOverflow tests sequence number wrapping
func TestSequenceNumberOverflow(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Set sequence number near max uint32
	client.sequenceNumber = ^uint32(0) - 5

	// Get several sequence numbers
	for i := 0; i < 10; i++ {
		seqNum := client.getNextSequenceNumber()
		t.Logf("Sequence number %d: %d", i, seqNum)
		
		// Should wrap around after max
		if i > 5 {
			// After wrapping, should be small numbers
			if seqNum > 10 {
				t.Errorf("Expected small sequence number after wrap, got %d", seqNum)
			}
		}
	}
}

// TestGetPendingRequestsInfoEmpty tests getting info with no pending requests
func TestGetPendingRequestsInfoEmpty(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	infos := client.GetPendingRequestsInfo()
	if len(infos) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(infos))
	}
}

// TestStatsReset tests resetting statistics
func TestStatsReset(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Generate some stats
	client.stats.incrementSent()
	client.stats.incrementReceived()
	client.stats.incrementTimeout()
	client.stats.recordLatency(100 * time.Millisecond)

	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalSent == 0 {
		t.Error("Expected non-zero stats before reset")
	}

	// Reset
	client.stats.Reset()

	snapshot = client.stats.GetSnapshot()
	if snapshot.TotalSent != 0 || snapshot.TotalReceived != 0 || 
	   snapshot.TotalTimeout != 0 || snapshot.AverageLatency != 0 {
		t.Error("Stats not properly reset")
	}
}