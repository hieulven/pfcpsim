// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"fmt"
	"net"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	ieLib "github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// SessionOperation represents an async session operation in progress.
// It holds the result channel and metadata about the operation.
type SessionOperation struct {
	// SessionID is the application-level session identifier
	SessionID int

	// Type indicates what kind of operation this is
	Type SessionOpType

	// ResultCh is the channel where the response will arrive
	ResultCh <-chan ResponseResult

	// StartTime records when the operation started
	StartTime time.Time

	// LocalSEID is the local Session Endpoint ID (for context)
	LocalSEID uint64

	// SequenceNumber is the PFCP sequence number used
	SequenceNumber uint32
}

// SessionOpType identifies the type of session operation
type SessionOpType int

const (
	// OpCreate represents a session establishment operation
	OpCreate SessionOpType = iota

	// OpModify represents a session modification operation
	OpModify

	// OpDelete represents a session deletion operation
	OpDelete
)

// String returns the string representation of SessionOpType
func (t SessionOpType) String() string {
	switch t {
	case OpCreate:
		return "CREATE"
	case OpModify:
		return "MODIFY"
	case OpDelete:
		return "DELETE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// EstablishSessionAsync sends a session establishment request without blocking.
// It returns a SessionOperation that can be used to wait for the result.
//
// Parameters:
//   - sessionID: Application-level session identifier
//   - pdrs: Packet Detection Rules to create
//   - fars: Forwarding Action Rules to create
//   - qers: QoS Enforcement Rules to create
//   - urrs: Usage Reporting Rules to create
//   - timeout: How long to wait for response (0 = use default)
//
// Returns:
//   - *SessionOperation: Operation handle to retrieve result
//   - error: Immediate error if request couldn't be sent
//
// Example:
//   op, err := client.EstablishSessionAsync(1, pdrs, fars, qers, urrs, 5*time.Second)
//   if err != nil {
//       return err
//   }
//   session, err := client.ProcessSessionResult(op, localSEID)
func (c *AsyncPFCPClient) EstablishSessionAsync(
	sessionID int,
	pdrs []*ieLib.IE,
	fars []*ieLib.IE,
	qers []*ieLib.IE,
	urrs []*ieLib.IE,
	timeout time.Duration,
) (*SessionOperation, error) {
	// Check association status
	if !c.isAssociationActive {
		logger.PfcpsimLog.Errorln("Cannot establish session: association is not active")
		return nil, NewAssociationInactiveError()
	}

	// Get next F-SEID for this session
	fseid := c.getNextFSEID()

	// Build session establishment request
	estReq := message.NewSessionEstablishmentRequest(
		0, // MP flag
		0, // S flag  
		0, // SEID (0 for initial request)
		0, // Sequence number (will be set by SendAsync)
		0, // Priority
		ieLib.NewNodeID(c.localAddr, "", ""),
		ieLib.NewFSEID(fseid, net.ParseIP(c.localAddr), nil),
		ieLib.NewPDNType(ieLib.PDNTypeIPv4),
	)

	// Add session rules
	estReq.CreatePDR = append(estReq.CreatePDR, pdrs...)
	estReq.CreateFAR = append(estReq.CreateFAR, fars...)
	estReq.CreateQER = append(estReq.CreateQER, qers...)
	estReq.CreateURR = append(estReq.CreateURR, urrs...)

	logger.PfcpsimLog.Infof(
		"Sending async session establishment for session %d (F-SEID: %d, PDRs: %d, FARs: %d, QERs: %d, URRs: %d)",
		sessionID, fseid, len(pdrs), len(fars), len(qers), len(urrs),
	)

	// Send async
	resultCh, err := c.SendAsync(estReq, timeout)
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to send session establishment request: %v", err)
		return nil, err
	}

	// Create operation handle
	op := &SessionOperation{
		SessionID:      sessionID,
		Type:           OpCreate,
		ResultCh:       resultCh,
		StartTime:      time.Now(),
		LocalSEID:      fseid,
		SequenceNumber: estReq.Sequence(),
	}

	logger.PfcpsimLog.Debugf(
		"Session establishment request sent for session %d (seq: %d)",
		sessionID, op.SequenceNumber,
	)

	return op, nil
}

// ModifySessionAsync sends a session modification request without blocking.
//
// Parameters:
//   - sessionID: Application-level session identifier
//   - peerSEID: Remote peer's Session Endpoint ID
//   - pdrs: PDRs to update (can be nil)
//   - fars: FARs to update (can be nil)
//   - qers: QERs to update (can be nil)
//   - urrs: URRs to update (can be nil)
//   - timeout: How long to wait for response (0 = use default)
//
// Returns:
//   - *SessionOperation: Operation handle to retrieve result
//   - error: Immediate error if request couldn't be sent
func (c *AsyncPFCPClient) ModifySessionAsync(
	sessionID int,
	peerSEID uint64,
	pdrs []*ieLib.IE,
	fars []*ieLib.IE,
	qers []*ieLib.IE,
	urrs []*ieLib.IE,
	timeout time.Duration,
) (*SessionOperation, error) {
	// Check association status
	if !c.isAssociationActive {
		logger.PfcpsimLog.Errorln("Cannot modify session: association is not active")
		return nil, NewAssociationInactiveError()
	}

	// Build session modification request
	modifyReq := message.NewSessionModificationRequest(
		0, // MP flag
		0, // S flag
		peerSEID,
		0, // Sequence number (will be set by SendAsync)
		0, // Priority
	)

	// Add update rules
	if pdrs != nil {
		modifyReq.UpdatePDR = append(modifyReq.UpdatePDR, pdrs...)
	}
	if fars != nil {
		modifyReq.UpdateFAR = append(modifyReq.UpdateFAR, fars...)
	}
	if qers != nil {
		modifyReq.UpdateQER = append(modifyReq.UpdateQER, qers...)
	}
	if urrs != nil {
		modifyReq.UpdateURR = append(modifyReq.UpdateURR, urrs...)
	}

	logger.PfcpsimLog.Infof(
		"Sending async session modification for session %d (peer SEID: %d, PDRs: %d, FARs: %d, QERs: %d, URRs: %d)",
		sessionID, peerSEID, len(modifyReq.UpdatePDR), len(modifyReq.UpdateFAR),
		len(modifyReq.UpdateQER), len(modifyReq.UpdateURR),
	)

	// Send async
	resultCh, err := c.SendAsync(modifyReq, timeout)
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to send session modification request: %v", err)
		return nil, err
	}

	// Create operation handle
	op := &SessionOperation{
		SessionID:      sessionID,
		Type:           OpModify,
		ResultCh:       resultCh,
		StartTime:      time.Now(),
		LocalSEID:      0, // Not used for modify
		SequenceNumber: modifyReq.Sequence(),
	}

	logger.PfcpsimLog.Debugf(
		"Session modification request sent for session %d (seq: %d)",
		sessionID, op.SequenceNumber,
	)

	return op, nil
}

