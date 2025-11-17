// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package stress

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/omec-project/pfcpsim/logger"
	"github.com/omec-project/pfcpsim/pkg/pfcpsim"
	"github.com/omec-project/pfcpsim/pkg/pfcpsim/session"
	ieLib "github.com/wmnsk/go-pfcp/ie"
)

// Worker processes session tasks from a work queue.
// Each worker maintains its own set of active sessions and communicates
// results back via metrics.
type Worker struct {
	// id uniquely identifies this worker
	id int

	// client is the async PFCP client for this worker
	client *pfcpsim.AsyncPFCPClient

	// config contains the stress test configuration
	config *StressConfig

	// workQueue is the channel to receive tasks from
	workQueue <-chan *SessionTask

	// metrics collects performance data
	metrics *Metrics

	// activeSessions tracks sessions this worker has created
	// Key: sessionID, Value: session info
	activeSessions map[int]*SessionInfo
	sessionMutex   sync.Mutex

	// lastUEAddr tracks the last UE address allocated
	lastUEAddr net.IP
	ueAddrLock sync.Mutex
}

// SessionInfo holds information about an active session
type SessionInfo struct {
	// LocalSEID is the local session endpoint ID
	LocalSEID uint64

	// Session is the PFCP session object
	Session *pfcpsim.PFCPSession

	// CreatedAt records when the session was created
	CreatedAt time.Time

	// ModifyCount tracks how many modifications have been performed
	ModifyCount int

	// UEAddress is the UE IP address for this session
	UEAddress string

	// BaseID is the base ID used for PDR/FAR/QER generation
	BaseID int
}

// SessionTask represents a unit of work for a worker
type SessionTask struct {
	// Type indicates what operation to perform
	Type TaskType

	// SessionID is the application-level session identifier
	SessionID int

	// Iteration is used for modify operations (which modification is this)
	Iteration int

	// Metadata can hold additional task-specific data
	Metadata map[string]interface{}
}

// TaskType identifies the type of session task
type TaskType int

const (
	// TaskCreate instructs the worker to create a session
	TaskCreate TaskType = iota

	// TaskModify instructs the worker to modify a session
	TaskModify

	// TaskDelete instructs the worker to delete a session
	TaskDelete
)

// String returns the string representation of TaskType
func (t TaskType) String() string {
	switch t {
	case TaskCreate:
		return "CREATE"
	case TaskModify:
		return "MODIFY"
	case TaskDelete:
		return "DELETE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// NewWorker creates a new worker instance
func NewWorker(
	id int,
	client *pfcpsim.AsyncPFCPClient,
	config *StressConfig,
	workQueue <-chan *SessionTask,
	metrics *Metrics,
) *Worker {
	// Parse UE pool for this worker
	_, ipNet, err := net.ParseCIDR(config.UEPoolCIDR)
	if err != nil {
		logger.PfcpsimLog.Fatalf("Failed to parse UE pool CIDR: %v", err)
	}

	return &Worker{
		id:             id,
		client:         client,
		config:         config,
		workQueue:      workQueue,
		metrics:        metrics,
		activeSessions: make(map[int]*SessionInfo),
		lastUEAddr:     ipNet.IP,
	}
}

// Start begins processing tasks from the work queue.
// This method blocks until the context is cancelled or the work queue is closed.
func (w *Worker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	logger.PfcpsimLog.Infof("Worker %d started", w.id)

	for {
		select {
		case <-ctx.Done():
			logger.PfcpsimLog.Infof("Worker %d stopping due to context cancellation", w.id)
			return

		case task, ok := <-w.workQueue:
			if !ok {
				// Work queue closed, worker should exit
				logger.PfcpsimLog.Infof("Worker %d stopping due to closed work queue", w.id)
				return
			}

			// Process the task
			w.processTask(task)
		}
	}
}

// processTask dispatches the task to the appropriate handler
func (w *Worker) processTask(task *SessionTask) {
	logger.PfcpsimLog.Debugf(
		"Worker %d processing task: %s for session %d",
		w.id, task.Type, task.SessionID,
	)

	switch task.Type {
	case TaskCreate:
		w.createSession(task.SessionID)
	case TaskModify:
		w.modifySession(task.SessionID, task.Iteration)
	case TaskDelete:
		w.deleteSession(task.SessionID)
	default:
		logger.PfcpsimLog.Errorf(
			"Worker %d received unknown task type: %v",
			w.id, task.Type,
		)
	}
}

// GetActiveSessionCount returns the number of active sessions for this worker
func (w *Worker) GetActiveSessionCount() int {
	w.sessionMutex.Lock()
	defer w.sessionMutex.Unlock()
	return len(w.activeSessions)
}

// GetSessionInfo retrieves session info for a given session ID
func (w *Worker) GetSessionInfo(sessionID int) (*SessionInfo, bool) {
	w.sessionMutex.Lock()
	defer w.sessionMutex.Unlock()
	info, exists := w.activeSessions[sessionID]
	return info, exists
}

// nextUEAddress generates the next UE IP address
func (w *Worker) nextUEAddress() net.IP {
	w.ueAddrLock.Lock()
	defer w.ueAddrLock.Unlock()

	// Increment IP address
	next := make(net.IP, len(w.lastUEAddr))
	copy(next, w.lastUEAddr)

	overflow := true
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			overflow = false
			break
		}
	}

    if overflow {
        logger.PfcpsimLog.Warnln("UE address pool exhausted, wrapping around")
        // Reset to start of pool or handle as needed
    }

	w.lastUEAddr = next
	return next
}

