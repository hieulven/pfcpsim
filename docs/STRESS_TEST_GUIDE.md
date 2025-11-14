# PFCP Stress Test Guide

## Overview

The `pfcpctl stress` command provides comprehensive stress testing capabilities for PFCP agents (UPFs). It simulates realistic session lifecycles with configurable load patterns and provides detailed performance metrics.

## Quick Start

### Basic Stress Test
```bash
pfcpctl stress \
  --tps 100 \
  --duration 60s \
  --sessions 1000 \
  --remote-peer-addr 192.168.1.100:8805
```

This creates 1000 sessions at 100 TPS over 60 seconds.

### With Modifications
```bash
pfcpctl stress \
  --tps 100 \
  --duration 5m \
  --sessions 5000 \
  --modifications 3 \
  --modify-interval 10s \
  --remote-peer-addr 192.168.1.100:8805
```

Each session will be modified 3 times with 10 seconds between modifications.

### With Ramp-Up
```bash
pfcpctl stress \
  --tps 200 \
  --ramp-up 30s \
  --duration 5m \
  --sessions 10000 \
  --remote-peer-addr 192.168.1.100:8805
```

TPS gradually increases from 1 to 200 over 30 seconds.

## Command Line Options

### Rate Control Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--tps` | `-t` | 100 | Target transactions per second |
| `--duration` | `-d` | 60s | Test duration |
| `--ramp-up` | `-r` | 0 | Ramp-up time for gradual TPS increase |

### Session Configuration

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--sessions` | `-s` | 1000 | Total sessions to create |
| `--concurrent` | - | 500 | Max concurrent active sessions |
| `--base-id` | - | 1 | Starting session ID |
| `--modifications` | `-m` | 2 | Modifications per session |
| `--modify-interval` | - | 5s | Time between modifications |

### Network Configuration

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--ue-pool` | `-u` | 17.0.0.0/16 | UE IP address pool |
| `--gnb-addr` | `-g` | 192.168.1.1 | gNodeB IP address |
| `--n3-addr` | `-n` | 192.168.1.100 | N3 interface address |
| `--remote-peer-addr` | `-p` | 127.0.0.1:8805 | PFCP agent address |
| `--qfi` | `-q` | 9 | QoS Flow Identifier |

### Application Filters

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--app-filter` | `-a` | ip:any:any:allow:100 | Application filter rules |

Format: `{protocol}:{network}:{ports}:{action}:{precedence}`

Examples:
- `ip:any:any:allow:100` - Allow all IP traffic
- `udp:10.0.0.0/8:80-88:allow:200` - Allow UDP to 10.0.0.0/8 ports 80-88
- `tcp:any:443-443:deny:50` - Deny TCP port 443

### Performance Tuning

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--workers` | `-w` | 10 | Number of worker threads |
| `--timeout` | - | 5s | PFCP request timeout |
| `--local-addr` | - | 127.0.0.1 | Local IP to bind to |

## Usage Scenarios

### 1. Quick Validation Test (1 minute)

Verify basic functionality:
```bash
pfcpctl stress \
  --tps 50 \
  --duration 60s \
  --sessions 100 \
  --modifications 1 \
  --remote-peer-addr 192.168.1.100:8805
```

**Expected Results:**
- Success rate > 99%
- Average latency < 50ms
- All sessions created and deleted

### 2. Sustained Load Test (10 minutes)

Test sustained performance:
```bash
pfcpctl stress \
  --tps 100 \
  --duration 10m \
  --sessions 5000 \
  --modifications 3 \
  --ramp-up 60s \
  --workers 15 \
  --remote-peer-addr 192.168.1.100:8805
```

**Expected Results:**
- Success rate > 95%
- Stable TPS throughout test
- No memory leaks

### 3. Peak Load Test (5 minutes)

Find maximum capacity:
```bash
pfcpctl stress \
  --tps 500 \
  --duration 5m \
  --sessions 10000 \
  --modifications 2 \
  --ramp-up 120s \
  --workers 50 \
  --timeout 10s \
  --remote-peer-addr 192.168.1.100:8805
```

