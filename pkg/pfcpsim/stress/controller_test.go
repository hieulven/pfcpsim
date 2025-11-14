// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"context"
	"testing"
	"time"

	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
)

func TestNewStressController(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")

	controller := NewStressController(config, client)

	if controller == nil {
		t.Fatal("NewStressController returned nil")
	}

	if controller.config != config {
		t.Error("Config not set correctly")
	}

	if controller.client != client {
		t.Error("Client not set correctly")
	}

	if controller.workQueue == nil {
		t.Error("Work queue not initialized")
	}

	if controller.metrics == nil {
		t.Error("Metrics not initialized")
	}

	if controller.rateLimiter == nil {
		t.Error("Rate limiter not initialized")
	}

	if controller.sessionsActive == nil {
		t.Error("Sessions active map not initialized")
	}
}

func TestStressControllerValidation(t *testing.T) {
	// Test with invalid config
	config := &StressConfig{
		TPS: -1, // Invalid
	}
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")

	// Should panic due to validation failure
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for invalid config")
		}
	}()

	NewStressController(config, client)
}

func TestStressControllerStartStop(t *testing.T) {
	config := NewDefaultStressConfig()
	config.TPS = 10
	config.Duration = 2 * time.Second
	config.TotalSessions = 5
	config.NumWorkers = 2
	config.ModificationsPerSession = 0 // Skip modifications for quick test

	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	// Mark association as active for testing
	client.SetAssociationStatus(true)

	controller := NewStressController(config, client)

	// Test initial state
	if controller.IsRunning() {
		t.Error("Controller should not be running initially")
	}

	// Start workers (without full run)
	controller.startWorkers()

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Stop
	controller.Stop()

	// Wait for workers
	controller.wg.Wait()

	if controller.IsRunning() {
		t.Error("Controller should not be running after stop")
	}
}

func TestStressControllerGetMetrics(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")

	controller := NewStressController(config, client)

	// Get initial metrics
	snapshot := controller.GetMetrics()

	if snapshot.TotalCreated != 0 {
		t.Error("Expected 0 created sessions initially")
	}

	// Record some operations
	controller.metrics.RecordCreate(10*time.Millisecond, true)
	controller.metrics.RecordModify(5*time.Millisecond, true)

	snapshot = controller.GetMetrics()

	if snapshot.TotalCreated != 1 {
		t.Errorf("Expected 1 created session, got %d", snapshot.TotalCreated)
	}

	if snapshot.TotalModified != 1 {
		t.Errorf("Expected 1 modified session, got %d", snapshot.TotalModified)
	}
}

func TestStressControllerGetProgress(t *testing.T) {
	config := NewDefaultStressConfig()
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")

	controller := NewStressController(config, client)

	// Initial progress
	created, modified, deleted := controller.GetProgress()
	if created != 0 || modified != 0 || deleted != 0 {
		t.Error("Expected zero progress initially")
	}

	// Update progress
	controller.progressMutex.Lock()
	controller.sessionsCreated = 10
	controller.sessionsModified = 5
	controller.sessionsDeleted = 2
	controller.progressMutex.Unlock()

	created, modified, deleted = controller.GetProgress()
	if created != 10 || modified != 5 || deleted != 2 {
		t.Errorf("Progress mismatch: got (%d,%d,%d), want (10,5,2)",
			created, modified, deleted)
	}
}

func TestStressControllerRampUp(t *testing.T) {
	config := NewDefaultStressConfig()
	config.TPS = 100
	config.RampUpTime = 500 * time.Millisecond

	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	controller := NewStressController(config, client)

	// Check initial rate (should be 1 for ramp-up)
	initialLimit := controller.rateLimiter.Limit()
	if initialLimit != 1 {
		t.Errorf("Expected initial rate 1, got %v", initialLimit)
	}

	// Run ramp-up
	done := make(chan struct{})
	go controller.rampUpTPS(done)

	// Wait for ramp-up to complete
	<-done

	// Check final rate
	finalLimit := controller.rateLimiter.Limit()
	if finalLimit != 100 {
		t.Errorf("Expected final rate 100, got %v", finalLimit)
	}
}

