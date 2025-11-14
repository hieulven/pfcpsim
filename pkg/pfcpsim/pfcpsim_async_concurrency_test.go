// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/message"
)

// TestConcurrentSendAsync tests multiple goroutines sending requests concurrently
func TestConcurrentSendAsync(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	numGoroutines := 50
	requestsPerGoroutine := 20

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*requestsPerGoroutine)

	// Launch multiple goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				// Create a mock message (won't actually send without connection)
				req := message.NewHeartbeatRequest(
					0,
					nil,
				)

				// This will fail to send since we're not connected,
				// but we're testing the concurrent access to data structures
				_, err := client.SendAsync(req, 1*time.Second)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors (other than expected send failures)
	errorCount := 0
	for err := range errors {
		errorCount++
		t.Logf("Error: %v", err)
	}

	// We expect errors since we're not connected, but shouldn't panic or deadlock
	t.Logf("Total errors from %d concurrent requests: %d", numGoroutines*requestsPerGoroutine, errorCount)

	// Verify data structures are still consistent
	pendingCount := client.GetPendingRequestCount()
	t.Logf("Pending requests after concurrent test: %d", pendingCount)
}

// TestConcurrentTimeouts tests multiple timeouts firing simultaneously
func TestConcurrentTimeouts(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	numRequests := 100
	channels := make([]chan ResponseResult, numRequests)

	// Add many pending requests
	for i := 0; i < numRequests; i++ {
		channels[i] = make(chan ResponseResult, 1)
		client.pendingRequestsLock.Lock()
		client.pendingRequests[uint32(i)] = &PendingRequest{
			SeqNum:     uint32(i),
			SentAt:     time.Now(),
			ResponseCh: channels[i],
			Timeout:    nil, // Will trigger manually
		}
		client.pendingRequestsLock.Unlock()
	}

	// Trigger all timeouts concurrently
	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(seqNum uint32) {
			defer wg.Done()
			client.handleTimeout(seqNum)
		}(uint32(i))
	}

	wg.Wait()

	// Verify all requests were removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}

	// Verify all channels received errors
	for i, ch := range channels {
		select {
		case result := <-ch:
			if result.Error == nil {
				t.Errorf("Channel %d: Expected error, got nil", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Channel %d: Timeout waiting for error", i)
		}
	}
}

// TestConcurrentResponseRouting tests concurrent response routing
func TestConcurrentResponseRouting(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	numResponses := 100
	channels := make([]chan ResponseResult, numResponses)

	// Add pending requests
	for i := 0; i < numResponses; i++ {
		channels[i] = make(chan ResponseResult, 1)
		client.pendingRequestsLock.Lock()
		client.pendingRequests[uint32(i)] = &PendingRequest{
			SeqNum:      uint32(i),
			MessageType: message.MsgTypeHeartbeatRequest,
			SentAt:      time.Now(),
			ResponseCh:  channels[i],
			Timeout:     time.NewTimer(10 * time.Second),
		}
		client.pendingRequestsLock.Unlock()
	}

	// Route responses concurrently
	var wg sync.WaitGroup
	for i := 0; i < numResponses; i++ {
		wg.Add(1)
		go func(seqNum uint32) {
			defer wg.Done()
			
			// Create mock response
			resp := message.NewHeartbeatResponse(seqNum, nil)
			client.routeResponse(resp)
		}(uint32(i))
	}

	wg.Wait()

	// Verify all responses were delivered
	for i, ch := range channels {
		select {
		case result := <-ch:
			if result.Error != nil {
				t.Errorf("Channel %d: Unexpected error: %v", i, result.Error)
			}
			if result.Message == nil {
				t.Errorf("Channel %d: Expected message, got nil", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Channel %d: Timeout waiting for response", i)
		}
	}

	// Verify all requests were removed
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests, got %d", count)
	}
}

// TestConcurrentMixedOperations tests mixed operations happening concurrently
func TestConcurrentMixedOperations(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	duration := 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine 1: Add requests
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				responseCh := make(chan ResponseResult, 1)
				seqNum := client.getNextSequenceNumber()
				client.pendingRequestsLock.Lock()
				client.pendingRequests[seqNum] = &PendingRequest{
					SeqNum:     seqNum,
					SentAt:     time.Now(),
					ResponseCh: responseCh,
					Timeout:    time.NewTimer(5 * time.Second),
				}
				client.pendingRequestsLock.Unlock()
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Route responses
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				ids := client.GetPendingRequestIDs()
				if len(ids) > 0 {
					resp := message.NewHeartbeatResponse(ids[0], nil)
					client.routeResponse(resp)
				}
				time.Sleep(15 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Trigger timeouts
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				ids := client.GetPendingRequestIDs()
				if len(ids) > 0 {
					client.handleTimeout(ids[len(ids)-1])
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Check stats
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_ = client.stats.GetSnapshot()
				_ = client.GetPendingRequestCount()
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	wg.Wait()

	// Final cleanup
	client.CleanupPendingRequests()

	t.Logf("Final stats: %+v", client.stats.GetSnapshot())
	t.Logf("Final pending count: %d", client.GetPendingRequestCount())
}

// TestStressLoadAsync tests the client under heavy load
func TestStressLoadAsync(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	client := NewAsyncPFCPClient("127.0.0.1")
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	numRequests := 10000
	timeout := 100 * time.Millisecond

	start := time.Now()

	// Add many requests rapidly
	for i := 0; i < numRequests; i++ {
		responseCh := make(chan ResponseResult, 1)
		
		pending := &PendingRequest{
			SeqNum:     uint32(i),
			SentAt:     time.Now(),
			ResponseCh: responseCh,
		}
		
		pending.Timeout = time.AfterFunc(timeout, func() {
			client.handleTimeout(uint32(i))
		})

		client.pendingRequestsLock.Lock()
		client.pendingRequests[uint32(i)] = pending
		client.pendingRequestsLock.Unlock()
	}

	elapsed := time.Since(start)
	t.Logf("Added %d requests in %v (%.0f req/s)", numRequests, elapsed, float64(numRequests)/elapsed.Seconds())

	// Wait for all timeouts to fire
	time.Sleep(timeout + 100*time.Millisecond)

	// Verify all were cleaned up
	if count := client.GetPendingRequestCount(); count != 0 {
		t.Errorf("Expected 0 pending requests after timeouts, got %d", count)
	}

	snapshot := client.stats.GetSnapshot()
	if snapshot.TotalTimeout != int64(numRequests) {
		t.Errorf("Expected %d timeouts, got %d", numRequests, snapshot.TotalTimeout)
	}

	t.Logf("Stress test completed: %+v", snapshot)
}