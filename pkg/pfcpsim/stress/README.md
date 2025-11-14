# PFCP Stress Test Framework

## Overview

The stress test framework provides high-throughput testing of PFCP agents (UPF) by simulating realistic session lifecycles with configurable load patterns.

## Architecture
```
┌─────────────────────────────────────────────────────────────┐
│                    Stress Controller                         │
│  - Rate Limiting (TPS control)                              │
│  - Session Lifecycle Orchestration                          │
│  - Progress Monitoring                                      │
└──────────────────┬──────────────────────────────────────────┘
                   │
                   │ Work Queue (buffered channel)
                   │
       ┌───────────┴───────────────────────────┐
       │           │           │                │
       ▼           ▼           ▼                ▼
   ┌────────┐ ┌────────┐ ┌────────┐      ┌────────┐
   │Worker 1│ │Worker 2│ │Worker 3│ ...  │Worker N│
   └───┬────┘ └───┬────┘ └───┬────┘      └───┬────┘
       │          │          │                │
       └──────────┴──────────┴────────────────┘
                   │
                   ▼
            ┌─────────────┐
            │  Async PFCP │
            │   Client    │
            └──────┬──────┘
                   │
                   ▼
              ┌─────────┐
              │   UPF   │
              └─────────┘
```

## Key Components

### 1. StressController
Orchestrates the test execution:
- Manages worker pool
- Enforces rate limits (TPS)
- Schedules session lifecycle (create → modify → delete)
- Collects and reports metrics

### 2. Worker Pool
Processes session operations concurrently:
- Each worker maintains its own session state
- Workers pull tasks from shared queue
- Independent error handling

### 3. Rate Limiter
Controls operation rate:
- Token bucket algorithm
- Supports ramp-up (gradual TPS increase)
- Per-operation rate limiting

### 4. Metrics
Tracks performance:
- Operation counts (create/modify/delete)
- Success/failure rates
- Latency percentiles (P50, P95, P99)
- Current and average TPS

## Configuration
```go
config := &StressConfig{
    // Rate control
    TPS:         100,              // Target transactions per second
    Duration:    60 * time.Second, // Test duration
    RampUpTime:  10 * time.Second, // Gradual TPS increase

    // Session parameters
    TotalSessions:           1000,
    ModificationsPerSession: 2,
    ModificationInterval:    5 * time.Second,

    // Network config
    UEPoolCIDR:        "17.0.0.0/16",
    GnbAddress:        "192.168.1.1",
    N3Address:         "192.168.1.100",
    RemotePeerAddress: "192.168.1.100:8805",

    // Worker pool
    NumWorkers: 10,

    // Timeouts
    RequestTimeout: 5 * time.Second,
}
```

## Usage Example
```go
// Create async PFCP client
client := pfcpsim.NewAsyncPFCPClient("192.168.1.1")

// Connect to UPF
err := client.ConnectN4Async("192.168.1.100:8805")
if err != nil {
    log.Fatal(err)
}

// Setup association
err = client.SetupAssociation()
if err != nil {
    log.Fatal(err)
}

// Create configuration
config := NewDefaultStressConfig()
config.TPS = 200
config.TotalSessions = 5000

// Create controller
controller := NewStressController(config, client)

// Run stress test
err = controller.Run()
if err != nil {
    log.Fatal(err)
}

// Metrics are printed automatically
```

## Session Lifecycle
```
Phase 1: CREATE
├─ Rate limited session establishment
├─ PDR/FAR/QER/URR generation
└─ UE address allocation

Phase 2: MODIFY (concurrent with CREATE)
├─ Wait for ModificationInterval
├─ Update downlink FARs
├─ Update URRs
└─ Repeat N times

Phase 3: DELETE
├─ Delete all active sessions
└─ Rate limited cleanup
```

## Performance Tuning

### TPS Tuning
- Start with low TPS (10-50)
- Use ramp-up for gradual load increase
- Monitor UPF CPU and network

