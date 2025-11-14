// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"sync/atomic"
	"time"
)

// incrementSent atomically increments the sent counter
func (s *AsyncStats) incrementSent() {
	atomic.AddInt64(&s.TotalSent, 1)
}

// incrementReceived atomically increments the received counter
func (s *AsyncStats) incrementReceived() {
	atomic.AddInt64(&s.TotalReceived, 1)
}

// incrementTimeout atomically increments the timeout counter
func (s *AsyncStats) incrementTimeout() {
	atomic.AddInt64(&s.TotalTimeout, 1)
}

// incrementErrors atomically increments the error counter
func (s *AsyncStats) incrementErrors() {
	atomic.AddInt64(&s.TotalErrors, 1)
}

// recordLatency updates the average latency with a new sample
// Uses exponential moving average for efficiency
func (s *AsyncStats) recordLatency(latency time.Duration) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.AverageLatency == 0 {
		// First sample
		s.AverageLatency = latency
	} else {
		// Exponential moving average (alpha = 0.1)
		// This gives more weight to recent samples
		alpha := 0.1
		s.AverageLatency = time.Duration(
			float64(s.AverageLatency)*(1-alpha) + float64(latency)*alpha,
		)
	}
}

// GetSnapshot returns a thread-safe snapshot of current statistics
func (s *AsyncStats) GetSnapshot() AsyncStatsSnapshot {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return AsyncStatsSnapshot{
		TotalSent:      atomic.LoadInt64(&s.TotalSent),
		TotalReceived:  atomic.LoadInt64(&s.TotalReceived),
		TotalTimeout:   atomic.LoadInt64(&s.TotalTimeout),
		TotalErrors:    atomic.LoadInt64(&s.TotalErrors),
		AverageLatency: s.AverageLatency,
	}
}

// Reset clears all statistics (useful for testing)
func (s *AsyncStats) Reset() {
	atomic.StoreInt64(&s.TotalSent, 0)
	atomic.StoreInt64(&s.TotalReceived, 0)
	atomic.StoreInt64(&s.TotalTimeout, 0)
	atomic.StoreInt64(&s.TotalErrors, 0)

	s.lock.Lock()
	s.AverageLatency = 0
	s.lock.Unlock()
}

// AsyncStatsSnapshot is a point-in-time snapshot of async statistics
type AsyncStatsSnapshot struct {
	TotalSent      int64
	TotalReceived  int64
	TotalTimeout   int64
	TotalErrors    int64
	AverageLatency time.Duration
}

// SuccessRate calculates the success rate as a percentage
func (s AsyncStatsSnapshot) SuccessRate() float64 {
	if s.TotalSent == 0 {
		return 0.0
	}
	successful := s.TotalReceived
	return (float64(successful) / float64(s.TotalSent)) * 100.0
}

// TimeoutRate calculates the timeout rate as a percentage
func (s AsyncStatsSnapshot) TimeoutRate() float64 {
	if s.TotalSent == 0 {
		return 0.0
	}
	return (float64(s.TotalTimeout) / float64(s.TotalSent)) * 100.0
}

// PendingCount returns the number of requests still pending
func (s AsyncStatsSnapshot) PendingCount() int64 {
	return s.TotalSent - s.TotalReceived - s.TotalTimeout - s.TotalErrors
}