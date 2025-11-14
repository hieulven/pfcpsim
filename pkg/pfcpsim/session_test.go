// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpsim

import (
	"testing"
	"time"
)

func TestPFCPSessionGetters(t *testing.T) {
	session := NewPFCPSession(12345, 67890)

	if session.GetLocalSEID() != 12345 {
		t.Errorf("Expected local SEID 12345, got %d", session.GetLocalSEID())
	}

	if session.GetPeerSEID() != 67890 {
		t.Errorf("Expected peer SEID 67890, got %d", session.GetPeerSEID())
	}
}

func TestPFCPSessionMetadata(t *testing.T) {
	session := NewPFCPSession(1, 2)

	// Test setting and getting metadata
	session.SetMetadata("ue_ip", "10.0.0.1")
	session.SetMetadata("created_at", time.Now())
	session.SetMetadata("priority", 10)

	// Get existing metadata
	ueIP := session.GetMetadata("ue_ip")
	if ueIP != "10.0.0.1" {
		t.Errorf("Expected ue_ip '10.0.0.1', got %v", ueIP)
	}

	priority := session.GetMetadata("priority")
	if priority != 10 {
		t.Errorf("Expected priority 10, got %v", priority)
	}

	// Get non-existent metadata
	missing := session.GetMetadata("non_existent")
	if missing != nil {
		t.Errorf("Expected nil for non-existent key, got %v", missing)
	}
}

func TestPFCPSessionGetAllMetadata(t *testing.T) {
	session := NewPFCPSession(1, 2)

	// Set some metadata
	session.SetMetadata("key1", "value1")
	session.SetMetadata("key2", 42)
	session.SetMetadata("key3", true)

	// Get all metadata
	allMetadata := session.GetAllMetadata()

	if len(allMetadata) != 3 {
		t.Errorf("Expected 3 metadata entries, got %d", len(allMetadata))
	}

	if allMetadata["key1"] != "value1" {
		t.Error("Metadata key1 not found or incorrect")
	}
	if allMetadata["key2"] != 42 {
		t.Error("Metadata key2 not found or incorrect")
	}
	if allMetadata["key3"] != true {
		t.Error("Metadata key3 not found or incorrect")
	}

	// Verify it's a copy (modifying returned map shouldn't affect session)
	allMetadata["key4"] = "new_value"
	if session.GetMetadata("key4") != nil {
		t.Error("Modifying returned metadata affected session")
	}
}

func TestPFCPSessionMetadataEmpty(t *testing.T) {
	session := NewPFCPSession(1, 2)

	// Get metadata without setting any
	value := session.GetMetadata("any_key")
	if value != nil {
		t.Errorf("Expected nil for empty metadata, got %v", value)
	}

	allMetadata := session.GetAllMetadata()
	if len(allMetadata) != 0 {
		t.Errorf("Expected empty metadata map, got %d entries", len(allMetadata))
	}
}

func TestPFCPSessionString(t *testing.T) {
	session := NewPFCPSession(12345, 67890)
	str := session.String()

	expected := "PFCPSession{LocalSEID: 12345, PeerSEID: 67890}"
	if str != expected {
		t.Errorf("Expected string %q, got %q", expected, str)
	}
}

func TestPFCPSessionConcurrentMetadata(t *testing.T) {
	session := NewPFCPSession(1, 2)

	// Test concurrent metadata access
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			session.SetMetadata("counter", i)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = session.GetMetadata("counter")
		}
		done <- true
	}()

	// GetAll goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = session.GetAllMetadata()
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	// If we get here without race detector complaints, test passed
}

func TestPFCPSessionMetadataOverwrite(t *testing.T) {
	session := NewPFCPSession(1, 2)

	// Set initial value
	session.SetMetadata("key", "initial")
	if session.GetMetadata("key") != "initial" {
		t.Error("Initial value not set correctly")
	}

	// Overwrite with different type
	session.SetMetadata("key", 123)
	if session.GetMetadata("key") != 123 {
		t.Error("Overwrite value not set correctly")
	}

	// Overwrite again
	session.SetMetadata("key", true)
	if session.GetMetadata("key") != true {
		t.Error("Second overwrite value not set correctly")
	}
}