// DeleteSessionAsync sends a session deletion request without blocking.
//
// Parameters:
//   - sessionID: Application-level session identifier
//   - localSEID: Local Session Endpoint ID
//   - remoteSEID: Remote peer's Session Endpoint ID
//   - timeout: How long to wait for response (0 = use default)
//
// Returns:
//   - *SessionOperation: Operation handle to retrieve result
//   - error: Immediate error if request couldn't be sent
func (c *AsyncPFCPClient) DeleteSessionAsync(
	sessionID int,
	localSEID uint64,
	remoteSEID uint64,
	timeout time.Duration,
) (*SessionOperation, error) {
	// Build session deletion request
	delReq := message.NewSessionDeletionRequest(
		0, // MP flag
		0, // S flag
		remoteSEID,
		0, // Sequence number (will be set by SendAsync)
		0, // Priority
		ieLib.NewFSEID(localSEID, net.ParseIP(c.localAddr), nil),
	)

	logger.PfcpsimLog.Infof(
		"Sending async session deletion for session %d (local SEID: %d, remote SEID: %d)",
		sessionID, localSEID, remoteSEID,
	)

	// Send async
	resultCh, err := c.SendAsync(delReq, timeout)
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to send session deletion request: %v", err)
		return nil, err
	}

	// Create operation handle
	op := &SessionOperation{
		SessionID:      sessionID,
		Type:           OpDelete,
		ResultCh:       resultCh,
		StartTime:      time.Now(),
		LocalSEID:      localSEID,
		SequenceNumber: delReq.Sequence(),
	}

	logger.PfcpsimLog.Debugf(
		"Session deletion request sent for session %d (seq: %d)",
		sessionID, op.SequenceNumber,
	)

	return op, nil
}

