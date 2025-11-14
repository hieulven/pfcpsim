// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
	"golang.org/x/time/rate"
)

// StressController orchestrates the stress test execution.
// It manages worker pools, rate limiting, and task distribution.
type StressController struct {
	// config holds the stress test configuration
	config *StressConfig

	// client is the async PFCP client (shared, or one per worker)
	client *pfcpsim.AsyncPFCPClient

	// rateLimiter controls the rate of operations
	rateLimiter *rate.Limiter

	// workQueue is the channel for distributing tasks to workers
	workQueue chan *SessionTask

	// workers is the pool of worker goroutines
	workers []*Worker

	// metrics collects performance data
	metrics *Metrics

	// ctx and cancel for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks worker goroutines
	wg sync.WaitGroup

	// state tracking
	stateMutex     sync.RWMutex
	isRunning      bool
	sessionsActive map[int]bool // tracks which sessions are currently active

	// Progress tracking
	sessionsCreated  int
	sessionsModified int
	sessionsDeleted  int
	progressMutex    sync.Mutex
}

// NewStressController creates a new stress test controller
func NewStressController(config *StressConfig, client *pfcpsim.AsyncPFCPClient) *StressController {
	// Validate config
	if err := config.Validate(); err != nil {
		logger.PfcpsimLog.Fatalf("Invalid configuration: %v", err)
	}

	// Create rate limiter
	// Start with a low rate if using ramp-up
	initialRate := rate.Limit(config.TPS)
	if config.RampUpTime > 0 {
		initialRate = rate.Limit(1) // Start at 1 TPS
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &StressController{
		config:         config,
		client:         client,
		rateLimiter:    rate.NewLimiter(initialRate, config.TPS), // burst = TPS
		workQueue:      make(chan *SessionTask, config.NumWorkers*10),
		workers:        make([]*Worker, 0, config.NumWorkers),
		metrics:        NewMetrics(),
		ctx:            ctx,
		cancel:         cancel,
		sessionsActive: make(map[int]bool),
	}
}

// Run starts the stress test execution.
// This is the main entry point that orchestrates the entire test.
func (sc *StressController) Run() error {
	sc.stateMutex.Lock()
	if sc.isRunning {
		sc.stateMutex.Unlock()
		return fmt.Errorf("stress test already running")
	}
	sc.isRunning = true
	sc.stateMutex.Unlock()

	logger.PfcpsimLog.Infoln("="*80)
	logger.PfcpsimLog.Infoln("STARTING STRESS TEST")
	logger.PfcpsimLog.Infoln("="*80)
	logger.PfcpsimLog.Infof("Configuration:")
	logger.PfcpsimLog.Infof("  Target TPS: %d", sc.config.TPS)
	logger.PfcpsimLog.Infof("  Duration: %v", sc.config.Duration)
	logger.PfcpsimLog.Infof("  Total Sessions: %d", sc.config.TotalSessions)
	logger.PfcpsimLog.Infof("  Modifications per Session: %d", sc.config.ModificationsPerSession)
	logger.PfcpsimLog.Infof("  Workers: %d", sc.config.NumWorkers)
	logger.PfcpsimLog.Infof("  Ramp-up Time: %v", sc.config.RampUpTime)
	logger.PfcpsimLog.Infoln("="*80)

	// Start workers
	sc.startWorkers()

	// Start progress reporting
	progressDone := make(chan struct{})
	go sc.reportProgress(progressDone)

	// Start ramp-up if configured
	rampUpDone := make(chan struct{})
	if sc.config.RampUpTime > 0 {
		go sc.rampUpTPS(rampUpDone)
	} else {
		close(rampUpDone)
	}

	// Start test execution
	testDone := make(chan struct{})
	go sc.executeTest(testDone)

	// Wait for test completion or timeout
	select {
	case <-testDone:
		logger.PfcpsimLog.Infoln("Test completed successfully")
	case <-time.After(sc.config.Duration + 30*time.Second): // Extra time for cleanup
		logger.PfcpsimLog.Warnln("Test duration exceeded, forcing shutdown")
	case <-sc.ctx.Done():
		logger.PfcpsimLog.Infoln("Test cancelled by user")
	}

	// Stop everything
	sc.Stop()
	close(progressDone)

	// Wait for workers to finish
	sc.wg.Wait()

	// Print final report
	sc.metrics.PrintReport()

	return nil
}

// startWorkers initializes and starts all worker goroutines
func (sc *StressController) startWorkers() {
	logger.PfcpsimLog.Infof("Starting %d workers...", sc.config.NumWorkers)

	for i := 0; i < sc.config.NumWorkers; i++ {
		worker := NewWorker(i+1, sc.client, sc.config, sc.workQueue, sc.metrics)
		sc.workers = append(sc.workers, worker)

		sc.wg.Add(1)
		go worker.Start(sc.ctx, &sc.wg)
	}

	logger.PfcpsimLog.Infof("All %d workers started", sc.config.NumWorkers)
}

// Stop gracefully stops the stress test
func (sc *StressController) Stop() {
	sc.stateMutex.Lock()
	defer sc.stateMutex.Unlock()

	if !sc.isRunning {
		return
	}

	logger.PfcpsimLog.Infoln("Stopping stress test...")

	// Cancel context to signal workers
	if sc.cancel != nil {
		sc.cancel()
	}

	// Close work queue
	close(sc.workQueue)

	sc.isRunning = false

	logger.PfcpsimLog.Infoln("Stress test stopped")
}

// GetMetrics returns the current metrics snapshot
func (sc *StressController) GetMetrics() MetricsSnapshot {
	return sc.metrics.GetSnapshot()
}

// rampUpTPS gradually increases the TPS to the target rate
func (sc *StressController) rampUpTPS(done chan struct{}) {
	defer close(done)

	logger.PfcpsimLog.Infof("Starting TPS ramp-up over %v", sc.config.RampUpTime)

	startTPS := 1.0
	targetTPS := float64(sc.config.TPS)
	steps := 20 // Number of steps in ramp-up
	stepDuration := sc.config.RampUpTime / time.Duration(steps)
	tpsIncrement := (targetTPS - startTPS) / float64(steps)

	currentTPS := startTPS

	for i := 0; i < steps; i++ {
		select {
		case <-sc.ctx.Done():
			return
		case <-time.After(stepDuration):
			currentTPS += tpsIncrement
			sc.rateLimiter.SetLimit(rate.Limit(currentTPS))
			logger.PfcpsimLog.Debugf("Ramp-up: TPS = %.2f", currentTPS)
		}
	}

	// Set final target TPS
	sc.rateLimiter.SetLimit(rate.Limit(targetTPS))
	logger.PfcpsimLog.Infof("Ramp-up complete: TPS = %.2f", targetTPS)
}

// reportProgress periodically reports test progress
func (sc *StressController) reportProgress(done chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-sc.ctx.Done():
			return
		case <-ticker.C:
			snapshot := sc.metrics.GetSnapshot()
			
			sc.progressMutex.Lock()
			created := sc.sessionsCreated
			modified := sc.sessionsModified
			deleted := sc.sessionsDeleted
			sc.progressMutex.Unlock()

			activeCount := 0
			for _, worker := range sc.workers {
				activeCount += worker.GetActiveSessionCount()
			}

			logger.PfcpsimLog.Infof(
				"Progress: Created=%d/%d, Modified=%d, Deleted=%d, Active=%d, TPS=%.2f, Success=%.1f%%",
				created, sc.config.TotalSessions,
				modified, deleted, activeCount,
				snapshot.CurrentTPS, snapshot.SuccessRate,
			)
		}
	}
}

