# Async PFCP Client Design

## Overview

The async PFCP client provides non-blocking operations for PFCP message exchange, enabling high-throughput stress testing and concurrent session management.

## Architecture

### Core Components

1. **AsyncPFCPClient** - Main async client with pending request tracking
2. **PendingRequest** - Tracks individual outstanding requests
3. **ResponseResult** - Encapsulates response or error
4. **AsyncStats** - Thread-safe statistics tracking

### Request Flow
```
SendAsync() → Register Pending → Send UDP → Wait on Channel
                                              ↓
                                    ← Route Response ← Receive UDP
                                    ← Timeout Handler
```

### Threading Model

- **Main Goroutine**: Application logic
- **Receiver Goroutine**: `receiveFromN4Async()` - routes incoming messages
- **Timeout Goroutines**: One timer per request, fires `handleTimeout()`

### Thread Safety

All concurrent access protected by:
- `pendingRequestsLock` (RWMutex) - protects pendingRequests map
- Atomic operations - for statistics counters
- Buffered channels - for response delivery

## Key Features

### 1. Non-Blocking Operations
```go
// Traditional blocking
response, err := client.EstablishSession(...)

// Async non-blocking
resultCh, err := client.EstablishSessionAsync(...)
result := <-resultCh // Wait when ready
```

### 2. Automatic Timeout Handling

Each request has individual timeout with automatic cleanup.

### 3. Response Routing

Sequence numbers match requests to responses:
- O(1) lookup via map
- Thread-safe access
- Handles out-of-order responses

### 4. Statistics Tracking

- Total sent/received/timeout/errors
- Average latency (exponential moving average)
- Thread-safe snapshots

## Usage Examples

### Basic Async Request
```go
client := NewAsyncPFCPClient("192.168.1.1")
err := client.ConnectN4Async("192.168.1.100:8805")

// Send request
resultCh, err := client.SendAsync(message, 5*time.Second)
if err != nil {
    // Handle send error
}

// Wait for response
result := <-resultCh
if result.Error != nil {
    // Handle timeout or error
}
// Process result.Message
```

### Concurrent Operations
```go
// Send multiple requests without waiting
var channels []<-chan ResponseResult

for i := 0; i < 100; i++ {
    ch, _ := client.SendAsync(messages[i], 5*time.Second)
    channels = append(channels, ch)
}

// Collect results
for _, ch := range channels {
    result := <-ch
    // Process result
}
```

### With Context and Cancellation
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resultCh, _ := client.SendAsync(msg, 5*time.Second)

select {
case result := <-resultCh:
    // Process result
case <-ctx.Done():
    // Cancel operation
    client.CancelPendingRequest(seqNum)
}
```

## Performance Characteristics

### Throughput
- **Blocking client**: ~10-50 TPS (serial operations)
- **Async client**: ~1000-5000+ TPS (parallel operations)

### Memory
- Per pending request: ~200 bytes
- 10,000 pending requests: ~2 MB

### Latency Overhead
- Async overhead: < 1ms
- Channel communication: < 100μs

## Error Handling

### Timeout Errors
- Request removed from pending map
- Timeout error sent to channel
- Statistics updated
- Channel closed

### Unexpected Responses
- Logged as warning
- Error counter incremented
- Response discarded

### Connection Errors
- All pending requests notified
- Association marked inactive
- Graceful cleanup

## Monitoring and Debugging

### Get Pending Request Count
```go
count := client.GetPendingRequestCount()
```

### Get Oldest Request
```go
info := client.GetOldestPendingRequest()
fmt.Printf("Oldest request: %d, age: %v\n", info.SeqNum, info.Age)
```

### Get All Pending Info
```go
infos := client.GetPendingRequestsInfo()
for _, info := range infos {
    fmt.Printf("SeqNum: %d, Age: %v\n", info.SeqNum, info.Age)
}
```

### Statistics
```go
snapshot := client.stats.GetSnapshot()
fmt.Printf("Sent: %d, Received: %d, Timeout: %d\n",
    snapshot.TotalSent,
    snapshot.TotalReceived,
    snapshot.TotalTimeout)
fmt.Printf("Success Rate: %.1f%%\n", snapshot.SuccessRate())
fmt.Printf("Avg Latency: %v\n", snapshot.AverageLatency)
```

## Cleanup and Shutdown

### Graceful Shutdown
```go
// Stop receiver
client.cancel()

// Cleanup all pending requests
client.CleanupPendingRequests()

// Close connection
client.DisconnectN4()
```

### Stale Request Cleanup
```go
// Cleanup requests older than 10 seconds
cleaned := client.CleanupStalePendingRequests(10 * time.Second)
```

## Testing

### Unit Tests
- `pfcpsim_async_test.go` - Basic functionality
- `pfcpsim_async_routing_test.go` - Response routing
- `pfcpsim_async_timeout_test.go` - Timeout handling
- `pfcpsim_async_concurrency_test.go` - Thread safety
- `pfcpsim_async_edge_cases_test.go` - Edge cases

### Integration Tests
- `pfcpsim_async_integration_test.go` - End-to-end with mock UPF

### Stress Tests
- `TestStressLoadAsync` - 10,000 concurrent requests
- `TestConcurrentMixedOperations` - Mixed operations under load

## Best Practices

1. **Always check for send errors**
```go
   resultCh, err := client.SendAsync(msg, timeout)
   if err != nil {
       // Handle error
   }
```

2. **Set appropriate timeouts**
   - Too short: False timeouts
   - Too long: Stuck requests
   - Recommended: 2-10 seconds depending on network

3. **Monitor pending requests**
```go
   if client.GetPendingRequestCount() > 1000 {
       log.Warn("High pending request count")
   }
```

4. **Cleanup on shutdown**
```go
   defer client.CleanupPendingRequests()
```

5. **Use context for cancellation**
```go
   ctx, cancel := context.WithTimeout(...)
   defer cancel()
```

## Future Enhancements

- [ ] Request retry mechanism
- [ ] Priority queues for requests
- [ ] Request batching
- [ ] Adaptive timeout based on RTT
- [ ] Circuit breaker pattern
- [ ] Request rate limiting built-in
- [ ] Histogram-based latency tracking