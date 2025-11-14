// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"fmt"
	"sync"
)

type PFCPSession struct {
	localSEID uint64
	peerSEID  uint64
		
	// Optional metadata (can be extended)
	mu       sync.RWMutex
	metadata map[string]interface{}
}

// GetLocalSEID returns the local Session Endpoint ID.
// This is the F-SEID assigned by this client.
func (s *PFCPSession) GetLocalSEID() uint64 {
	return s.localSEID
}

// GetPeerSEID returns the peer's Session Endpoint ID.
// This is the F-SEID assigned by the remote UPF/PFCP peer.
func (s *PFCPSession) GetPeerSEID() uint64 {
	return s.peerSEID
}

// SetMetadata stores arbitrary metadata for this session.
// This can be used to associate application-specific data with the session.
//
// Example:
//   session.SetMetadata("ue_ip", "10.0.0.1")
//   session.SetMetadata("created_at", time.Now())
func (s *PFCPSession) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.metadata == nil {
		s.metadata = make(map[string]interface{})
	}
	s.metadata[key] = value
}

// GetMetadata retrieves metadata for this session.
// Returns nil if the key doesn't exist.
func (s *PFCPSession) GetMetadata(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	if s.metadata == nil {
		return nil
	}
	return s.metadata[key]
}

// GetAllMetadata returns a copy of all metadata.
func (s *PFCPSession) GetAllMetadata() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	if s.metadata == nil {
		return make(map[string]interface{})
	}
	
	// Return a copy to prevent external modification
	copy := make(map[string]interface{}, len(s.metadata))
	for k, v := range s.metadata {
		copy[k] = v
	}
	return copy
}

// String returns a string representation of the session
func (s *PFCPSession) String() string {
	return fmt.Sprintf("PFCPSession{LocalSEID: %d, PeerSEID: %d}", s.localSEID, s.peerSEID)
}

// NewPFCPSession creates a new PFCP session with the given SEIDs.
// This is primarily for testing purposes.
func NewPFCPSession(localSEID, peerSEID uint64) *PFCPSession {
	return &PFCPSession{
		localSEID: localSEID,
		peerSEID:  peerSEID,
		metadata:  make(map[string]interface{}),
	}
}