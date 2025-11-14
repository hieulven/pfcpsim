// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	"github.com/wmnsk/go-pfcp/message"
)

// PendingRequest tracks an outstanding PFCP request awaiting response
type PendingRequest struct {
	// SeqNum is the PFCP sequence number for matching request/response
	SeqNum uint32

	// MessageType stores the type of request sent (for validation)
	MessageType message.MessageType

	// SentAt records when the request was sent (for latency calculation)
	SentAt time.Time

	// ResponseCh is the channel where the response will be delivered
	ResponseCh chan ResponseResult

	// Timeout is the timer that will trigger if no response arrives
	Timeout *time.Timer

	// Context stores additional request context (optional)
	Context interface{}
}

// ResponseResult encapsulates the response message or error from a PFCP operation
type ResponseResult struct {
	// Message contains the parsed PFCP response (nil if error occurred)
	Message message.Message

	// Error contains any error that occurred (nil if successful)
	Error error

	// Latency is the time between sending request and receiving response
	Latency time.Duration

	// SeqNum is the sequence number for correlation
	SeqNum uint32
}

// AsyncPFCPClient extends PFCPClient with non-blocking, asynchronous operations.
// It maintains a map of pending requests and routes incoming responses to the
// appropriate channels.
type AsyncPFCPClient struct {
	// Embed the original PFCPClient for basic PFCP functionality
	*PFCPClient

	// pendingRequests maps sequence numbers to pending request info
	// Protected by pendingRequestsLock for thread-safe access
	pendingRequests map[uint32]*PendingRequest

	// pendingRequestsLock protects concurrent access to pendingRequests map
	pendingRequestsLock sync.RWMutex

	// responseChan is a buffered channel for routing responses
	// Not used for direct delivery, but could be used for monitoring
	responseChan chan ResponseResult

	// stats tracks async operation statistics
	stats AsyncStats

	// defaultTimeout is the default timeout for operations if not specified
	defaultTimeout time.Duration
}

// AsyncStats tracks statistics for async PFCP operations
// All counters use atomic operations for thread safety
type AsyncStats struct {
	// TotalSent counts total requests sent
	TotalSent int64

	// TotalReceived counts total responses received
	TotalReceived int64

	// TotalTimeout counts requests that timed out
	TotalTimeout int64

	// TotalErrors counts requests that failed with errors
	TotalErrors int64

	// AverageLatency tracks running average of request latency
	AverageLatency time.Duration

	// lock protects AverageLatency updates
	lock sync.RWMutex
}

// NewAsyncPFCPClient creates a new async PFCP client with the given local address.
// The client must be connected via ConnectN4 before use.
//
// Parameters:
//   - localAddr: The local IP address to bind to (e.g., "192.168.1.100")
//
// Returns:
//   - *AsyncPFCPClient: A new async PFCP client instance
func NewAsyncPFCPClient(localAddr string) *AsyncPFCPClient {
	client := &AsyncPFCPClient{
		PFCPClient:      NewPFCPClient(localAddr),
		pendingRequests: make(map[uint32]*PendingRequest),
		responseChan:    make(chan ResponseResult, 1000), // Buffered for high throughput
		defaultTimeout:  5 * time.Second,
	}

	return client
}

// SetDefaultTimeout sets the default timeout for async operations
func (c *AsyncPFCPClient) SetDefaultTimeout(timeout time.Duration) {
	c.defaultTimeout = timeout
}

// GetDefaultTimeout returns the current default timeout
func (c *AsyncPFCPClient) GetDefaultTimeout() time.Duration {
	return c.defaultTimeout
}

// SendAsync sends a PFCP message without blocking for a response.
// It returns a channel that will receive the response or timeout error.
//
// Parameters:
//   - msg: The PFCP message to send
//   - timeout: How long to wait for a response (0 = use default timeout)
//
// Returns:
//   - <-chan ResponseResult: Channel that will receive the response
//   - error: Error if sending failed immediately
//
// The caller should read from the returned channel to get the response:
//
//	resultCh, err := client.SendAsync(msg, 5*time.Second)
//	if err != nil {
//	    // Handle send error
//	}
//	result := <-resultCh
//	if result.Error != nil {
//	    // Handle timeout or response error
//	}
//	// Process result.Message
func (c *AsyncPFCPClient) SendAsync(msg message.Message, timeout time.Duration) (<-chan ResponseResult, error) {
	// Use default timeout if not specified
	if timeout == 0 {
		timeout = c.defaultTimeout
	}

	// Get next sequence number for this request
	seqNum := c.getNextSequenceNumber()

	// Create pending request structure
	pending := &PendingRequest{
		SeqNum:      seqNum,
		MessageType: msg.MessageType(),
		SentAt:      time.Now(),
		ResponseCh:  make(chan ResponseResult, 1), // Buffered to prevent blocking
	}

	// Set up timeout handler
	// This will be called if no response arrives within the timeout period
	pending.Timeout = time.AfterFunc(timeout, func() {
		c.handleTimeout(seqNum)
	})

	// Register this pending request BEFORE sending
	// This ensures we don't miss a fast response
	c.pendingRequestsLock.Lock()
	c.pendingRequests[seqNum] = pending
	c.pendingRequestsLock.Unlock()

	// Send the message
	if err := c.sendMsg(msg); err != nil {
		// Send failed, cleanup the pending request
		c.removePendingRequest(seqNum)
		return nil, err
	}

	// Update statistics
	c.stats.incrementSent()

	// Return read-only channel
	return pending.ResponseCh, nil
}

