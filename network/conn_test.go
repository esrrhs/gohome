package network

import (
	"testing"
)

func TestNewConn(t *testing.T) {
	validProtos := []string{"tcp", "udp", "rudp", "ricmp", "kcp", "quic", "rhttp"}

	for _, proto := range validProtos {
		conn, err := NewConn(proto)
		if err != nil {
			t.Fatalf("NewConn(%q) returned error: %v", proto, err)
		}
		if conn == nil {
			t.Fatalf("NewConn(%q) returned nil conn", proto)
		}
		if conn.Name() == "" {
			t.Errorf("NewConn(%q).Name() returned empty string", proto)
		}
	}

	// Test case insensitivity
	for _, proto := range []string{"TCP", "Udp", "RUDP", "KCP", "QUIC"} {
		conn, err := NewConn(proto)
		if err != nil {
			t.Fatalf("NewConn(%q) returned error: %v", proto, err)
		}
		if conn == nil {
			t.Fatalf("NewConn(%q) returned nil conn", proto)
		}
	}

	// Test invalid protocol
	conn, err := NewConn("invalid")
	if err == nil {
		t.Fatal("NewConn(\"invalid\") should return error")
	}
	if conn != nil {
		t.Fatal("NewConn(\"invalid\") should return nil conn")
	}
}

func TestSupportReliableProtos(t *testing.T) {
	expected := map[string]bool{
		"tcp":   true,
		"rudp":  true,
		"ricmp": true,
		"kcp":   true,
		"quic":  true,
		"rhttp": true,
	}

	protos := SupportReliableProtos()
	if len(protos) != len(expected) {
		t.Fatalf("SupportReliableProtos() returned %d protos, want %d", len(protos), len(expected))
	}
	for _, p := range protos {
		if !expected[p] {
			t.Errorf("SupportReliableProtos() contains unexpected proto %q", p)
		}
	}
}

func TestSupportProtos(t *testing.T) {
	protos := SupportProtos()

	// Must contain all reliable protos plus "udp"
	reliableProtos := SupportReliableProtos()
	if len(protos) != len(reliableProtos)+1 {
		t.Fatalf("SupportProtos() returned %d protos, want %d", len(protos), len(reliableProtos)+1)
	}

	set := make(map[string]bool)
	for _, p := range protos {
		set[p] = true
	}
	for _, rp := range reliableProtos {
		if !set[rp] {
			t.Errorf("SupportProtos() missing reliable proto %q", rp)
		}
	}
	if !set["udp"] {
		t.Error("SupportProtos() missing \"udp\"")
	}
}

func TestHasReliableProto(t *testing.T) {
	for _, proto := range SupportReliableProtos() {
		if !HasReliableProto(proto) {
			t.Errorf("HasReliableProto(%q) = false, want true", proto)
		}
	}

	if HasReliableProto("udp") {
		t.Error("HasReliableProto(\"udp\") = true, want false")
	}
	if HasReliableProto("invalid") {
		t.Error("HasReliableProto(\"invalid\") = true, want false")
	}
}

func TestHasProto(t *testing.T) {
	for _, proto := range SupportProtos() {
		if !HasProto(proto) {
			t.Errorf("HasProto(%q) = false, want true", proto)
		}
	}

	if HasProto("invalid") {
		t.Error("HasProto(\"invalid\") = true, want false")
	}
	if HasProto("") {
		t.Error("HasProto(\"\") = true, want false")
	}
}
