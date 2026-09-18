package network

import (
	"errors"
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
	go func() {
		errCh <- Sock5HandshakeBy(server, "", "")
	}()

	if _, err := client.Write([]byte{socksVer5, 1, socks5AuthNone}); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

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

	if _, err := client.Write([]byte{socksVer5, 1, socks5UserPassAuth}); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	resp := make([]byte, 2)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("client read method selection failed: %v", err)
	}
	if resp[0] != socksVer5 || resp[1] != UserPassAuth {
		t.Fatalf("unexpected method selection: %v", resp)
	}

	authMsg := []byte{userAuthVersion, byte(len(user))}
	authMsg = append(authMsg, []byte(user)...)
	authMsg = append(authMsg, byte(len(pass)))
	authMsg = append(authMsg, []byte(pass)...)
	if _, err := client.Write(authMsg); err != nil {
		t.Fatalf("client write auth failed: %v", err)
	}

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

func TestSock5HandshakeByAuthFailure(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Sock5HandshakeBy(server, "admin", "secret")
	}()

	if _, err := client.Write([]byte{socksVer5, 1, socks5UserPassAuth}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatal(err)
	}

	bad := []byte{userAuthVersion, 1, 'x', 1, 'y'}
	if _, err := client.Write(bad); err != nil {
		t.Fatal(err)
	}
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(client, authResp); err != nil {
		t.Fatal(err)
	}
	if authResp[1] != authFailure {
		t.Fatalf("want authFailure, got %v", authResp)
	}

	err := <-errCh
	if err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestSock5HandshakeByNoAcceptableMethod(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Sock5HandshakeBy(server, "u", "p")
	}()

	// Client only offers NoAuth while server requires UserPass
	if _, err := client.Write([]byte{socksVer5, 1, socks5AuthNone}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatal(err)
	}
	if resp[1] != socks5NoAcceptable {
		t.Fatalf("want 0xFF, got %v", resp)
	}
	if err := <-errCh; err == nil {
		t.Fatal("expected errMethod")
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

	if _, err := client.Write([]byte{0x04, 0}); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	err := <-errCh
	if err == nil {
		t.Fatal("expected error for bad SOCKS version")
	}
}

func TestSock5HandshakeClientUserPassRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer c.Close()
		errCh <- Sock5HandshakeBy(c, "u", "p")
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tcp := conn.(*net.TCPConn)
	if err := Sock5Handshake(tcp, 3000, "u", "p"); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}

func TestSock5SetRequestRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer c.Close()
		if err := Sock5HandshakeBy(c, "", ""); err != nil {
			errCh <- err
			return
		}
		_, host, err := Sock5GetRequest(c)
		if err != nil {
			errCh <- err
			return
		}
		if host != "example.com:80" {
			errCh <- errors.New("unexpected host: " + host)
			return
		}
		errCh <- Sock5SendConnectReply(c, 0, "127.0.0.1:1080")
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tcp := conn.(*net.TCPConn)
	if err := Sock5Handshake(tcp, 3000, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := Sock5SetRequest(tcp, "example.com", 80, 3000); err != nil {
		t.Fatalf("SetRequest: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server: %v", err)
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

	req := []byte{
		socksVer5, socks5Connect, 0x00,
		Socks5AtypIP4,
		192, 168, 1, 1,
		0x1F, 0x90,
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
	req := []byte{socksVer5, socks5Connect, 0x00, Socks5AtypDomain, byte(len(domain))}
	req = append(req, []byte(domain)...)
	req = append(req, 0x00, 0x50)

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

	ipv6 := net.ParseIP("::1").To16()
	req := []byte{socksVer5, socks5Connect, 0x00, Socks5AtypIP6}
	req = append(req, ipv6...)
	req = append(req, 0x01, 0xBB)

	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Sock5GetRequest returned error: %v", res.err)
	}
	if !strings.Contains(res.host, "443") || !strings.Contains(res.host, "::1") {
		t.Errorf("host = %q, expected [::1]:443 form", res.host)
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

	// VER CMD RSV ATYP only — enough for version rejection with net.Pipe.
	req := []byte{0x04, socks5Connect, 0x00, Socks5AtypIP4}
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

	// Unsupported command (BIND=2); header only for net.Pipe.
	req := []byte{socksVer5, 0x02, 0x00, Socks5AtypIP4}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	res := <-resCh
	if res.err == nil {
		t.Fatal("expected error for unsupported command")
	}
}

func TestSock5GetRequestStickyFollowOn(t *testing.T) {
	// CONNECT request followed immediately by application data must not fail.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type result struct {
		host string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		_, host, err := Sock5GetRequest(server)
		resCh <- result{host, err}
	}()

	req := []byte{
		socksVer5, socks5Connect, 0x00, Socks5AtypIP4,
		10, 0, 0, 1, 0x00, 0x50,
	}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = client.Write([]byte("HI"))
	}()

	res := <-resCh
	if res.err != nil {
		t.Fatalf("sticky packet should be ok with exact reads: %v", res.err)
	}
	if res.host != "10.0.0.1:80" {
		t.Fatalf("host=%q", res.host)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "HI" {
		t.Fatalf("payload=%q", buf)
	}
}
