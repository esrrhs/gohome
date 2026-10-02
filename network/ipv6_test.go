package network

import (
	"net"
	"strings"
	"testing"
	"time"
)

// skipIfNoIPv6Loopback skips the test when the host has no IPv6 loopback.
func skipIfNoIPv6Loopback(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	_ = l.Close()
}

// ipv6ListenAddr extracts the concrete listen address of a Conn returned by
// Listen("[::1]:0") so the dialer targets the ephemeral port.
func ipv6ListenAddr(t *testing.T, ln Conn) string {
	t.Helper()
	var addr string
	switch c := ln.(type) {
	case *TcpConn:
		addr = c.listener.Addr().String()
	case *UdpConn:
		addr = c.listener.listenerconn.LocalAddr().String()
	case *RudpConn:
		addr = c.listener.listenerconn.LocalAddr().String()
	case *KcpConn:
		addr = c.listener.Addr().String()
	case *QuicConn:
		addr = c.listener.Addr().String()
	default:
		t.Fatalf("unsupported listener type %T", ln)
	}
	if !strings.HasPrefix(addr, "[::1]") {
		t.Fatalf("listener %s is not on IPv6 loopback", addr)
	}
	return addr
}

// runIPv6Loopback boots a listener on [::1]:0, accepts one connection and
// echoes client bytes back, then dials as a client and verifies the round
// trip. It works for both datagram (udp) and stream/reliable underlays.
func runIPv6Loopback(t *testing.T, proto string) {
	t.Helper()
	skipIfNoIPv6Loopback(t)

	c, err := NewConn(proto)
	if err != nil {
		t.Fatalf("NewConn(%s): %v", proto, err)
	}
	ln, err := c.Listen("[::1]:0")
	if err != nil {
		t.Fatalf("Listen(%s [::1]): %v", proto, err)
	}
	defer ln.Close()

	addr := ipv6ListenAddr(t, ln)

	srvReady := make(chan Conn, 1)
	errCh := make(chan error, 2)
	go func() {
		srv, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		srvReady <- srv
		// Echo every received chunk until the connection closes. A single
		// datagram underlay (udp) does exactly one read/write round.
		buf := make([]byte, 128)
		for {
			n, err := srv.Read(buf)
			if err != nil {
				return
			}
			if _, err := srv.Write(buf[:n]); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Give the listener a moment to start accepting before dialing.
	time.Sleep(100 * time.Millisecond)

	cli, err := c.Dial(addr)
	if err != nil {
		t.Fatalf("Dial(%s %s): %v", proto, addr, err)
	}
	defer cli.Close()

	msg := []byte("gohome ipv6 loopback " + proto)
	// Write before waiting for Accept: datagram/session underlays (udp, kcp,
	// quic) only surface an accepted connection once the first client packet
	// or stream data arrives, mirroring real usage.
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var srv Conn
	select {
	case srv = <-srvReady:
		defer srv.Close()
	case err := <-errCh:
		t.Fatalf("Accept: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatal("Accept timed out")
	}

	got := make([]byte, 0, len(msg))
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 128)
		for len(got) < len(msg) {
			n, err := cli.Read(buf)
			if err != nil {
				readDone <- err
				return
			}
			got = append(got, buf[:n]...)
		}
		readDone <- nil
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("client Read: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("client Read timed out")
	}
	if string(got) != string(msg) {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}
}

func TestIPv6_TCP_Loopback(t *testing.T) {
	runIPv6Loopback(t, "tcp")
}

func TestIPv6_UDP_Loopback(t *testing.T) {
	runIPv6Loopback(t, "udp")
}

func TestIPv6_RUDP_Loopback(t *testing.T) {
	// On Linux this exercises the rudp sendmmsg batch path with an
	// AF_INET6 destination sockaddr; other platforms use plain WriteToUDP.
	runIPv6Loopback(t, "rudp")
}

func TestIPv6_KCP_Loopback(t *testing.T) {
	runIPv6Loopback(t, "kcp")
}

func TestIPv6_QUIC_Loopback(t *testing.T) {
	runIPv6Loopback(t, "quic")
}
