// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"sync/atomic"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	"github.com/wmnsk/go-pfcp/message"
)

// TimeoutManager tracks and manages request timeouts
type TimeoutManager struct {
	// activeTimeouts tracks number of active timeout timers
	activeTimeouts int64

	// totalTimeoutsTriggered tracks how many timeouts have fired
	totalTimeoutsTriggered int64

	// timeoutDuration is the default timeout duration
	timeoutDuration time.Duration
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(defaultTimeout time.Duration) *TimeoutManager {
	return &TimeoutManager{
		timeoutDuration: defaultTimeout,
	}
}

// GetActiveTimeouts returns the number of active timeout timers
func (tm *TimeoutManager) GetActiveTimeouts() int64 {
	return atomic.LoadInt64(&tm.activeTimeouts)
}

// GetTotalTimeoutsTriggered returns total timeouts that have fired
func (tm *TimeoutManager) GetTotalTimeoutsTriggered() int64 {
	return atomic.LoadInt64(&tm.totalTimeoutsTriggered)
}

// incrementActiveTimeouts increments the active timeout counter
func (tm *TimeoutManager) incrementActiveTimeouts() {
	atomic.AddInt64(&tm.activeTimeouts, 1)
}

// decrementActiveTimeouts decrements the active timeout counter
func (tm *TimeoutManager) decrementActiveTimeouts() {
	atomic.AddInt64(&tm.activeTimeouts, -1)
}

// incrementTimeoutsTriggered increments the triggered timeout counter
func (tm *TimeoutManager) incrementTimeoutsTriggered() {
	atomic.AddInt64(&tm.totalTimeoutsTriggered, 1)
}

// handleTimeoutWithRetry handles timeout with optional retry logic
// This is an enhanced version that could support retries in the future
func (c *AsyncPFCPClient) handleTimeoutWithRetry(seqNum uint32, retryCount int) {
	c.pendingRequestsLock.Lock()
	pending, exists := c.pendingRequests[seqNum]
	if !exists {
		// Request was already handled (response arrived just before timeout)
		c.pendingRequestsLock.Unlock()
		logger.PfcpsimLog.Debugf("Timeout fired for sequence %d but request already handled", seqNum)
		return
	}

	// Check if this is a retry scenario (future enhancement)
	if retryCount > 0 {
		// For now, we don't implement retries, but the structure is here
		logger.PfcpsimLog.Debugf("Retry %d for sequence %d (not implemented)", retryCount, seqNum)
	}

	// Remove from pending requests
	delete(c.pendingRequests, seqNum)
	c.pendingRequestsLock.Unlock()

	// Update timeout statistics
	c.stats.incrementTimeout()

	// Calculate how long we waited
	latency := time.Since(pending.SentAt)

	logger.PfcpsimLog.Warnf(
		"Request timeout for sequence %d (type: %s) after %v",
		seqNum,
		pending.MessageType,
		latency,
	)

	// Create timeout error with details
	timeoutErr := &pfcpSimError{
		message: "Request timeout expired",
		error: []error{
			NewTimeoutExpiredError(),
		},
	}

	// Send timeout error to the waiting goroutine
	result := ResponseResult{
		Message: nil,
		Error:   timeoutErr,
		Latency: latency,
		SeqNum:  seqNum,
	}

	// Attempt to send, but don't block if channel is closed
	select {
	case pending.ResponseCh <- result:
		logger.PfcpsimLog.Debugf("Timeout error sent for sequence %d", seqNum)
	default:
		logger.PfcpsimLog.Warnf("Failed to send timeout error for sequence %d, channel closed", seqNum)
	}

	// Close the response channel
	close(pending.ResponseCh)
}

// CancelPendingRequest allows manual cancellation of a pending request
// Returns true if the request was found and cancelled
func (c *AsyncPFCPClient) CancelPendingRequest(seqNum uint32) bool {
	pending := c.removePendingRequest(seqNum)
	if pending == nil {
		return false
	}

	logger.PfcpsimLog.Infof("Manually cancelled pending request %d", seqNum)

	// Send cancellation error
	result := ResponseResult{
		Message: nil,
		Error:   NewAssociationInactiveError(), // Reuse this error for cancellation
		Latency: time.Since(pending.SentAt),
		SeqNum:  seqNum,
	}

	select {
	case pending.ResponseCh <- result:
	default:
		// Channel closed, that's ok
	}
	close(pending.ResponseCh)

	return true
}

// GetOldestPendingRequest returns info about the oldest pending request
// Useful for monitoring stuck requests
func (c *AsyncPFCPClient) GetOldestPendingRequest() *PendingRequestInfo {
	c.pendingRequestsLock.RLock()
	defer c.pendingRequestsLock.RUnlock()

	var oldest *PendingRequest
	var oldestTime time.Time

	for _, pending := range c.pendingRequests {
		if oldest == nil || pending.SentAt.Before(oldestTime) {
			oldest = pending
			oldestTime = pending.SentAt
		}
	}

	if oldest == nil {
		return nil
	}

	return &PendingRequestInfo{
		SeqNum:      oldest.SeqNum,
		MessageType: oldest.MessageType,
		Age:         time.Since(oldest.SentAt),
		SentAt:      oldest.SentAt,
	}
}

// PendingRequestInfo provides read-only info about a pending request
type PendingRequestInfo struct {
	SeqNum      uint32
	MessageType message.MessageType
	Age         time.Duration
	SentAt      time.Time
}

// GetPendingRequestsInfo returns info about all pending requests
// Useful for monitoring and debugging
func (c *AsyncPFCPClient) GetPendingRequestsInfo() []PendingRequestInfo {
	c.pendingRequestsLock.RLock()
	defer c.pendingRequestsLock.RUnlock()

	infos := make([]PendingRequestInfo, 0, len(c.pendingRequests))

	for _, pending := range c.pendingRequests {
		infos = append(infos, PendingRequestInfo{
			SeqNum:      pending.SeqNum,
			MessageType: pending.MessageType,
			Age:         time.Since(pending.SentAt),
			SentAt:      pending.SentAt,
		})
	}

	return infos
}

// CleanupStalePendingRequests removes requests older than maxAge
// Returns the number of requests cleaned up
func (c *AsyncPFCPClient) CleanupStalePendingRequests(maxAge time.Duration) int {
	c.pendingRequestsLock.Lock()
	
	staleRequests := make([]*PendingRequest, 0)
	now := time.Now()

	for seqNum, pending := range c.pendingRequests {
		if now.Sub(pending.SentAt) > maxAge {
			staleRequests = append(staleRequests, pending)
			delete(c.pendingRequests, seqNum)
		}
	}
	
	c.pendingRequestsLock.Unlock()

	// Process stale requests outside the lock
	for _, pending := range staleRequests {
		if pending.Timeout != nil {
			pending.Timeout.Stop()
		}

		logger.PfcpsimLog.Warnf(
			"Cleaned up stale request %d (age: %v)",
			pending.SeqNum,
			now.Sub(pending.SentAt),
		)

		result := ResponseResult{
			Error:   NewTimeoutExpiredError(),
			Latency: now.Sub(pending.SentAt),
			SeqNum:  pending.SeqNum,
		}

		select {
		case pending.ResponseCh <- result:
		default:
		}
		close(pending.ResponseCh)
	}

	return len(staleRequests)
}