// handleTimeout is called when a request times out without receiving a response.
// It removes the pending request and sends a timeout error to the response channel.
//
// Parameters:
//   - seqNum: The sequence number of the timed-out request
func (c *AsyncPFCPClient) handleTimeout(seqNum uint32) {
	// Remove the pending request
	c.pendingRequestsLock.Lock()
	pending, exists := c.pendingRequests[seqNum]
	if !exists {
		// Request was already handled (response arrived just before timeout)
		c.pendingRequestsLock.Unlock()
		return
	}
	delete(c.pendingRequests, seqNum)
	c.pendingRequestsLock.Unlock()

	// Update timeout statistics
	c.stats.incrementTimeout()

	// Calculate how long we waited
	latency := time.Since(pending.SentAt)

	// Send timeout error to the waiting goroutine
	pending.ResponseCh <- ResponseResult{
		Message: nil,
		Error:   NewTimeoutExpiredError(),
		Latency: latency,
		SeqNum:  seqNum,
	}
	close(pending.ResponseCh)
}

// removePendingRequest removes a pending request and stops its timeout timer.
// Returns the removed request, or nil if it doesn't exist.
//
// Parameters:
//   - seqNum: The sequence number of the request to remove
//
// Returns:
//   - *PendingRequest: The removed request, or nil if not found
func (c *AsyncPFCPClient) removePendingRequest(seqNum uint32) *PendingRequest {
	c.pendingRequestsLock.Lock()
	defer c.pendingRequestsLock.Unlock()

	pending, exists := c.pendingRequests[seqNum]
	if !exists {
		return nil
	}

	// Stop the timeout timer if it hasn't fired yet
	if pending.Timeout != nil {
		pending.Timeout.Stop()
	}

	// Remove from map
	delete(c.pendingRequests, seqNum)

	return pending
}

// GetPendingRequestCount returns the current number of pending requests
// This is useful for monitoring and debugging
func (c *AsyncPFCPClient) GetPendingRequestCount() int {
	c.pendingRequestsLock.RLock()
	defer c.pendingRequestsLock.RUnlock()
	return len(c.pendingRequests)
}

// GetPendingRequestIDs returns a slice of all pending request sequence numbers
// This is useful for debugging and monitoring
func (c *AsyncPFCPClient) GetPendingRequestIDs() []uint32 {
	c.pendingRequestsLock.RLock()
	defer c.pendingRequestsLock.RUnlock()

	ids := make([]uint32, 0, len(c.pendingRequests))
	for seqNum := range c.pendingRequests {
		ids = append(ids, seqNum)
	}
	return ids
}

// CleanupPendingRequests closes all pending requests with an error
// This should be called during shutdown
func (c *AsyncPFCPClient) CleanupPendingRequests() {
	c.pendingRequestsLock.Lock()
	pendingCopy := make(map[uint32]*PendingRequest)
	for k, v := range c.pendingRequests {
		pendingCopy[k] = v
	}
	// Clear the map
	c.pendingRequests = make(map[uint32]*PendingRequest)
	c.pendingRequestsLock.Unlock()

	// Send errors to all pending requests
	for _, pending := range pendingCopy {
		if pending.Timeout != nil {
			pending.Timeout.Stop()
		}

		select {
		case pending.ResponseCh <- ResponseResult{
			Error:   NewAssociationInactiveError(),
			Latency: time.Since(pending.SentAt),
			SeqNum:  pending.SeqNum,
		}:
		default:
			// Channel already closed or full, skip
		}
		close(pending.ResponseCh)
	}
}

// ConnectN4Async establishes connection and starts async receiver
// This is the proper way to connect an async client
func (c *AsyncPFCPClient) ConnectN4Async(remoteAddr string) error {
	addr := fmt.Sprintf("%s:%d", remoteAddr, PFCPStandardPort)

	if host, port, err := net.SplitHostPort(remoteAddr); err == nil {
		// remoteAddr contains also a port. Use provided port instead of PFCPStandardPort
		addr = fmt.Sprintf("%s:%s", host, port)
	}

	c.remoteAddr = addr

	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", c.localAddr, PFCPStandardPort))
	if err != nil {
		return err
	}

	rxconn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return err
	}

	c.conn = rxconn

	// Start async receiver instead of blocking receiver
	go c.receiveFromN4Async()

	logger.PfcpsimLog.Infof("Async PFCP client connected to %s", c.remoteAddr)

	return nil
}

// SetAssociationStatus sets the association status (for testing)
func (c *AsyncPFCPClient) SetAssociationStatus(active bool) {
	c.aliveLock.Lock()
	defer c.aliveLock.Unlock()
	c.isAssociationActive = active
}

// SetContext sets the context (for testing)
func (c *AsyncPFCPClient) SetContext(ctx context.Context) {
	c.ctx = ctx
}