// createSession creates a new PFCP session
func (w *Worker) createSession(sessionID int) {
	start := time.Now()

	logger.PfcpsimLog.Debugf("Worker %d creating session %d", w.id, sessionID)

	// Build session IEs
	pdrs, fars, qers, urrs := w.buildSessionIEs(sessionID)

	// Send async session establishment
	op, err := w.client.EstablishSessionAsync(
		sessionID,
		pdrs, fars, qers, urrs,
		w.config.RequestTimeout,
	)

	if err != nil {
		w.metrics.RecordCreate(time.Since(start), false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Failed to send create request for session %d: %v",
			w.id, sessionID, err,
		)
		return
	}

	// Process result
	sess, err := w.client.ProcessSessionResult(op)

	latency := time.Since(start)

	if err != nil {
		w.metrics.RecordCreate(latency, false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Session %d creation failed after %v: %v",
			w.id, sessionID, latency, err,
		)
		return
	}

	// Calculate UE address for this session
	ueAddr := w.nextUEAddress()

	// Store session info
	w.sessionMutex.Lock()
	w.activeSessions[sessionID] = &SessionInfo{
		LocalSEID:   op.LocalSEID,
		Session:     sess,
		CreatedAt:   start,
		ModifyCount: 0,
		UEAddress:   ueAddr.String(),
		BaseID:      w.calculateBaseID(sessionID),
	}
	w.sessionMutex.Unlock()

	w.metrics.RecordCreate(latency, true)

	logger.PfcpsimLog.Infof(
		"Worker %d: Session %d created (local SEID: %d, remote SEID: %d, latency: %v)",
		w.id, sessionID, sess.GetLocalSEID(), sess.GetPeerSEID(), latency,
	)
}

// modifySession modifies an existing PFCP session
func (w *Worker) modifySession(sessionID int, iteration int) {
	start := time.Now()

	logger.PfcpsimLog.Debugf(
		"Worker %d modifying session %d (iteration %d)",
		w.id, sessionID, iteration,
	)

	// Get session info
	w.sessionMutex.Lock()
	sessInfo, exists := w.activeSessions[sessionID]
	w.sessionMutex.Unlock()

	if !exists {
		w.metrics.RecordModify(time.Since(start), false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Session %d not found for modification",
			w.id, sessionID,
		)
		return
	}

	// Build modify IEs
	pdrs, fars, qers, urrs := w.buildModifyIEs(sessionID, iteration, sessInfo)

	// Send async session modification
	op, err := w.client.ModifySessionAsync(
		sessionID,
		sessInfo.Session.GetPeerSEID(),
		pdrs, fars, qers, urrs,
		w.config.RequestTimeout,
	)

	if err != nil {
		w.metrics.RecordModify(time.Since(start), false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Failed to send modify request for session %d: %v",
			w.id, sessionID, err,
		)
		return
	}

	// Process result
	_, err = w.client.ProcessSessionResult(op)

	latency := time.Since(start)

	if err != nil {
		w.metrics.RecordModify(latency, false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Session %d modification failed after %v: %v",
			w.id, sessionID, latency, err,
		)
		return
	}

	// Update modify count
	w.sessionMutex.Lock()
	sessInfo.ModifyCount++
	w.sessionMutex.Unlock()

	w.metrics.RecordModify(latency, true)

	logger.PfcpsimLog.Infof(
		"Worker %d: Session %d modified (iteration %d, latency: %v)",
		w.id, sessionID, iteration, latency,
	)
}