// WaitForResult waits for the operation to complete and returns the raw result.
// This is a convenience method that blocks on the result channel.
//
// Returns:
//   - ResponseResult: The response or error
func (op *SessionOperation) WaitForResult() ResponseResult {
	return <-op.ResultCh
}

// WaitForResultWithTimeout waits for the operation with an additional timeout.
// This allows setting a different timeout than the request timeout.
//
// Returns:
//   - ResponseResult: The response or error
//   - error: Timeout error if additional timeout expires
func (op *SessionOperation) WaitForResultWithTimeout(timeout time.Duration) (ResponseResult, error) {
	select {
	case result := <-op.ResultCh:
		return result, nil
	case <-time.After(timeout):
		return ResponseResult{}, NewTimeoutExpiredError()
	}
}

// IsComplete checks if the operation has completed (non-blocking).
// Returns true if a result is available.
func (op *SessionOperation) IsComplete() bool {
	select {
	case <-op.ResultCh:
		return true
	default:
		return false
	}
}

// GetElapsedTime returns how long the operation has been running.
func (op *SessionOperation) GetElapsedTime() time.Duration {
	return time.Since(op.StartTime)
}

// ProcessSessionResult processes the async result and returns session information.
// This method handles the response validation and extracts relevant data.
//
// For CREATE operations:
//   - Validates the response and extracts remote SEID
//   - Returns a new PFCPSession object
//
// For MODIFY operations:
//   - Validates the response
//   - Returns nil session (modify doesn't create new session)
//
// For DELETE operations:
//   - Validates the response
//   - Returns nil session (delete removes session)
//
// Parameters:
//   - op: The session operation to process
//
// Returns:
//   - *PFCPSession: New session object (only for CREATE), nil otherwise
//   - error: Any error that occurred
func (c *AsyncPFCPClient) ProcessSessionResult(op *SessionOperation) (*PFCPSession, error) {
	// Wait for result
	result := <-op.ResultCh

	// Check for errors (timeout, send failure, etc.)
	if result.Error != nil {
		logger.PfcpsimLog.Errorf(
			"Session operation %s for session %d failed: %v (latency: %v)",
			op.Type, op.SessionID, result.Error, result.Latency,
		)
		return nil, result.Error
	}

	logger.PfcpsimLog.Debugf(
		"Session operation %s for session %d completed in %v",
		op.Type, op.SessionID, result.Latency,
	)

	// Process based on operation type
	switch op.Type {
	case OpCreate:
		return c.processCreateResult(op, result)
	case OpModify:
		return c.processModifyResult(op, result)
	case OpDelete:
		return c.processDeleteResult(op, result)
	default:
		return nil, fmt.Errorf("unknown operation type: %v", op.Type)
	}
}

// processCreateResult processes a session establishment response
func (c *AsyncPFCPClient) processCreateResult(
	op *SessionOperation,
	result ResponseResult,
) (*PFCPSession, error) {
	// Type assert to establishment response
	estResp, ok := result.Message.(*message.SessionEstablishmentResponse)
	if !ok {
		err := fmt.Errorf(
			"unexpected response type for session establishment: got %T, expected SessionEstablishmentResponse",
			result.Message,
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidResponseError(err)
	}

	// Check cause
	cause, err := estResp.Cause.Cause()
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to extract cause from establishment response: %v", err)
		return nil, NewInvalidCauseError(err)
	}

	if cause != ieLib.CauseRequestAccepted {
		err := fmt.Errorf(
			"session establishment rejected with cause: %d (%s)",
			cause, getCauseName(cause),
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidCauseError(err)
	}

	// Extract remote F-SEID
	remoteFSEID, err := estResp.UPFSEID.FSEID()
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to extract UPF F-SEID: %v", err)
		return nil, fmt.Errorf("failed to extract UPF F-SEID: %w", err)
	}

	// Create session object
	sess := &PFCPSession{
		localSEID: op.LocalSEID,
		peerSEID:  remoteFSEID.SEID,
	}

	logger.PfcpsimLog.Infof(
		"Session %d established successfully (local SEID: %d, remote SEID: %d, latency: %v)",
		op.SessionID, sess.localSEID, sess.peerSEID, result.Latency,
	)

	return sess, nil
}

