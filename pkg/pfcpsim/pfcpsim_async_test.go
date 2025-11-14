// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/message"
)

func TestNewAsyncPFCPClient(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	if client == nil {
		t.Fatal("NewAsyncPFCPClient returned nil")
	}

	if client.PFCPClient == nil {
		t.Error("Embedded PFCPClient is nil")
	}

	if client.pendingRequests == nil {
		t.Error("pendingRequests map is nil")
	}

	if client.defaultTimeout != 5*time.Second {
		t.Errorf("Expected default timeout 5s, got %v", client.defaultTimeout)
	}

	if cap(client.responseChan) != 1000 {
		t.Errorf("Expected response channel capacity 1000, got %d", cap(client.responseChan))
	}
}

func TestSetDefaultTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	newTimeout := 10 * time.Second
	client.SetDefaultTimeout(newTimeout)

	if client.GetDefaultTimeout() != newTimeout {
		t.Errorf("Expected timeout %v, got %v", newTimeout, client.GetDefaultTimeout())
	}
}

func TestGetPendingRequestCount(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}

	// Add a pending request manually for testing
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum:     1,
		SentAt:     time.Now(),
		ResponseCh: make(chan ResponseResult, 1),
	}
	client.pendingRequestsLock.Unlock()

	if count := client.GetPendingRequestCount(); count != 1 {
		t.Errorf("Expected 1 pending request, got %d", count)
	}
}

func TestGetPendingRequestIDs(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add some pending requests
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{SeqNum: 1, ResponseCh: make(chan ResponseResult, 1)}
	client.pendingRequests[5] = &PendingRequest{SeqNum: 5, ResponseCh: make(chan ResponseResult, 1)}
	client.pendingRequests[10] = &PendingRequest{SeqNum: 10, ResponseCh: make(chan ResponseResult, 1)}
	client.pendingRequestsLock.Unlock()

	ids := client.GetPendingRequestIDs()

	if len(ids) != 3 {
		t.Errorf("Expected 3 IDs, got %d", len(ids))
	}

	// Check that all expected IDs are present
	idMap := make(map[uint32]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, expectedID := range []uint32{1, 5, 10} {
		if !idMap[expectedID] {
			t.Errorf("Expected ID %d not found in result", expectedID)
		}
	}
}

func TestRemovePendingRequest(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add a pending request
	timer := time.NewTimer(10 * time.Second)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum:     1,
		SentAt:     time.Now(),
		ResponseCh: make(chan ResponseResult, 1),
		Timeout:    timer,
	}
	client.pendingRequestsLock.Unlock()

	// Remove it
	pending := client.removePendingRequest(1)

	if pending == nil {
		t.Fatal("removePendingRequest returned nil")
	}

	if pending.SeqNum != 1 {
		t.Errorf("Expected SeqNum 1, got %d", pending.SeqNum)
	}

	// Verify it's removed from map
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after removal, got %d", count)
	}

	// Remove non-existent request
	pending = client.removePendingRequest(999)
	if pending != nil {
		t.Error("Expected nil when removing non-existent request")
	}
}

func TestHandleTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add a pending request
	responseCh := make(chan ResponseResult, 1)
	client.pendingRequestsLock.Lock()
	client.pendingRequests[1] = &PendingRequest{
		SeqNum:     1,
		SentAt:     time.Now(),
		ResponseCh: responseCh,
		Timeout:    nil, // Don't set a real timer for this test
	}
	client.pendingRequestsLock.Unlock()

	// Trigger timeout
	client.handleTimeout(1)

	// Check that request was removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after timeout, got %d", count)
	}

	// Check that error was sent
	select {
	case result := <-responseCh:
		if result.Error == nil {
			t.Error("Expected timeout error, got nil")
		}
		if result.Message != nil {
			t.Error("Expected nil message on timeout")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for error response")
	}

	// Test timeout on non-existent request (should not panic)
	client.handleTimeout(999)
}

