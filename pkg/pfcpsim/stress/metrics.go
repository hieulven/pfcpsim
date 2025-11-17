// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"strings"
)

// Metrics collects performance statistics for the stress test
type Metrics struct {
	// Operation counts (atomic)
	TotalCreated  int64
	TotalModified int64
	TotalDeleted  int64
	TotalFailed   int64

	// Success counts
	CreateSuccess  int64
	ModifySuccess  int64
	DeleteSuccess  int64

	// Latency tracking
	CreateLatency *LatencyTracker
	ModifyLatency *LatencyTracker
	DeleteLatency *LatencyTracker

	// Rate tracking
	startTime time.Time
	lastCheck time.Time
	lastCount int64

	mutex sync.RWMutex
}

// LatencyTracker tracks latency statistics
type LatencyTracker struct {
	samples []time.Duration
	mutex   sync.RWMutex

	// Cached percentiles (recalculated when samples change)
	min, max, avg time.Duration
	p50, p95, p99 time.Duration
	dirty         bool
}

// NewMetrics creates a new metrics collector
func NewMetrics() *Metrics {
	return &Metrics{
		CreateLatency: NewLatencyTracker(),
		ModifyLatency: NewLatencyTracker(),
		DeleteLatency: NewLatencyTracker(),
		startTime:     time.Now(),
		lastCheck:     time.Now(),
	}
}

// NewLatencyTracker creates a new latency tracker
func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		samples: make([]time.Duration, 0, 10000),
		dirty:   true,
	}
}

// RecordCreate records a create operation
func (m *Metrics) RecordCreate(latency time.Duration, success bool) {
	atomic.AddInt64(&m.TotalCreated, 1)
	if success {
		atomic.AddInt64(&m.CreateSuccess, 1)
		m.CreateLatency.AddSample(latency)
	} else {
		atomic.AddInt64(&m.TotalFailed, 1)
	}
}

// RecordModify records a modify operation
func (m *Metrics) RecordModify(latency time.Duration, success bool) {
	atomic.AddInt64(&m.TotalModified, 1)
	if success {
		atomic.AddInt64(&m.ModifySuccess, 1)
		m.ModifyLatency.AddSample(latency)
	} else {
		atomic.AddInt64(&m.TotalFailed, 1)
	}
}

// RecordDelete records a delete operation
func (m *Metrics) RecordDelete(latency time.Duration, success bool) {
	atomic.AddInt64(&m.TotalDeleted, 1)
	if success {
		atomic.AddInt64(&m.DeleteSuccess, 1)
		m.DeleteLatency.AddSample(latency)
	} else {
		atomic.AddInt64(&m.TotalFailed, 1)
	}
}

// AddSample adds a latency sample
func (lt *LatencyTracker) AddSample(latency time.Duration) {
	lt.mutex.Lock()
	defer lt.mutex.Unlock()

	lt.samples = append(lt.samples, latency)
	lt.dirty = true

	// Limit sample size to prevent memory bloat
	if len(lt.samples) > 10000 {
		// Keep only recent samples
		lt.samples = lt.samples[len(lt.samples)-5000:]
	}
}

// calculatePercentiles calculates percentile values
// Must be called with lock held
func (lt *LatencyTracker) calculatePercentiles() {
	if !lt.dirty || len(lt.samples) == 0 {
		return
	}

	// Make a copy and sort
	sorted := make([]time.Duration, len(lt.samples))
	copy(sorted, lt.samples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate min, max, avg
	lt.min = sorted[0]
	lt.max = sorted[len(sorted)-1]

	var sum time.Duration
	for _, s := range sorted {
		sum += s
	}
	lt.avg = sum / time.Duration(len(sorted))

	// Calculate percentiles
	lt.p50 = sorted[len(sorted)*50/100]
	lt.p95 = sorted[len(sorted)*95/100]
	lt.p99 = sorted[len(sorted)*99/100]

	lt.dirty = false
}

// GetStats returns latency statistics
func (lt *LatencyTracker) GetStats() LatencyStats {
	lt.mutex.Lock()
	defer lt.mutex.Unlock()

	lt.calculatePercentiles()

	return LatencyStats{
		Min:    lt.min,
		Max:    lt.max,
		Avg:    lt.avg,
		P50:    lt.p50,
		P95:    lt.p95,
		P99:    lt.p99,
		Count:  len(lt.samples),
	}
}

// LatencyStats holds latency statistics
type LatencyStats struct {
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
	Count int
}

// MetricsSnapshot is a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	TotalCreated   int64
	TotalModified  int64
	TotalDeleted   int64
	TotalFailed    int64
	CreateSuccess  int64
	ModifySuccess  int64
	DeleteSuccess  int64
	CreateLatency  LatencyStats
	ModifyLatency  LatencyStats
	DeleteLatency  LatencyStats
	CurrentTPS     float64
	AverageTPS     float64
	ElapsedTime    time.Duration
	SuccessRate    float64
}