func TestStressControllerGetActiveSessionCount(t *testing.T) {
	config := NewDefaultStressConfig()
	config.NumWorkers = 3
	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")

	controller := NewStressController(config, client)
	controller.startWorkers()

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

	// Initially should be 0
	count := controller.GetActiveSessionCount()
	if count != 0 {
		t.Errorf("Expected 0 active sessions, got %d", count)
	}

	// Add sessions to workers
	for i, worker := range controller.workers {
		worker.sessionMutex.Lock()
		worker.activeSessions[i] = &SessionInfo{
			LocalSEID: uint64(i),
			Session:   pfcpsim.NewPFCPSession(uint64(i), uint64(i+1000)),
			CreatedAt: time.Now(),
		}
		worker.sessionMutex.Unlock()
	}

	// Should now be 3 (one per worker)
	count = controller.GetActiveSessionCount()
	if count != 3 {
		t.Errorf("Expected 3 active sessions, got %d", count)
	}

	// Cleanup
	controller.Stop()
	controller.wg.Wait()
}

func TestStressControllerCreateSessionsQueue(t *testing.T) {
	config := NewDefaultStressConfig()
	config.TPS = 1000 // High TPS for quick test
	config.TotalSessions = 10
	config.BaseID = 100

	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	controller := NewStressController(config, client)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	controller.ctx = ctx

	// Start create phase
	done := make(chan struct{})
	go controller.createSessions(done)

	// Collect tasks from queue
	tasksReceived := 0
	timeout := time.After(3 * time.Second)

	for tasksReceived < config.TotalSessions {
		select {
		case task := <-controller.workQueue:
			if task.Type != TaskCreate {
				t.Errorf("Expected TaskCreate, got %v", task.Type)
			}
			if task.SessionID < config.BaseID || task.SessionID >= config.BaseID+config.TotalSessions {
				t.Errorf("Invalid session ID: %d", task.SessionID)
			}
			tasksReceived++
		case <-timeout:
			t.Fatalf("Timeout waiting for tasks, received %d/%d", tasksReceived, config.TotalSessions)
		case <-done:
			goto checkCount
		}
	}

checkCount:
	created, _, _ := controller.GetProgress()
	if created != config.TotalSessions {
		t.Errorf("Expected %d sessions created, got %d", config.TotalSessions, created)
	}
}

func TestStressControllerModifySessionIteration(t *testing.T) {
	config := NewDefaultStressConfig()
	config.TPS = 1000
	config.BaseID = 1

	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	controller := NewStressController(config, client)

	// Mark some sessions as active
	controller.stateMutex.Lock()
	controller.sessionsActive[1] = true
	controller.sessionsActive[2] = true
	controller.sessionsActive[3] = true
	controller.stateMutex.Unlock()

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	controller.ctx = ctx

	// Run modification iteration
	modifiedCount := controller.modifySessionIteration(0)

	if modifiedCount != 3 {
		t.Errorf("Expected 3 modifications, got %d", modifiedCount)
	}

	// Check that modify tasks were queued
	_, modified, _ := controller.GetProgress()
	if modified != 3 {
		t.Errorf("Expected 3 in progress count, got %d", modified)
	}
}

func TestStressControllerDeleteSessions(t *testing.T) {
	config := NewDefaultStressConfig()
	config.TPS = 1000
	config.BaseID = 1

	client := pfcpsim.NewAsyncPFCPClient("127.0.0.1")
	controller := NewStressController(config, client)

	// Mark some sessions as active
	controller.stateMutex.Lock()
	controller.sessionsActive[1] = true
	controller.sessionsActive[2] = true
	controller.sessionsActive[3] = true
	controller.stateMutex.Unlock()

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	controller.ctx = ctx

	// Delete sessions
	controller.deleteSessions()

	// Check that sessions were marked inactive
	controller.stateMutex.RLock()
	for id := 1; id <= 3; id++ {
		if controller.sessionsActive[id] {
			t.Errorf("Session %d should be marked inactive", id)
		}
	}
	controller.stateMutex.RUnlock()

	// Check progress
	_, _, deleted := controller.GetProgress()
	if deleted != 3 {
		t.Errorf("Expected 3 deletions, got %d", deleted)
	}
}