// deleteSession deletes an existing PFCP session
func (w *Worker) deleteSession(sessionID int) {
	start := time.Now()

	logger.PfcpsimLog.Debugf("Worker %d deleting session %d", w.id, sessionID)

	// Get and remove session info
	w.sessionMutex.Lock()
	sessInfo, exists := w.activeSessions[sessionID]
	if exists {
		delete(w.activeSessions, sessionID)
	}
	w.sessionMutex.Unlock()

	if !exists {
		w.metrics.RecordDelete(time.Since(start), false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Session %d not found for deletion",
			w.id, sessionID,
		)
		return
	}

	// Send async session deletion
	op, err := w.client.DeleteSessionAsync(
		sessionID,
		sessInfo.LocalSEID,
		sessInfo.Session.GetPeerSEID(),
		w.config.RequestTimeout,
	)

	if err != nil {
		w.metrics.RecordDelete(time.Since(start), false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Failed to send delete request for session %d: %v",
			w.id, sessionID, err,
		)
		return
	}

	// Process result
	_, err = w.client.ProcessSessionResult(op)

	latency := time.Since(start)

	if err != nil {
		w.metrics.RecordDelete(latency, false)
		logger.PfcpsimLog.Errorf(
			"Worker %d: Session %d deletion failed after %v: %v",
			w.id, sessionID, latency, err,
		)
		return
	}

	w.metrics.RecordDelete(latency, true)

	logger.PfcpsimLog.Infof(
		"Worker %d: Session %d deleted (latency: %v)",
		w.id, sessionID, latency,
	)
}

// calculateBaseID calculates the base ID for PDR/FAR/QER generation
// This ensures unique IDs across sessions
func (w *Worker) calculateBaseID(sessionID int) int {
	// Use sessionID * 10 to give room for multiple rules per session
	return sessionID * 10
}