// GetSnapshot returns a snapshot of current metrics
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	elapsed := time.Since(m.startTime)
	totalOps := atomic.LoadInt64(&m.TotalCreated) +
		atomic.LoadInt64(&m.TotalModified) +
		atomic.LoadInt64(&m.TotalDeleted)

	// Calculate average TPS
	avgTPS := 0.0
	if elapsed.Seconds() > 0 {
		avgTPS = float64(totalOps) / elapsed.Seconds()
	}

	// Calculate current TPS (since last check)
	currentTPS := 0.0
	timeSinceLastCheck := time.Since(m.lastCheck)
	if timeSinceLastCheck.Seconds() > 0 {
		opsSinceLastCheck := totalOps - m.lastCount
		currentTPS = float64(opsSinceLastCheck) / timeSinceLastCheck.Seconds()
	}
	m.lastCheck = time.Now()
	m.lastCount = totalOps

	// Calculate success rate
	successCount := atomic.LoadInt64(&m.CreateSuccess) +
		atomic.LoadInt64(&m.ModifySuccess) +
		atomic.LoadInt64(&m.DeleteSuccess)
	successRate := 0.0
	if totalOps > 0 {
		successRate = float64(successCount) / float64(totalOps) * 100
	}

	return MetricsSnapshot{
		TotalCreated:   atomic.LoadInt64(&m.TotalCreated),
		TotalModified:  atomic.LoadInt64(&m.TotalModified),
		TotalDeleted:   atomic.LoadInt64(&m.TotalDeleted),
		TotalFailed:    atomic.LoadInt64(&m.TotalFailed),
		CreateSuccess:  atomic.LoadInt64(&m.CreateSuccess),
		ModifySuccess:  atomic.LoadInt64(&m.ModifySuccess),
		DeleteSuccess:  atomic.LoadInt64(&m.DeleteSuccess),
		CreateLatency:  m.CreateLatency.GetStats(),
		ModifyLatency:  m.ModifyLatency.GetStats(),
		DeleteLatency:  m.DeleteLatency.GetStats(),
		CurrentTPS:     currentTPS,
		AverageTPS:     avgTPS,
		ElapsedTime:    elapsed,
		SuccessRate:    successRate,
	}
}

// PrintReport prints a formatted report of the metrics
func (m *Metrics) PrintReport() {
	snapshot := m.GetSnapshot()

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("STRESS TEST RESULTS")
	fmt.Println(strings.Repeat("=", 80))

	// Summary
	fmt.Printf("\nDuration: %v\n", snapshot.ElapsedTime.Round(time.Second))
	fmt.Printf("Total Operations: %d\n", 
		snapshot.TotalCreated+snapshot.TotalModified+snapshot.TotalDeleted)
	fmt.Printf("Success Rate: %.2f%%\n", snapshot.SuccessRate)
	fmt.Printf("Failed Operations: %d\n", snapshot.TotalFailed)
	fmt.Printf("Average TPS: %.2f\n", snapshot.AverageTPS)
	fmt.Printf("Current TPS: %.2f\n", snapshot.CurrentTPS)

	// Create operations
	if snapshot.TotalCreated > 0 {
		fmt.Println("\n--- CREATE Operations ---")
		fmt.Printf("Total: %d (Success: %d, Failed: %d)\n",
			snapshot.TotalCreated,
			snapshot.CreateSuccess,
			snapshot.TotalCreated-snapshot.CreateSuccess)
		printLatencyStats("Create", snapshot.CreateLatency)
	}

	// Modify operations
	if snapshot.TotalModified > 0 {
		fmt.Println("\n--- MODIFY Operations ---")
		fmt.Printf("Total: %d (Success: %d, Failed: %d)\n",
			snapshot.TotalModified,
			snapshot.ModifySuccess,
			snapshot.TotalModified-snapshot.ModifySuccess)
		printLatencyStats("Modify", snapshot.ModifyLatency)
	}

	// Delete operations
	if snapshot.TotalDeleted > 0 {
		fmt.Println("\n--- DELETE Operations ---")
		fmt.Printf("Total: %d (Success: %d, Failed: %d)\n",
			snapshot.TotalDeleted,
			snapshot.DeleteSuccess,
			snapshot.TotalDeleted-snapshot.DeleteSuccess)
		printLatencyStats("Delete", snapshot.DeleteLatency)
	}

	fmt.Println(strings.Repeat("=", 80) + "\n")
}

func printLatencyStats(name string, stats LatencyStats) {
	if stats.Count == 0 {
		fmt.Printf("%s Latency: No samples\n", name)
		return
	}

	fmt.Printf("%s Latency:\n", name)
	fmt.Printf("  Min: %v\n", stats.Min.Round(time.Microsecond))
	fmt.Printf("  Avg: %v\n", stats.Avg.Round(time.Microsecond))
	fmt.Printf("  P50: %v\n", stats.P50.Round(time.Microsecond))
	fmt.Printf("  P95: %v\n", stats.P95.Round(time.Microsecond))
	fmt.Printf("  P99: %v\n", stats.P99.Round(time.Microsecond))
	fmt.Printf("  Max: %v\n", stats.Max.Round(time.Microsecond))
	fmt.Printf("  Samples: %d\n", stats.Count)
}