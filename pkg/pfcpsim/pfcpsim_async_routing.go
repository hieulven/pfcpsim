// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"time"
	"github.com/omec-project/pfcpsim/logger"
	"github.com/wmnsk/go-pfcp/message"
)

// receiveFromN4Async is the async version of the receiver that routes responses
// to the appropriate pending request channels based on sequence numbers.
// This replaces the blocking receiver for async operations.
func (c *AsyncPFCPClient) receiveFromN4Async() {
	buf := make([]byte, 3000)

	for {
		select {
		case <-c.ctx.Done():
			logger.PfcpsimLog.Infoln("Async receiver stopping due to context cancellation")
			return
		default:
			// Non-blocking read from UDP socket
			n, _, err := c.conn.ReadFrom(buf)
			if err != nil {
				// Check if context was cancelled
				select {
				case <-c.ctx.Done():
					return
				default:
					// Log error but continue receiving
					logger.PfcpsimLog.Debugf("Error reading from socket: %v", err)
					continue
				}
			}

			// Parse PFCP message
			msg, err := message.Parse(buf[:n])
			if err != nil {
				logger.PfcpsimLog.Warnf("Failed to parse PFCP message: %v", err)
				continue
			}

			// Route message based on type
			c.routeMessage(msg)
		}
	}
}

// routeMessage determines where to send the received message
// Different message types are handled differently:
// - Heartbeat responses go to heartbeat channel
// - Session report requests are handled inline
// - All other responses are routed to pending requests
func (c *AsyncPFCPClient) routeMessage(msg message.Message) {
	switch msg := msg.(type) {
	case *message.HeartbeatResponse:
		// Route to heartbeat channel (original behavior)
		select {
		case c.heartbeatsChan <- msg:
			logger.PfcpsimLog.Debugln("Heartbeat response routed to heartbeat channel")
		default:
			logger.PfcpsimLog.Warnln("Heartbeat channel full, dropping response")
		}

	case *message.HeartbeatRequest:
		// Handle heartbeat request from peer (send response)
		logger.PfcpsimLog.Debugln("Received heartbeat request from peer")
		// Note: Heartbeat request handling could be implemented here
		// For now, we just log it

	case *message.SessionReportRequest:
		// Handle session report request inline
		logger.PfcpsimLog.Infoln("Received session report request")
		if c.handleSessionReportRequest(msg) {
			// Successfully handled, continue to next message
			return
		}
		// If not handled, fall through to route as response
		c.routeResponse(msg)

	default:
		// Route to pending request
		c.routeResponse(msg)
	}
}

// routeResponse matches a response message to its pending request and delivers it.
// This is the core of the async response handling.
//
// The process:
// 1. Extract sequence number from response
// 2. Find and remove the pending request
// 3. Calculate latency
// 4. Send response to the waiting channel
// 5. Update statistics
func (c *AsyncPFCPClient) routeResponse(msg message.Message) {
	// Extract sequence number from message
	seqNum := msg.Sequence()

	logger.PfcpsimLog.Debugf("Routing response with sequence number %d, type %s",
		seqNum, msg.MessageTypeName())

	// Find and remove the pending request
	pending := c.removePendingRequest(seqNum)
	if pending == nil {
		// No pending request found for this sequence number
		// This could happen if:
		// - Request already timed out
		// - Duplicate response received
		// - Response for unknown request
		logger.PfcpsimLog.Warnf(
			"Received unexpected response for sequence number %d (type: %s), no pending request found",
			seqNum,
			msg.MessageTypeName(),
		)
		c.stats.incrementErrors()
		return
	}

	// Validate message type matches what we expected
	if !c.isExpectedResponseType(pending.MessageType, msg.MessageType()) {
		logger.PfcpsimLog.Warnf(
			"Response type mismatch: sent %s, received %s for sequence %d",
			pending.MessageType,
			msg.MessageType(),
			seqNum,
		)
		// Still deliver it, but log the mismatch
	}

	// Calculate latency
	latency := time.Since(pending.SentAt)

	// Update statistics
	c.stats.recordLatency(latency)
	c.stats.incrementReceived()

	logger.PfcpsimLog.Debugf("Response for sequence %d received after %v", seqNum, latency)

	// Create response result
	result := ResponseResult{
		Message: msg,
		Error:   nil,
		Latency: latency,
		SeqNum:  seqNum,
	}

	// Send response to the waiting channel
	// Use select with default to prevent blocking if channel is closed
	select {
	case pending.ResponseCh <- result:
		// Successfully delivered
		logger.PfcpsimLog.Debugf("Response delivered to waiting channel for sequence %d", seqNum)
	default:
		// Channel was closed or full (shouldn't happen with buffered channel)
		logger.PfcpsimLog.Warnf("Failed to deliver response for sequence %d, channel closed or full", seqNum)
		c.stats.incrementErrors()
	}

	// Close the response channel to signal completion
	defer func() {
		if r := recover(); r != nil {
			logger.PfcpsimLog.Debugf("Channel already closed for sequence %d", seqNum)
		}
	}()
	close(pending.ResponseCh)
}

