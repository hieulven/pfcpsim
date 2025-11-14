// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
)

func TestTaskTypeString(t *testing.T) {
	tests := []struct {
		taskType TaskType
		expected string
	}{
		{TaskCreate, "CREATE"},
		{TaskModify, "MODIFY"},
		{TaskDelete, "DELETE"},
		{TaskType(999), "UNKNOWN(999)"},
	}

	for _, tt := range tests {
		if got := tt.taskType.String(); got != tt.expected {
			t.Errorf("TaskType.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestNewWorker(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	if worker == nil {
		t.Fatal("NewWorker returned nil")
	}

	if worker.id != 1 {
		t.Errorf("Expected worker ID 1, got %d", worker.id)
	}

	if worker.client != client {
		t.Error("Worker client not set correctly")
	}

	if worker.config != config {
		t.Error("Worker config not set correctly")
	}

	if worker.activeSessions == nil {
		t.Error("Active sessions map not initialized")
	}
}

func TestWorkerGetActiveSessionCount(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Initially should be 0
	if count := worker.GetActiveSessionCount(); count != 0 {
		t.Errorf("Expected 0 active sessions, got %d", count)
	}

	// Add a session
	worker.sessionMutex.Lock()
	worker.activeSessions[1] = &SessionInfo{
		LocalSEID:   123,
		Session:     pfcpsim.NewPFCPSession(123, 456),
		CreatedAt:   time.Now(),
		ModifyCount: 0,
	}
	worker.sessionMutex.Unlock()

	if count := worker.GetActiveSessionCount(); count != 1 {
		t.Errorf("Expected 1 active session, got %d", count)
	}
}

func TestWorkerGetSessionInfo(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Add a session
	expectedInfo := &SessionInfo{
		LocalSEID:   123,
		Session:     pfcpsim.NewPFCPSession(123, 456),
		CreatedAt:   time.Now(),
		ModifyCount: 0,
		UEAddress:   "10.0.0.1",
		BaseID:      10,
	}

	worker.sessionMutex.Lock()
	worker.activeSessions[1] = expectedInfo
	worker.sessionMutex.Unlock()

	// Get existing session
	info, exists := worker.GetSessionInfo(1)
	if !exists {
		t.Error("Expected session to exist")
	}
	if info != expectedInfo {
		t.Error("Retrieved session info doesn't match")
	}

	// Get non-existent session
	_, exists = worker.GetSessionInfo(999)
	if exists {
		t.Error("Expected session to not exist")
	}
}

func TestWorkerNextUEAddress(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Get first address
	addr1 := worker.nextUEAddress()
	if addr1 == nil {
		t.Fatal("First UE address is nil")
	}

	// Get second address (should be different)
	addr2 := worker.nextUEAddress()
	if addr2 == nil {
		t.Fatal("Second UE address is nil")
	}

	if addr1.Equal(addr2) {
		t.Error("Consecutive UE addresses should be different")
	}

	// Verify increment
	expected := make([]byte, len(addr1))
	copy(expected, addr1)
	expected[len(expected)-1]++

	if !addr2.Equal(expected) {
		t.Errorf("UE address not incremented correctly: got %v, want %v", addr2, expected)
	}
}

func TestWorkerCalculateBaseID(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	tests := []struct {
		sessionID int
		expected  int
	}{
		{1, 10},
		{2, 20},
		{10, 100},
{100, 1000},
	}

	for _, tt := range tests {
		got := worker.calculateBaseID(tt.sessionID)
		if got != tt.expected {
			t.Errorf("calculateBaseID(%d) = %d, want %d", tt.sessionID, got, tt.expected)
		}
	}
}

func TestWorkerProcessTask(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Test processing task with unknown type (should log error but not crash)
	task := &SessionTask{
		Type:      TaskType(999),
		SessionID: 1,
	}

	// Should not panic
	worker.processTask(task)

	// Verify metrics weren't updated for invalid task
	snapshot := metrics.GetSnapshot()
	if snapshot.TotalCreated != 0 {
		t.Error("Metrics updated for invalid task type")
	}
}

func TestWorkerBuildSessionIEs(t *testing.T) {
	config := NewDefaultStressConfig()
	config.AppFilters = []string{"ip:any:any:allow:100"}
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	sessionID := 1
	pdrs, fars, qers, urrs := worker.buildSessionIEs(sessionID)

	// Verify IEs were created
	if len(pdrs) == 0 {
		t.Error("No PDRs created")
	}
	if len(fars) == 0 {
		t.Error("No FARs created")
	}
	if len(qers) == 0 {
		t.Error("No QERs created")
	}
	if len(urrs) == 0 {
		t.Error("No URRs created")
	}

	// For 1 app filter, we expect:
	// - 2 PDRs (uplink + downlink)
	// - 2 FARs (uplink + downlink)
	// - 3 QERs (1 session + 2 application)
	// - 1 URR

	expectedPDRs := 2
	expectedFARs := 2
	expectedQERs := 3
	expectedURRs := 1

	if len(pdrs) != expectedPDRs {
		t.Errorf("Expected %d PDRs, got %d", expectedPDRs, len(pdrs))
	}
	if len(fars) != expectedFARs {
		t.Errorf("Expected %d FARs, got %d", expectedFARs, len(fars))
	}
	if len(qers) != expectedQERs {
		t.Errorf("Expected %d QERs, got %d", expectedQERs, len(qers))
	}
	if len(urrs) != expectedURRs {
		t.Errorf("Expected %d URRs, got %d", expectedURRs, len(urrs))
	}
}

func TestWorkerBuildModifyIEs(t *testing.T) {
	config := NewDefaultStressConfig()
	config.AppFilters = []string{"ip:any:any:allow:100"}
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	sessionID := 1
	iteration := 1
	sessInfo := &SessionInfo{
		LocalSEID:   123,
		Session:     pfcpsim.NewPFCPSession(123, 456),
		CreatedAt:   time.Now(),
		ModifyCount: 0,
		BaseID:      10,
	}

	pdrs, fars, qers, urrs := worker.buildModifyIEs(sessionID, iteration, sessInfo)

	// Modify should update FARs and URRs
	if len(fars) == 0 {
		t.Error("No FARs created for modify")
	}
	if len(urrs) == 0 {
		t.Error("No URRs created for modify")
	}

	// For 1 app filter, we expect:
	// - 1 FAR (downlink)
	// - 1 URR

	expectedFARs := 1
	expectedURRs := 1

	if len(fars) != expectedFARs {
		t.Errorf("Expected %d FARs, got %d", expectedFARs, len(fars))
	}
	if len(urrs) != expectedURRs {
		t.Errorf("Expected %d URRs, got %d", expectedURRs, len(urrs))
	}
}

func TestWorkerStartStop(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Start worker
	wg.Add(1)
	go worker.Start(ctx, &wg)

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Send a task (will fail since not connected, but tests the flow)
	workQueue <- &SessionTask{
		Type:      TaskCreate,
		SessionID: 1,
	}

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Stop worker via context
	cancel()

	// Wait for worker to exit
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Worker stopped successfully
	case <-time.After(1 * time.Second):
		t.Error("Worker didn't stop within timeout")
	}
}

func TestWorkerStopViaClosedQueue(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Start worker
	wg.Add(1)
	go worker.Start(ctx, &wg)

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Close work queue
	close(workQueue)

	// Wait for worker to exit
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Worker stopped successfully
	case <-time.After(1 * time.Second):
		t.Error("Worker didn't stop within timeout")
	}
}