// buildSessionIEs builds the Information Elements for session creation
func (w *Worker) buildSessionIEs(sessionID int) ([]*ieLib.IE, []*ieLib.IE, []*ieLib.IE, []*ieLib.IE) {
	baseID := w.calculateBaseID(sessionID)
	ueAddr := w.nextUEAddress()

	var pdrs, fars, qers, urrs []*ieLib.IE

	// Session-level QER (QER ID 0)
	sessQerID := uint32(0)
	qers = append(qers, session.NewQERBuilder().
		WithID(sessQerID).
		WithMethod(session.Create).
		WithQFI(w.config.QFI).
		WithUplinkMBR(60000).
		WithDownlinkMBR(60000).
		Build())

	// Create PDRs, FARs, and QERs for each application filter
	ID := uint16(baseID)
	for idx, appFilter := range w.config.AppFilters {
		// Parse application filter
		sdfFilter, gateStatus, precedence, err := w.parseAppFilter(appFilter)
		if err != nil {
			logger.PfcpsimLog.Errorf(
				"Worker %d: Failed to parse app filter '%s': %v",
				w.id, appFilter, err,
			)
			continue
		}

		uplinkPdrID := ID
		downlinkPdrID := ID + 1
		uplinkFarID := uint32(ID)
		downlinkFarID := uint32(ID + 1)
		uplinkAppQerID := uint32(ID)
		downlinkAppQerID := uint32(ID + 1)

		// Uplink PDR
		uplinkPDR := session.NewPDRBuilder().
			WithID(uplinkPdrID).
			WithMethod(session.Create).
			WithTEID(uint32(baseID + idx)).
			WithFARID(uplinkFarID).
			AddQERID(sessQerID).
			AddQERID(uplinkAppQerID).
			WithN3Address(w.config.N3Address).
			WithSDFFilter(sdfFilter).
			WithPrecedence(precedence).
			MarkAsUplink().
			BuildPDR()

		// Downlink PDR
		downlinkPDR := session.NewPDRBuilder().
			WithID(downlinkPdrID).
			WithMethod(session.Create).
			WithPrecedence(precedence).
			WithUEAddress(ueAddr.String()).
			WithSDFFilter(sdfFilter).
			AddQERID(sessQerID).
			AddQERID(downlinkAppQerID).
			WithFARID(downlinkFarID).
			MarkAsDownlink().
			BuildPDR()

		pdrs = append(pdrs, uplinkPDR, downlinkPDR)

		// Uplink FAR (forward to core)
		uplinkFAR := session.NewFARBuilder().
			WithID(uplinkFarID).
			WithAction(session.ActionForward).
			WithDstInterface(ieLib.DstInterfaceCore).
			WithMethod(session.Create).
			BuildFAR()

		// Downlink FAR (initially drop, will be updated in modify)
		downlinkFAR := session.NewFARBuilder().
			WithID(downlinkFarID).
			WithAction(session.ActionDrop).
			WithMethod(session.Create).
			WithDstInterface(ieLib.DstInterfaceAccess).
			WithZeroBasedOuterHeaderCreation().
			BuildFAR()

		fars = append(fars, uplinkFAR, downlinkFAR)

		// Application QERs
		uplinkAppQER := session.NewQERBuilder().
			WithID(uplinkAppQerID).
			WithMethod(session.Create).
			WithQFI(w.config.QFI).
			WithUplinkMBR(50000).
			WithDownlinkMBR(30000).
			WithGateStatus(gateStatus).
			Build()

		downlinkAppQER := session.NewQERBuilder().
			WithID(downlinkAppQerID).
			WithMethod(session.Create).
			WithQFI(w.config.QFI).
			WithUplinkMBR(50000).
			WithDownlinkMBR(30000).
			WithGateStatus(gateStatus).
			Build()

		qers = append(qers, uplinkAppQER, downlinkAppQER)

		// URRs for usage reporting
		urrID := uint32(ID)
		urr := session.NewURRBuilder().
			WithID(urrID).
			WithMethod(session.Create).
			WithMeasurementMethod(0, 1, 0).
			WithMeasurementPeriod(10 * time.Second).
			WithReportingTrigger(session.ReportingTrigger{
				Flags: session.RPT_TRIG_PERIO,
			}).
			Build()

		urrs = append(urrs, urr)

		ID += 2
	}

	return pdrs, fars, qers, urrs
}

// buildModifyIEs builds the Information Elements for session modification
func (w *Worker) buildModifyIEs(
	sessionID int,
	iteration int,
	sessInfo *SessionInfo,
) ([]*ieLib.IE, []*ieLib.IE, []*ieLib.IE, []*ieLib.IE) {
	baseID := sessInfo.BaseID

	var pdrs, fars, qers, urrs []*ieLib.IE

	// Update downlink FARs to forward (instead of drop)
	ID := uint32(baseID + 1) // Start with first downlink FAR

	for range w.config.AppFilters {
		// Downlink FAR - update to forward with GNB address
		downlinkFAR := session.NewFARBuilder().
			WithID(ID).
			WithMethod(session.Update).
			WithAction(session.ActionForward).
			WithDstInterface(ieLib.DstInterfaceAccess).
			WithTEID(uint32(baseID)).
			WithDownlinkIP(w.config.GnbAddress).
			BuildFAR()

		fars = append(fars, downlinkFAR)

		// Update URR (optional, for testing)
		urrID := ID - 1
		urr := session.NewURRBuilder().
			WithID(urrID).
			WithMethod(session.Update).
			WithMeasurementPeriod(time.Duration(10+iteration) * time.Second).
			Build()

		urrs = append(urrs, urr)

		ID += 2
	}

	return pdrs, fars, qers, urrs
}

// parseAppFilter parses an application filter string
// Format: "{ip|udp|tcp}:{IPv4 Prefix|any}:{<lower-port>-<upper-port>|any}:{allow|deny}:{precedence}"
// Example: "udp:10.0.0.0/8:80-88:allow:100"
func (w *Worker) parseAppFilter(filter string) (string, uint8, uint32, error) {
	// For now, use a simple default if empty
	if filter == "" {
		return "", ieLib.GateStatusOpen, 100, nil
	}

	// This is a simplified parser - in production, use the full parser from internal/pfcpsim
	// For stress testing, we can use simple defaults
	return "permit out ip from any to assigned", ieLib.GateStatusOpen, 100, nil
}