**Expected Results:**
- Identify maximum sustainable TPS
- Monitor UPF resource usage
- Track error patterns

### 4. Endurance Test (1 hour)

Long-running stability test:
```bash
pfcpctl stress \
  --tps 200 \
  --duration 1h \
  --sessions 50000 \
  --modifications 5 \
  --ramp-up 5m \
  --workers 30 \
  --remote-peer-addr 192.168.1.100:8805
```

**Expected Results:**
- Stable performance over time
- No degradation
- Consistent resource usage

### 5. Multiple Application Flows

Test with multiple app filters:
```bash
pfcpctl stress \
  --tps 100 \
  --duration 5m \
  --sessions 1000 \
  --app-filter "udp:10.0.0.0/8:80-80:allow:100" \
  --app-filter "tcp:10.0.0.0/8:443-443:allow:100" \
  --app-filter "ip:any:any:deny:200" \
  --remote-peer-addr 192.168.1.100:8805
```

## Understanding the Output

### Progress Reports

Every 5 seconds during the test:
```
Progress: Created=234/1000, Modified=145, Deleted=89, Active=145, TPS=98.5, Success=99.2%
```

- **Created**: Sessions created so far / total target
- **Modified**: Total modifications performed
- **Deleted**: Sessions deleted
- **Active**: Currently active sessions
- **TPS**: Current transactions per second
- **Success**: Success rate percentage

### Final Report

At the end of the test:
```
================================================================================
STRESS TEST RESULTS
================================================================================

Duration: 60s
Total Operations: 3234
Success Rate: 99.12%
Failed Operations: 28
Average TPS: 53.90
Current TPS: 54.20

--- CREATE Operations ---
Total: 1000 (Success: 998, Failed: 2)
Create Latency:
  Min: 12.453ms
  Avg: 45.234ms
  P50: 43.123ms
  P95: 78.456ms
  P99: 95.234ms
  Max: 125.678ms
  Samples: 998

--- MODIFY Operations ---
Total: 2000 (Success: 1988, Failed: 12)
Modify Latency:
  Min: 8.234ms
  Avg: 32.456ms
  P50: 30.123ms
  P95: 52.345ms
  P99: 68.234ms
  Max: 89.456ms
  Samples: 1988

--- DELETE Operations ---
Total: 234 (Success: 234, Failed: 0)
Delete Latency:
  Min: 5.123ms
  Avg: 18.234ms
  P50: 16.789ms
  P95: 28.456ms
  P99: 35.123ms
  Max: 42.345ms
  Samples: 234

================================================================================
```

## Interpreting Metrics

### Success Rate
- **> 99%**: Excellent - UPF handling load well
- **95-99%**: Good - Minor issues, investigate logs
- **90-95%**: Fair - Significant issues, reduce load
- **< 90%**: Poor - UPF struggling, check capacity

### Latency Percentiles
- **P50 (Median)**: Typical case, should be consistent
- **P95**: 95% of requests complete within this time
- **P99**: Worst 1% of requests, indicates outliers
- **Max**: Absolute worst case

**Healthy Latency Pattern:**
- P50: 20-50ms
- P95: < 2x P50
- P99: < 3x P50
- Max: < 5x P50

### TPS (Transactions Per Second)
- **Current TPS**: Instantaneous rate (last 5 seconds)
- **Average TPS**: Overall rate for entire test
- Should be close to target TPS
- Significant deviation indicates bottleneck

## Troubleshooting

### Problem: Low Success Rate (< 95%)

**Symptoms:**
- Many failed operations
- High error count in logs

**Solutions:**
1. Reduce TPS: `--tps 50`
2. Increase timeout: `--timeout 10s`
3. Check UPF logs for errors
4. Verify network connectivity
5. Check UPF resource usage (CPU, memory)

### Problem: Low Actual TPS (< 80% of target)

**Symptoms:**
- Average TPS much lower than target
- High pending request count