func TestWorkerConcurrentSessions(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Concurrently add sessions
	numSessions := 100
	var wg sync.WaitGroup

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker.sessionMutex.Lock()
			worker.activeSessions[id] = &SessionInfo{
				LocalSEID: uint64(id),
				Session:   pfcpsim.NewPFCPSession(uint64(id), uint64(id+1000)),
				CreatedAt: time.Now(),
			}
			worker.sessionMutex.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify all sessions were added
	if count := worker.GetActiveSessionCount(); count != numSessions {
		t.Errorf("Expected %d active sessions, got %d", numSessions, count)
	}
}

func TestWorkerParseAppFilter(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	workQueue := make(chan *SessionTask, 10)
	metrics := NewMetrics()

	worker := NewWorker(1, client, config, workQueue, metrics)

	// Test empty filter
	sdfFilter, gateStatus, precedence, err := worker.parseAppFilter("")
	if err != nil {
		t.Errorf("Unexpected error for empty filter: %v", err)
	}
	if sdfFilter != "" {
		t.Error("Expected empty SDF filter for empty input")
	}
	if gateStatus != ieLib.GateStatusOpen {
		t.Error("Expected gate status open for empty filter")
	}
	if precedence != 100 {
		t.Error("Expected default precedence 100")
	}

	// Test non-empty filter (uses default implementation)
	sdfFilter, gateStatus, precedence, err = worker.parseAppFilter("ip:any:any:allow:100")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if sdfFilter == "" {
		t.Error("Expected non-empty SDF filter")
	}
}