### Worker Pool Sizing
- General rule: `NumWorkers = TPS / 10`
- More workers = better throughput, more resources
- Too many = context switching overhead

### Request Timeout
- Network latency + UPF processing time
- Typical: 2-5 seconds
- Too short = false failures
- Too long = stuck requests

### Memory Optimization
- Limit concurrent sessions
- Use `ConcurrentSessions` to cap active sessions
- Monitor pending requests

## Metrics Interpretation

### Success Rate
- \> 99%: Excellent
- 95-99%: Good
- < 95%: Issues (check logs, UPF capacity)

### Latency
- P50: Typical case
- P95: Most requests
- P99: Worst case (should be < 2x P50)

### TPS
- Current: Instantaneous rate
- Average: Overall rate
- Should be close to target TPS

## Troubleshooting

### Low Success Rate
- Check UPF logs for errors
- Reduce TPS
- Increase request timeout
- Check network connectivity

### Low TPS
- Increase worker count
- Check rate limiter settings
- Monitor pending requests
- Check for bottlenecks

### High Latency
- Check network latency
- Monitor UPF CPU
- Reduce concurrent load
- Check for packet loss

### Memory Issues
- Reduce concurrent sessions
- Lower TPS
- Increase cleanup frequency
- Monitor goroutine count

## Best Practices

1. **Start Small**: Begin with 10-50 TPS and scale up
2. **Use Ramp-Up**: Gradual load increase prevents spikes
3. **Monitor Continuously**: Watch metrics during test
4. **Test Duration**: 1-5 minutes for quick tests, 30+ for stress
5. **Baseline First**: Test with low load to establish baseline
6. **Incremental Testing**: Increase load gradually
7. **Document Results**: Keep records of successful configurations

## Common Test Scenarios

### Quick Validation (1 minute)
```go
config.TPS = 50
config.Duration = 60 * time.Second
config.TotalSessions = 100
config.ModificationsPerSession = 1
```

### Sustained Load (10 minutes)
```go
config.TPS = 100
config.Duration = 10 * time.Minute
config.TotalSessions = 5000
config.ModificationsPerSession = 3
config.RampUpTime = 60 * time.Second
```

### Peak Load Test
```go
config.TPS = 500
config.Duration = 5 * time.Minute
config.TotalSessions = 10000
config.ModificationsPerSession = 2
config.RampUpTime = 120 * time.Second
config.NumWorkers = 50
```

### Endurance Test (1 hour)
```go
config.TPS = 200
config.Duration = 60 * time.Minute
config.TotalSessions = 50000
config.ModificationsPerSession = 5
config.RampUpTime = 300 * time.Second
```

### Stress Testing (NEW)

pfcpsim now includes comprehensive stress testing capabilities for PFCP agents.

#### Quick Start
```bash
# Basic stress test
pfcpctl stress --tps 100 --duration 60s --sessions 1000 \
  --remote-peer-addr 192.168.1.100:8805

# With modifications and ramp-up
pfcpctl stress --tps 200 --ramp-up 30s --duration 5m \
  --sessions 5000 --modifications 3 \
  --remote-peer-addr 192.168.1.100:8805
```

#### Key Features

- **Configurable TPS**: Control transaction rate with precise rate limiting
- **Ramp-up Support**: Gradually increase load to target TPS
- **Session Lifecycle**: Automatic create → modify → delete flow
- **Real-time Monitoring**: Live progress and metrics reporting
- **Detailed Analytics**: Latency percentiles (P50, P95, P99), success rates
- **Worker Pool**: Parallel processing with configurable concurrency
- **Graceful Shutdown**: Ctrl+C handling with cleanup

#### Performance Metrics

The stress test provides comprehensive metrics:
- Operation counts (create/modify/delete)
- Success and failure rates
- Latency statistics (min/avg/p50/p95/p99/max)
- Current and average TPS
- Active session count

See [STRESS_TEST_GUIDE.md](../../../docs/STRESS_TEST_GUIDE.md) for detailed documentation.