**Solutions:**
1. Increase workers: `--workers 20`
2. Check for rate limiter issues
3. Monitor goroutine count
4. Check network bandwidth
5. Verify UPF can handle load

### Problem: High Latency

**Symptoms:**
- P95 > 100ms
- P99 >> P50

**Solutions:**
1. Check network latency: `ping` to UPF
2. Monitor UPF CPU usage
3. Reduce concurrent load
4. Check for packet loss
5. Increase timeout if needed

### Problem: Memory Growth

**Symptoms:**
- Increasing memory usage
- Eventually crashes

**Solutions:**
1. Reduce concurrent sessions: `--concurrent 200`
2. Lower TPS temporarily
3. Check for goroutine leaks
4. Monitor pending requests
5. Increase cleanup frequency

### Problem: Connection Failures

**Symptoms:**
- "Failed to connect" errors
- Association failures

**Solutions:**
1. Verify UPF is running and reachable
2. Check firewall rules
3. Verify PFCP port (default 8805)
4. Test with `ping` and `telnet`
5. Check UPF logs for connection errors

## Best Practices

### 1. Start Small, Scale Up
```bash
# Start
--tps 10 --sessions 50

# Then increase
--tps 50 --sessions 200

# Then scale
--tps 100 --sessions 1000
```

### 2. Always Use Ramp-Up for High Load
```bash
# For TPS > 100
--ramp-up 30s

# For TPS > 200
--ramp-up 60s

# For TPS > 500
--ramp-up 120s
```

### 3. Monitor During Testing
- Watch UPF CPU usage
- Monitor network bandwidth
- Check memory consumption
- Review real-time progress

### 4. Save Results
```bash
pfcpctl stress ... 2>&1 | tee stress_test_results.log
```

### 5. Document Successful Configurations
Keep a record of successful test parameters for future reference.

### 6. Test Incrementally
Don't jump from 100 to 1000 TPS. Increase gradually:
- 100 → 200 → 500 → 1000

## Advanced Usage

### Testing with Custom Local Address
```bash
pfcpctl stress \
  --local-addr 192.168.1.50 \
  --remote-peer-addr 192.168.1.100:8805 \
  --tps 100
```

### High Worker Count for Maximum Throughput
```bash
pfcpctl stress \
  --workers 50 \
  --tps 1000 \
  --sessions 50000 \
  --remote-peer-addr 192.168.1.100:8805
```

### Custom UE Address Pool
```bash
pfcpctl stress \
  --ue-pool 10.0.0.0/8 \
  --tps 100 \
  --sessions 1000 \
  --remote-peer-addr 192.168.1.100:8805
```

### Extended Timeout for Slow Networks
```bash
pfcpctl stress \
  --timeout 15s \
  --tps 50 \
  --remote-peer-addr 192.168.1.100:8805
```

## Common Issues and Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Connection timeout | UPF not reachable | Check network, firewall, UPF status |
| Association failed | PFCP protocol issue | Verify UPF PFCP support, check versions |
| High failure rate | UPF overloaded | Reduce TPS, increase timeout |
| Memory leak | Too many pending requests | Reduce concurrent sessions |
| Low TPS | Insufficient workers | Increase `--workers` |
| Timeouts | Network or UPF slow | Increase `--timeout` |

## Example Test Plan

### Day 1: Baseline Testing
1. Quick validation: 50 TPS, 1 minute
2. Basic load: 100 TPS, 5 minutes
3. Document baseline performance

### Day 2: Capacity Testing
1. Incremental load: 100 → 200 → 500 TPS
2. Find breaking point
3. Document maximum capacity

### Day 3: Endurance Testing
1. Run at 70% of max capacity
2. Test for 1 hour
3. Monitor for degradation

### Day 4: Stress Testing
1. Run at 120% of max capacity
2. Observe failure modes
3. Document recovery behavior

## Support and Feedback

For issues or questions:
1. Check pfcpctl logs
2. Review UPF logs
3. Verify configuration
4. Contact support with full test parameters and results