// executeTest orchestrates the session lifecycle (create -> modify -> delete)
func (sc *StressController) executeTest(done chan struct{}) {
	defer close(done)

	startTime := time.Now()
	endTime := startTime.Add(sc.config.Duration)

	logger.PfcpsimLog.Infoln("Test execution started")

	// Phase 1: Create sessions
	createDone := make(chan struct{})
	go sc.createSessions(createDone)

	// Phase 2: Modify sessions (concurrent with creates)
	modifyDone := make(chan struct{})
	go sc.modifySessions(modifyDone)

	// Wait for duration or completion
	select {
	case <-time.After(sc.config.Duration):
		logger.PfcpsimLog.Infoln("Test duration reached")
	case <-sc.ctx.Done():
		logger.PfcpsimLog.Infoln("Test cancelled")
		return
	}

	// Wait for create and modify phases to complete
	<-createDone
	<-modifyDone

	// Phase 3: Delete all sessions
	logger.PfcpsimLog.Infoln("Starting session deletion phase")
	sc.deleteSessions()

	elapsed := time.Since(startTime)
	logger.PfcpsimLog.Infof("Test execution completed in %v", elapsed.Round(time.Second))
}

// createSessions creates sessions with rate limiting
func (sc *StressController) createSessions(done chan struct{}) {
	defer close(done)

	logger.PfcpsimLog.Infof("Creating %d sessions...", sc.config.TotalSessions)

	for i := 0; i < sc.config.TotalSessions; i++ {
		select {
		case <-sc.ctx.Done():
			logger.PfcpsimLog.Infoln("Session creation cancelled")
			return
		default:
			// Wait for rate limiter
			if err := sc.rateLimiter.Wait(sc.ctx); err != nil {
				logger.PfcpsimLog.Errorf("Rate limiter error: %v", err)
				return
			}

			sessionID := sc.config.BaseID + i

			// Mark session as active
			sc.stateMutex.Lock()
			sc.sessionsActive[sessionID] = true
			sc.stateMutex.Unlock()

			// Send create task
			task := &SessionTask{
				Type:      TaskCreate,
				SessionID: sessionID,
			}

			select {
			case sc.workQueue <- task:
				sc.progressMutex.Lock()
				sc.sessionsCreated++
				sc.progressMutex.Unlock()

				logger.PfcpsimLog.Debugf("Queued create task for session %d", sessionID)
			case <-sc.ctx.Done():
				return
			}
		}
	}

	logger.PfcpsimLog.Infof("All %d session create tasks queued", sc.config.TotalSessions)
}