// isExpectedResponseType checks if the response type matches the request type.
// PFCP request/response pairs have predictable relationships.
func (c *AsyncPFCPClient) isExpectedResponseType(
	requestType uint8,
	responseType uint8,
) bool {
	// Map request types to expected response types
	expectedResponses := map[uint8]uint8{
		message.MsgTypeAssociationSetupRequest:       message.MsgTypeAssociationSetupResponse,
		message.MsgTypeAssociationReleaseRequest:     message.MsgTypeAssociationReleaseResponse,
		message.MsgTypeSessionEstablishmentRequest:   message.MsgTypeSessionEstablishmentResponse,
		message.MsgTypeSessionModificationRequest:    message.MsgTypeSessionModificationResponse,
		message.MsgTypeSessionDeletionRequest:        message.MsgTypeSessionDeletionResponse,
		message.MsgTypeHeartbeatRequest:              message.MsgTypeHeartbeatResponse,
		message.MsgTypeSessionReportResponse:         message.MsgTypeSessionReportRequest,
		message.MsgTypePFDManagementRequest:          message.MsgTypePFDManagementResponse,
		message.MsgTypeNodeReportRequest:             message.MsgTypeNodeReportResponse,
		message.MsgTypeSessionSetDeletionRequest:     message.MsgTypeSessionSetDeletionResponse,
		message.MsgTypeAssociationUpdateRequest:      message.MsgTypeAssociationUpdateResponse,
	}

	expected, exists := expectedResponses[requestType]
	if !exists {
		// Unknown request type, can't validate
		logger.PfcpsimLog.Warnf("Unknown request type for validation: %d", requestType)
		return true // Don't fail on unknown types
	}

	return expected == responseType
}

// StartAsyncReceiver starts the async receiver in a goroutine.
// This should be called after ConnectN4.
func (c *AsyncPFCPClient) StartAsyncReceiver() {
	go c.receiveFromN4Async()
	logger.PfcpsimLog.Infoln("Async PFCP receiver started")
}

// ConnectN4Async is the async version of ConnectN4 that uses the async receiver.
// It replaces the original ConnectN4 for async clients.
// func (c *AsyncPFCPClient) ConnectN4Async(remoteAddr string) error {
// 	// Use the parent's ConnectN4 to establish connection
// 	// but don't start the old receiver
// 	err := c.PFCPClient.ConnectN4(remoteAddr)
// 	if err != nil {
// 		return err
// 	}

// 	// The parent ConnectN4 starts receiveFromN4(), but we need to stop it
// 	// and start our async version instead.
// 	// Since we can't easily stop the parent's receiver, we'll override
// 	// the connection method completely.

// 	return nil
// }