// processModifyResult processes a session modification response
func (c *AsyncPFCPClient) processModifyResult(
	op *SessionOperation,
	result ResponseResult,
) (*PFCPSession, error) {
	// Type assert to modification response
	modResp, ok := result.Message.(*message.SessionModificationResponse)
	if !ok {
		err := fmt.Errorf(
			"unexpected response type for session modification: got %T, expected SessionModificationResponse",
			result.Message,
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidResponseError(err)
	}

	// Check cause
	cause, err := modResp.Cause.Cause()
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to extract cause from modification response: %v", err)
		return nil, NewInvalidCauseError(err)
	}

	if cause != ieLib.CauseRequestAccepted {
		err := fmt.Errorf(
			"session modification rejected with cause: %d (%s)",
			cause, getCauseName(cause),
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidCauseError(err)
	}

	logger.PfcpsimLog.Infof(
		"Session %d modified successfully (latency: %v)",
		op.SessionID, result.Latency,
	)

	// Modification doesn't return a new session
	return nil, nil
}

// processDeleteResult processes a session deletion response
func (c *AsyncPFCPClient) processDeleteResult(
	op *SessionOperation,
	result ResponseResult,
) (*PFCPSession, error) {
	// Type assert to deletion response
	delResp, ok := result.Message.(*message.SessionDeletionResponse)
	if !ok {
		err := fmt.Errorf(
			"unexpected response type for session deletion: got %T, expected SessionDeletionResponse",
			result.Message,
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidResponseError(err)
	}

	// Check cause
	cause, err := delResp.Cause.Cause()
	if err != nil {
		logger.PfcpsimLog.Errorf("Failed to extract cause from deletion response: %v", err)
		return nil, NewInvalidCauseError(err)
	}

	if cause != ieLib.CauseRequestAccepted {
		err := fmt.Errorf(
			"session deletion rejected with cause: %d (%s)",
			cause, getCauseName(cause),
		)
		logger.PfcpsimLog.Error(err)
		return nil, NewInvalidCauseError(err)
	}

	logger.PfcpsimLog.Infof(
		"Session %d deleted successfully (latency: %v)",
		op.SessionID, result.Latency,
	)

	// Deletion doesn't return a session
	return nil, nil
}

// getCauseName returns a human-readable name for a PFCP cause code
func getCauseName(cause uint8) string {
	// Map common cause codes to names
	causeNames := map[uint8]string{
		ieLib.CauseRequestAccepted:                    "Request accepted",
		ieLib.CauseRequestRejected:                    "Request rejected",
		ieLib.CauseSessionContextNotFound:             "Session context not found",
		ieLib.CauseMandatoryIEMissing:                 "Mandatory IE missing",
		ieLib.CauseConditionalIEMissing:               "Conditional IE missing",
		ieLib.CauseInvalidLength:                      "Invalid length",
		ieLib.CauseMandatoryIEIncorrect:               "Mandatory IE incorrect",
		ieLib.CauseInvalidForwardingPolicy:            "Invalid forwarding policy",
		ieLib.CauseInvalidFTEIDAllocationOption:       "Invalid F-TEID allocation option",
		ieLib.CauseNoEstablishedPFCPAssociation:       "No established PFCP association",
		ieLib.CauseRuleCreationModificationFailure:    "Rule creation/modification failure",
		ieLib.CausePFCPEntityInCongestion:             "PFCP entity in congestion",
		ieLib.CauseNoResourcesAvailable:               "No resources available",
		ieLib.CauseServiceNotSupported:                "Service not supported",
		ieLib.CauseSystemFailure:                      "System failure",
	}

	if name, exists := causeNames[cause]; exists {
		return name
	}
	return fmt.Sprintf("Unknown cause (%d)", cause)
}

// BatchProcessSessionResults processes multiple session operations concurrently.
// This is useful when you have many operations in flight and want to collect all results.
//
// Parameters:
//   - ops: Slice of session operations to process
//
// Returns:
//   - []*PFCPSession: Slice of sessions (nil for modify/delete operations)
//   - []error: Slice of errors (nil for successful operations)
//
// The returned slices have the same length and order as the input operations.
func (c *AsyncPFCPClient) BatchProcessSessionResults(ops []*SessionOperation) ([]*PFCPSession, []error) {
	sessions := make([]*PFCPSession, len(ops))
	errors := make([]error, len(ops))

	for i, op := range ops {
		sessions[i], errors[i] = c.ProcessSessionResult(op)
	}

	return sessions, errors
}