// modifySessions modifies sessions based on configuration
func (sc *StressController) modifySessions(done chan struct{}) {
	defer close(done)

	if sc.config.ModificationsPerSession <= 0 {
		logger.PfcpsimLog.Infoln("No modifications configured, skipping modify phase")
		return
	}

	logger.PfcpsimLog.Infof(
		"Will perform %d modifications per session",
		sc.config.ModificationsPerSession,
	)

	// Wait a bit for some sessions to be created
	time.Sleep(sc.config.ModificationInterval)

	for iteration := 0; iteration < sc.config.ModificationsPerSession; iteration++ {
		select {
		case <-sc.ctx.Done():
			logger.PfcpsimLog.Infoln("Session modification cancelled")
			return
		default:
			logger.PfcpsimLog.Infof(
				"Starting modification iteration %d/%d",
				iteration+1, sc.config.ModificationsPerSession,
			)

			// Modify all sessions
			modifiedCount := sc.modifySessionIteration(iteration)

			logger.PfcpsimLog.Infof(
				"Modification iteration %d complete: %d sessions modified",
				iteration+1, modifiedCount,
			)

			// Wait before next iteration
			if iteration < sc.config.ModificationsPerSession-1 {
				select {
				case <-time.After(sc.config.ModificationInterval):
				case <-sc.ctx.Done():
					return
				}
			}
		}
	}

	logger.PfcpsimLog.Infoln("All modification iterations complete")
}