func TestCleanupPendingRequests(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Add multiple pending requests
	channels := make([]chan ResponseResult, 3)
	for i := 0; i < 3; i++ {
		channels[i] = make(chan ResponseResult, 1)
		client.pendingRequestsLock.Lock()
		client.pendingRequests[uint32(i+1)] = &PendingRequest{
			SeqNum:     uint32(i + 1),
			SentAt:     time.Now(),
			ResponseCh: channels[i],
			Timeout:    time.NewTimer(10 * time.Second),
		}
		client.pendingRequestsLock.Unlock()
	}

	// Cleanup
	client.CleanupPendingRequests()

	// Verify all requests removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after cleanup, got %d", count)
	}

	// Verify all channels received errors
	for i, ch := range channels {
		select {
		case result := <-ch:
			if result.Error == nil {
				t.Errorf("Channel %d: Expected error, got nil", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Channel %d: Timeout waiting for cleanup error", i)
		}
	}
}

func TestAsyncStats(t *testing.T) {
	stats := &AsyncStats{}

	// Test initial values
	snapshot := stats.GetSnapshot()
	if snapshot.TotalSent != 0 {
		t.Errorf("Expected TotalSent 0, got %d", snapshot.TotalSent)
	}

	// Test increment operations
	stats.incrementSent()
	stats.incrementSent()
	stats.incrementReceived()
	stats.incrementTimeout()
	stats.incrementErrors()

	snapshot = stats.GetSnapshot()
	if snapshot.TotalSent != 2 {
		t.Errorf("Expected TotalSent 2, got %d", snapshot.TotalSent)
	}
	if snapshot.TotalReceived != 1 {
		t.Errorf("Expected TotalReceived 1, got %d", snapshot.TotalReceived)
	}
	if snapshot.TotalTimeout != 1 {
		t.Errorf("Expected TotalTimeout 1, got %d", snapshot.TotalTimeout)
	}
	if snapshot.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors 1, got %d", snapshot.TotalErrors)
	}

	// Test latency recording
	stats.recordLatency(100 * time.Millisecond)
	snapshot = stats.GetSnapshot()
	if snapshot.AverageLatency != 100*time.Millisecond {
		t.Errorf("Expected AverageLatency 100ms, got %v", snapshot.AverageLatency)
	}

	// Second latency sample
	stats.recordLatency(200 * time.Millisecond)
	snapshot = stats.GetSnapshot()
	// With alpha=0.1: 100*(1-0.1) + 200*0.1 = 90 + 20 = 110ms
	expected := 110 * time.Millisecond
	if snapshot.AverageLatency != expected {
		t.Errorf("Expected AverageLatency %v, got %v", expected, snapshot.AverageLatency)
	}

	// Test reset
	stats.Reset()
	snapshot = stats.GetSnapshot()
	if snapshot.TotalSent != 0 || snapshot.AverageLatency != 0 {
		t.Error("Stats not properly reset")
	}
}

func TestAsyncStatsSnapshot(t *testing.T) {
	snapshot := AsyncStatsSnapshot{
		TotalSent:     100,
		TotalReceived: 90,
		TotalTimeout:  5,
		TotalErrors:   3,
	}

	// Test success rate
	successRate := snapshot.SuccessRate()
	expected := 90.0
	if successRate != expected {
		t.Errorf("Expected success rate %.1f%%, got %.1f%%", expected, successRate)
	}

	// Test timeout rate
	timeoutRate := snapshot.TimeoutRate()
	expected = 5.0
	if timeoutRate != expected {
		t.Errorf("Expected timeout rate %.1f%%, got %.1f%%", expected, timeoutRate)
	}

	// Test pending count
	pendingCount := snapshot.PendingCount()
	expected64 := int64(2) // 100 - 90 - 5 - 3
	if pendingCount != expected64 {
		t.Errorf("Expected pending count %d, got %d", expected64, pendingCount)
	}

	// Test with zero sent (avoid division by zero)
	emptySnapshot := AsyncStatsSnapshot{}
	if rate := emptySnapshot.SuccessRate(); rate != 0.0 {
		t.Errorf("Expected success rate 0%% for empty stats, got %.1f%%", rate)
	}
}