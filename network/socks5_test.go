package network

import (
	"io"
	"net"
	"strings"
	"testing"
)

func TestSock5HandshakeByNoAuth(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)

	// Server side: run Sock5HandshakeBy
	go func() {
		errCh <- Sock5HandshakeBy(server, "", "")
	}()

	// Client side: send SOCKS5 greeting with no-auth method
	_, err := client.Write([]byte{socksVer5, 1, socks5AuthNone})
	if err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	// Read server response
	resp := make([]byte, 2)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("client read failed: %v", err)
	}
	if resp[0] != socksVer5 || resp[1] != NoAuth {
		t.Errorf("unexpected handshake response: %v", resp)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Sock5HandshakeBy returned error: %v", err)
	}
}

func TestSock5HandshakeByUserPassAuth(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	user := "admin"
	pass := "secret"

	errCh := make(chan error, 1)

	go func() {
		errCh <- Sock5HandshakeBy(server, user, pass)
	}()

	// Client sends SOCKS5 greeting with user/pass method
	_, err := client.Write([]byte{socksVer5, 1, socks5UserPassAuth})
	if err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	// Read server's method selection response
	resp := make([]byte, 2)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("client read method selection failed: %v", err)
	}
	if resp[0] != socksVer5 || resp[1] != UserPassAuth {
		t.Fatalf("unexpected method selection: %v", resp)
	}

	// Send user/pass authentication sub-negotiation
	authMsg := []byte{userAuthVersion, byte(len(user))}
	authMsg = append(authMsg, []byte(user)...)
	authMsg = append(authMsg, byte(len(pass)))
	authMsg = append(authMsg, []byte(pass)...)
	if _, err := client.Write(authMsg); err != nil {
		t.Fatalf("client write auth failed: %v", err)
	}

	// Read auth result
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(client, authResp); err != nil {
		t.Fatalf("client read auth result failed: %v", err)
	}
	if authResp[0] != userAuthVersion || authResp[1] != authSuccess {
		t.Errorf("auth should succeed, got: %v", authResp)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Sock5HandshakeBy returned error: %v", err)
	}
}

func TestSock5HandshakeByBadVersion(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)

	go func() {
		errCh <- Sock5HandshakeBy(server, "", "")
	}()

	// Send wrong SOCKS version
	if _, err := client.Write([]byte{0x04, 1, socks5AuthNone}); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	err := <-errCh
	if err == nil {
		t.Fatal("expected error for bad SOCKS version")
	}
}

func TestSock5GetRequestIPv4(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		rawaddr []byte
		host    string
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		rawaddr, host, err := Sock5GetRequest(server)
		resCh <- result{rawaddr, host, err}
	}()

	// Build SOCKS5 CONNECT request for 192.168.1.1:8080
	// ver=5, cmd=1(connect), rsv=0, atyp=1(IPv4), ip(4 bytes), port(2 bytes)
	req := []byte{
		socksVer5, socks5Connect, 0x00,
		Socks5AtypIP4,
		192, 168, 1, 1,
		0x1F, 0x90, // port 8080 big-endian
	}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Sock5GetRequest returned error: %v", res.err)
	}
	if res.host != "192.168.1.1:8080" {
		t.Errorf("host = %q, want %q", res.host, "192.168.1.1:8080")
	}
	if res.rawaddr == nil {
		t.Error("rawaddr should not be nil")
	}
}

func TestSock5GetRequestDomain(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		rawaddr []byte
		host    string
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		rawaddr, host, err := Sock5GetRequest(server)
		resCh <- result{rawaddr, host, err}
	}()

	domain := "example.com"
	// ver=5, cmd=1(connect), rsv=0, atyp=3(domain), len, domain, port(2 bytes)
	req := []byte{socksVer5, socks5Connect, 0x00, Socks5AtypDomain, byte(len(domain))}
	req = append(req, []byte(domain)...)
	req = append(req, 0x00, 0x50) // port 80

	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Sock5GetRequest returned error: %v", res.err)
	}
	if res.host != "example.com:80" {
		t.Errorf("host = %q, want %q", res.host, "example.com:80")
	}
}

func TestSock5GetRequestIPv6(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		rawaddr []byte
		host    string
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		rawaddr, host, err := Sock5GetRequest(server)
		resCh <- result{rawaddr, host, err}
	}()

	// ::1 in 16 bytes
	ipv6 := net.ParseIP("::1").To16()
	req := []byte{socksVer5, socks5Connect, 0x00, Socks5AtypIP6}
	req = append(req, ipv6...)
	req = append(req, 0x01, 0xBB) // port 443

	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Sock5GetRequest returned error: %v", res.err)
	}
	if !strings.Contains(res.host, "443") {
		t.Errorf("host = %q, expected port 443", res.host)
	}
}

func TestSock5GetRequestBadVersion(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		rawaddr []byte
		host    string
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		rawaddr, host, err := Sock5GetRequest(server)
		resCh <- result{rawaddr, host, err}
	}()

	// Wrong version
	req := []byte{0x04, socks5Connect, 0x00, Socks5AtypIP4, 127, 0, 0, 1, 0x00, 0x50}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestSock5GetRequestBadCmd(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		rawaddr []byte
		host    string
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		rawaddr, host, err := Sock5GetRequest(server)
		resCh <- result{rawaddr, host, err}
	}()

	// Unsupported command (BIND=2)
	req := []byte{socksVer5, 0x02, 0x00, Socks5AtypIP4, 127, 0, 0, 1, 0x00, 0x50}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err == nil {
		t.Fatal("expected error for unsupported command")
	}
}