// modifySessionIteration performs one iteration of modifications across all sessions
func (sc *StressController) modifySessionIteration(iteration int) int {
	modifiedCount := 0

	// Get list of active sessions
	sc.stateMutex.RLock()
	activeSessions := make([]int, 0, len(sc.sessionsActive))
	for sessionID, active := range sc.sessionsActive {
		if active {
			activeSessions = append(activeSessions, sessionID)
		}
	}
	sc.stateMutex.RUnlock()

	logger.PfcpsimLog.Debugf(
		"Modifying %d active sessions (iteration %d)",
		len(activeSessions), iteration,
	)

	// Modify each active session
	for _, sessionID := range activeSessions {
		select {
		case <-sc.ctx.Done():
			return modifiedCount
		default:
			// Wait for rate limiter
			if err := sc.rateLimiter.Wait(sc.ctx); err != nil {
				return modifiedCount
			}

			// Send modify task
			task := &SessionTask{
				Type:      TaskModify,
				SessionID: sessionID,
				Iteration: iteration,
			}

			select {
			case sc.workQueue <- task:
				modifiedCount++
				sc.progressMutex.Lock()
				sc.sessionsModified++
				sc.progressMutex.Unlock()

				logger.PfcpsimLog.Debugf(
					"Queued modify task for session %d (iteration %d)",
					sessionID, iteration,
				)
			case <-sc.ctx.Done():
				return modifiedCount
			}
		}
	}

	return modifiedCount
}

// deleteSessions deletes all active sessions
func (sc *StressController) deleteSessions() {
	// Get list of active sessions
	sc.stateMutex.RLock()
	activeSessions := make([]int, 0, len(sc.sessionsActive))
	for sessionID, active := range sc.sessionsActive {
		if active {
			activeSessions = append(activeSessions, sessionID)
		}
	}
	sc.stateMutex.RUnlock()

	logger.PfcpsimLog.Infof("Deleting %d sessions...", len(activeSessions))

	deletedCount := 0

	for _, sessionID := range activeSessions {
		select {
		case <-sc.ctx.Done():
			logger.PfcpsimLog.Warnf("Session deletion cancelled after %d sessions", deletedCount)
			return
		default:
			// Wait for rate limiter (use higher rate for deletion)
			if err := sc.rateLimiter.Wait(sc.ctx); err != nil {
				logger.PfcpsimLog.Errorf("Rate limiter error during deletion: %v", err)
				return
			}

			// Send delete task
			task := &SessionTask{
				Type:      TaskDelete,
				SessionID: sessionID,
			}

			select {
			case sc.workQueue <- task:
				deletedCount++
				sc.progressMutex.Lock()
				sc.sessionsDeleted++
				sc.progressMutex.Unlock()

				// Mark as inactive
				sc.stateMutex.Lock()
				sc.sessionsActive[sessionID] = false
				sc.stateMutex.Unlock()

				logger.PfcpsimLog.Debugf("Queued delete task for session %d", sessionID)
			case <-sc.ctx.Done():
				return
			}
		}
	}

	// Wait a bit for deletions to complete
	time.Sleep(2 * time.Second)

	logger.PfcpsimLog.Infof("All %d session delete tasks queued", deletedCount)
}

// GetActiveSessionCount returns the number of currently active sessions
func (sc *StressController) GetActiveSessionCount() int {
	count := 0
	for _, worker := range sc.workers {
		count += worker.GetActiveSessionCount()
	}
	return count
}

// GetProgress returns the current progress
func (sc *StressController) GetProgress() (created, modified, deleted int) {
	sc.progressMutex.Lock()
	defer sc.progressMutex.Unlock()
	return sc.sessionsCreated, sc.sessionsModified, sc.sessionsDeleted
}

// IsRunning returns whether the test is currently running
func (sc *StressController) IsRunning() bool {
	sc.stateMutex.RLock()
	defer sc.stateMutex.RUnlock()
	return sc.isRunning
}