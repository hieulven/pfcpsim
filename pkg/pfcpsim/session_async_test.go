// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"context"
	"net"
	"testing"
	"time"

	ieLib "github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

func TestSessionOpTypeString(t *testing.T) {
	tests := []struct {
		opType   SessionOpType
		expected string
	}{
		{OpCreate, "CREATE"},
		{OpModify, "MODIFY"},
		{OpDelete, "DELETE"},
		{SessionOpType(999), "UNKNOWN(999)"},
	}

	for _, tt := range tests {
		if got := tt.opType.String(); got != tt.expected {
			t.Errorf("SessionOpType.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestSessionOperationMethods(t *testing.T) {
	resultCh := make(chan ResponseResult, 1)
	startTime := time.Now()

	op := &SessionOperation{
		SessionID:      100,
		Type:           OpCreate,
		ResultCh:       resultCh,
		StartTime:      startTime,
		LocalSEID:      12345,
		SequenceNumber: 1,
	}

	// Test IsComplete (should be false)
	if op.IsComplete() {
		t.Error("Expected IsComplete() to be false before result available")
	}

	// Test GetElapsedTime
	time.Sleep(10 * time.Millisecond)
	elapsed := op.GetElapsedTime()
	if elapsed < 10*time.Millisecond {
		t.Errorf("Expected elapsed time >= 10ms, got %v", elapsed)
	}

	// Send result
	resultCh <- ResponseResult{
		Message: nil,
		Error:   nil,
		Latency: 50 * time.Millisecond,
	}

	// Test WaitForResult
	result := op.WaitForResult()
	if result.Latency != 50*time.Millisecond {
		t.Errorf("Expected latency 50ms, got %v", result.Latency)
	}
}

func TestSessionOperationWaitForResultWithTimeout(t *testing.T) {
	resultCh := make(chan ResponseResult, 1)

	op := &SessionOperation{
		SessionID: 200,
		Type:      OpCreate,
		ResultCh:  resultCh,
		StartTime: time.Now(),
	}

	// Test timeout
	_, err := op.WaitForResultWithTimeout(50 * time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}

	// Test successful wait
	resultCh <- ResponseResult{Latency: 10 * time.Millisecond}
	op2 := &SessionOperation{
		SessionID: 201,
		Type:      OpCreate,
		ResultCh:  resultCh,
		StartTime: time.Now(),
	}

	result, err := op2.WaitForResultWithTimeout(100 * time.Millisecond)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Latency != 10*time.Millisecond {
		t.Errorf("Expected latency 10ms, got %v", result.Latency)
	}
}

func TestEstablishSessionAsyncNotAssociated(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")
	client.isAssociationActive = false

	op, err := client.EstablishSessionAsync(1, nil, nil, nil, nil, 1*time.Second)
	if err == nil {
		t.Error("Expected error when association not active")
	}
	if op != nil {
		t.Error("Expected nil operation when error occurs")
	}
}

func TestModifySessionAsyncNotAssociated(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")
	client.isAssociationActive = false

	op, err := client.ModifySessionAsync(1, 100, nil, nil, nil, nil, 1*time.Second)
	if err == nil {
		t.Error("Expected error when association not active")
	}
	if op != nil {
		t.Error("Expected nil operation when error occurs")
	}
}

func TestProcessCreateResult(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Test successful create
	t.Run("Success", func(t *testing.T) {
		estResp := message.NewSessionEstablishmentResponse(
			0, 0, 0, 123, 0,
			ieLib.NewNodeID("192.168.1.1", "", ""),
			ieLib.NewCause(ieLib.CauseRequestAccepted),
			ieLib.NewFSEID(999, net.ParseIP("192.168.1.1"), nil),
		)

		result := ResponseResult{
			Message: estResp,
			Latency: 50 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpCreate,
			LocalSEID: 123,
		}

		session, err := client.processCreateResult(op, result)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if session == nil {
			t.Fatal("Expected session, got nil")
		}
		if session.GetLocalSEID() != 123 {
			t.Errorf("Expected local SEID 123, got %d", session.GetLocalSEID())
		}
		if session.GetPeerSEID() != 999 {
			t.Errorf("Expected peer SEID 999, got %d", session.GetPeerSEID())
		}
	})

	// Test rejected cause
	t.Run("Rejected", func(t *testing.T) {
		estResp := message.NewSessionEstablishmentResponse(
0, 0, 0, 123, 0,
			ieLib.NewNodeID("192.168.1.1", "", ""),
			ieLib.NewCause(ieLib.CauseRequestRejected),
			ieLib.NewFSEID(999, net.ParseIP("192.168.1.1"), nil),
		)

		result := ResponseResult{
			Message: estResp,
			Latency: 50 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpCreate,
			LocalSEID: 123,
		}

		session, err := client.processCreateResult(op, result)
		if err == nil {
			t.Error("Expected error for rejected cause")
		}
		if session != nil {
			t.Error("Expected nil session for rejected cause")
		}
	})

	// Test wrong response type
	t.Run("WrongResponseType", func(t *testing.T) {
		// Send a deletion response instead of establishment
		wrongResp := message.NewSessionDeletionResponse(
			0, 0, 0, 123, 0,
			ieLib.NewCause(ieLib.CauseRequestAccepted),
		)

		result := ResponseResult{
			Message: wrongResp,
			Latency: 50 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpCreate,
			LocalSEID: 123,
		}

		session, err := client.processCreateResult(op, result)
		if err == nil {
			t.Error("Expected error for wrong response type")
		}
		if session != nil {
			t.Error("Expected nil session for wrong response type")
		}
	})
}

func TestProcessModifyResult(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Test successful modify
	t.Run("Success", func(t *testing.T) {
		modResp := message.NewSessionModificationResponse(
			0, 0, 0, 123, 0,
			ieLib.NewCause(ieLib.CauseRequestAccepted),
		)

		result := ResponseResult{
			Message: modResp,
			Latency: 30 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpModify,
		}

		session, err := client.processModifyResult(op, result)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if session != nil {
			t.Error("Expected nil session for modify operation")
		}
	})

	// Test rejected modify
	t.Run("Rejected", func(t *testing.T) {
		modResp := message.NewSessionModificationResponse(
			0, 0, 0, 123, 0,
			ieLib.NewCause(ieLib.CauseSessionContextNotFound),
		)

		result := ResponseResult{
			Message: modResp,
			Latency: 30 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpModify,
		}

		session, err := client.processModifyResult(op, result)
		if err == nil {
			t.Error("Expected error for rejected modify")
		}
		if session != nil {
			t.Error("Expected nil session for rejected modify")
		}
	})
}

func TestProcessDeleteResult(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Test successful delete
	t.Run("Success", func(t *testing.T) {
		delResp := message.NewSessionDeletionResponse(
			0, 0, 0, 123, 0,
			ieLib.NewCause(ieLib.CauseRequestAccepted),
		)

		result := ResponseResult{
			Message: delResp,
			Latency: 20 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpDelete,
		}

		session, err := client.processDeleteResult(op, result)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if session != nil {
			t.Error("Expected nil session for delete operation")
		}
	})

	// Test rejected delete
	t.Run("Rejected", func(t *testing.T) {
		delResp := message.NewSessionDeletionResponse(
			0, 0, 0, 123, 0,
			ieLib.NewCause(ieLib.CauseSessionContextNotFound),
		)

		result := ResponseResult{
			Message: delResp,
			Latency: 20 * time.Millisecond,
		}

		op := &SessionOperation{
			SessionID: 1,
			Type:      OpDelete,
		}

		session, err := client.processDeleteResult(op, result)
		if err == nil {
			t.Error("Expected error for rejected delete")
		}
		if session != nil {
			t.Error("Expected nil session for rejected delete")
		}
	})
}

func TestProcessSessionResultWithTimeout(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create operation with timeout error
	resultCh := make(chan ResponseResult, 1)
	resultCh <- ResponseResult{
		Error:   NewTimeoutExpiredError(),
		Latency: 5 * time.Second,
	}

	op := &SessionOperation{
		SessionID: 1,
		Type:      OpCreate,
		ResultCh:  resultCh,
		LocalSEID: 123,
	}

	session, err := client.ProcessSessionResult(op)
	if err == nil {
		t.Error("Expected error for timeout")
	}
	if session != nil {
		t.Error("Expected nil session for timeout error")
	}
}

func TestBatchProcessSessionResults(t *testing.T) {
	client := NewAsyncPFCPClient("127.0.0.1")

	// Create multiple operations
	ops := make([]*SessionOperation, 3)

	// Operation 1: Successful create
	resultCh1 := make(chan ResponseResult, 1)
	resultCh1 <- ResponseResult{
		Message: message.NewSessionEstablishmentResponse(
			0, 0, 0, 1, 0,
			ieLib.NewNodeID("192.168.1.1", "", ""),
			ieLib.NewCause(ieLib.CauseRequestAccepted),
			ieLib.NewFSEID(101, net.ParseIP("192.168.1.1"), nil),
		),
		Latency: 50 * time.Millisecond,
	}
	ops[0] = &SessionOperation{
		SessionID: 1,
		Type:      OpCreate,
		ResultCh:  resultCh1,
		LocalSEID: 1,
	}

	// Operation 2: Successful modify
	resultCh2 := make(chan ResponseResult, 1)
	resultCh2 <- ResponseResult{
		Message: message.NewSessionModificationResponse(
			0, 0, 0, 2, 0,
			ieLib.NewCause(ieLib.CauseRequestAccepted),
		),
		Latency: 30 * time.Millisecond,
	}
	ops[1] = &SessionOperation{
		SessionID: 2,
		Type:      OpModify,
		ResultCh:  resultCh2,
	}

	// Operation 3: Failed create (timeout)
	resultCh3 := make(chan ResponseResult, 1)
	resultCh3 <- ResponseResult{
		Error:   NewTimeoutExpiredError(),
		Latency: 5 * time.Second,
	}
	ops[2] = &SessionOperation{
		SessionID: 3,
		Type:      OpCreate,
		ResultCh:  resultCh3,
		LocalSEID: 3,
	}

	// Batch process
	sessions, errors := client.BatchProcessSessionResults(ops)

	// Verify results
	if len(sessions) != 3 || len(errors) != 3 {
		t.Fatalf("Expected 3 results, got sessions: %d, errors: %d", len(sessions), len(errors))
	}

	// Check operation 1 (successful create)
	if errors[0] != nil {
		t.Errorf("Operation 1: unexpected error: %v", errors[0])
	}
	if sessions[0] == nil {
		t.Error("Operation 1: expected session, got nil")
	} else if sessions[0].GetPeerSEID() != 101 {
		t.Errorf("Operation 1: expected peer SEID 101, got %d", sessions[0].GetPeerSEID())
	}

	// Check operation 2 (successful modify)
	if errors[1] != nil {
		t.Errorf("Operation 2: unexpected error: %v", errors[1])
	}
	if sessions[1] != nil {
		t.Error("Operation 2: expected nil session for modify, got non-nil")
	}

	// Check operation 3 (failed create)
	if errors[2] == nil {
		t.Error("Operation 3: expected error, got nil")
	}
	if sessions[2] != nil {
		t.Error("Operation 3: expected nil session for error, got non-nil")
	}
}

func TestGetCauseName(t *testing.T) {
	tests := []struct {
		cause    uint8
		expected string
	}{
		{ieLib.CauseRequestAccepted, "Request accepted"},
		{ieLib.CauseRequestRejected, "Request rejected"},
		{ieLib.CauseSessionContextNotFound, "Session context not found"},
		{ieLib.CauseMandatoryIEMissing, "Mandatory IE missing"},
		{ieLib.CauseSystemFailure, "System failure"},
		{255, "Unknown cause (255)"},
	}

	for _, tt := range tests {
		got := getCauseName(tt.cause)
		if got != tt.expected {
			t.Errorf("getCauseName(%d) = %v, want %v", tt.cause, got, tt.expected)
		}
	}
}