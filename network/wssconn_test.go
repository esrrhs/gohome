package network

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func wssTestTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "wss.test"}, DNSNames: []string{"wss.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, &tls.Config{RootCAs: roots, ServerName: "wss.test"}
}
func wssTestListener(t *testing.T) (*WssConn, *tls.Config) {
	t.Helper()
	server, client := wssTestTLS(t)
	d := &WssConn{}
	cfg := DefaultWssConfig()
	cfg.TLSConfig = server
	d.SetConfig(cfg)
	l, err := d.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.(*WssConn), client
}
func wssTestPair(t *testing.T) (*WssConn, *WssConn, *WssConn) {
	t.Helper()
	l, tlsCfg := wssTestListener(t)
	d := &WssConn{}
	cfg := DefaultWssConfig()
	cfg.TLSConfig = tlsCfg
	d.SetConfig(cfg)
	c, err := d.Dial(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ch := make(chan Conn, 1)
	go func() { a, _ := l.Accept(); ch <- a }()
	select {
	case a := <-ch:
		if a == nil {
			t.Fatal("accept failed")
		}
		t.Cleanup(func() { _ = a.Close() })
		return c.(*WssConn), a.(*WssConn), l
	case <-time.After(3 * time.Second):
		t.Fatal("accept timeout")
	}
	return nil, nil, nil
}
func TestWssRegistrationAndValidation(t *testing.T) {
	c, err := NewConn("WSS")
	if err != nil || c.Name() != "wss" || !HasReliableProto("wss") || !HasProto("wss") {
		t.Fatal("wss registration", err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("factory accepted")
	}
	if _, err := c.Listen("127.0.0.1:0"); err == nil {
		t.Fatal("certificate required")
	}
	for _, dst := range []string{"ws://localhost:1234/gohome", "https://localhost/gohome", "wss://user:pass@localhost/", "wss://localhost/#fragment"} {
		if _, err := c.Dial(dst); err == nil {
			t.Fatalf("accepted %s", dst)
		}
	}
	for _, path := range []string{"relative", "//evil/path", "/foo?x=1", "/foo#bar", "/a%2Fb"} {
		d := &WssConn{}
		cfg := DefaultWssConfig()
		cfg.Path = path
		d.SetConfig(cfg)
		if _, err := d.Dial("127.0.0.1:1"); err == nil {
			t.Fatalf("accepted path %s", path)
		}
	}
	d := &WssConn{}
	d.SetConfig(&WssConfig{TLSConfig: &tls.Config{InsecureSkipVerify: true}})
	if _, err := d.Dial("127.0.0.1:1"); err == nil {
		t.Fatal("insecure config accepted")
	}
}
func TestWssTLSAndPathVerification(t *testing.T) {
	l, trust := wssTestListener(t)
	for _, name := range []string{"untrusted", "hostname", "path"} {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultWssConfig()
			cfg.TLSConfig = trust.Clone()
			switch name {
			case "untrusted":
				cfg.TLSConfig.RootCAs = x509.NewCertPool()
			case "hostname":
				cfg.TLSConfig.ServerName = "wrong.test"
			case "path":
				cfg.Path = "/wrong"
			}
			d := &WssConn{}
			d.SetConfig(cfg)
			if c, err := d.Dial(l.Addr().String()); err == nil {
				c.Close()
				t.Fatal("verification did not fail")
			}
		})
	}
}
func TestWssStreamLargeBidirectional(t *testing.T) {
	c, s, _ := wssTestPair(t)
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	up := make([]byte, 512*1024)
	down := make([]byte, 384*1024)
	for i := range up {
		up[i] = byte(i*17 + 13)
	}
	for i := range down {
		down[i] = byte(i*31 + 7)
	}
	errs := make(chan error, 4)
	write := func(dst Conn, p []byte) {
		for len(p) > 0 {
			n := min(len(p), 7919)
			got, err := dst.Write(p[:n])
			if err != nil {
				errs <- err
				return
			}
			if got != n {
				errs <- io.ErrShortWrite
				return
			}
			p = p[n:]
		}
		errs <- nil
	}
	read := func(src Conn, want []byte) {
		got := make([]byte, len(want))
		_, err := io.ReadFull(src, got)
		if err == nil && !bytes.Equal(got, want) {
			err = errors.New("payload corruption")
		}
		errs <- err
	}
	go write(c, up)
	go write(s, down)
	go read(s, up)
	go read(c, down)
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := c.Read(nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := c.Write(nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}
func TestWssFragmentedMessageAndEOF(t *testing.T) {
	l, trust := wssTestListener(t)
	tr := &http.Transport{TLSClientConfig: trust}
	defer tr.CloseIdleConnections()
	ws, _, err := websocket.Dial(context.Background(), "wss://"+l.Addr().String()+"/gohome", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	s, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.(*WssConn).SetReadDeadline(time.Now().Add(5 * time.Second))
	payload := bytes.Repeat([]byte("fragmented"), 16000)
	done := make(chan error, 1)
	go func() {
		w, err := ws.Writer(context.Background(), websocket.MessageBinary)
		if err == nil {
			for i := 0; i < len(payload) && err == nil; i += 1024 {
				_, err = w.Write(payload[i:min(i+1024, len(payload))])
			}
			if err == nil {
				err = w.Close()
			}
		}
		done <- err
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(s, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("fragmented corruption")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	go func() { done <- ws.Close(websocket.StatusNormalClosure, "") }()
	if _, err := s.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("want EOF: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("repeated EOF: %v", err)
	}
}
func TestWssDeadlinesAndClose(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		c, _, _ := wssTestPair(t)
		_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		_, err := c.Read(make([]byte, 1))
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("deadline: %v", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		c, _, _ := wssTestPair(t)
		_ = c.SetWriteDeadline(time.Now().Add(-time.Second))
		_, err := c.Write([]byte("x"))
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("deadline: %v", err)
		}
	})
	t.Run("blocked-write", func(t *testing.T) {
		c, _, _ := wssTestPair(t)
		_ = c.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		_, err := c.Write(make([]byte, 8*1024*1024))
		var timeout net.Error
		if !errors.Is(err, os.ErrDeadlineExceeded) || !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("blocked write deadline: %v", err)
		}
	})
	t.Run("reset", func(t *testing.T) {
		c, s, _ := wssTestPair(t)
		_ = c.SetDeadline(time.Now().Add(time.Second))
		_ = c.SetDeadline(time.Time{})
		go s.Write([]byte("x"))
		if _, err := io.ReadFull(c, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("listener", func(t *testing.T) {
		c, s, l := wssTestPair(t)
		done := make(chan error, 3)
		go func() { _, e := c.Read(make([]byte, 1)); done <- e }()
		go func() { _, e := s.Read(make([]byte, 1)); done <- e }()
		go func() { _, e := l.Accept(); done <- e }()
		_ = l.Close()
		for i := 0; i < 3; i++ {
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("close did not interrupt")
				}
			case <-time.After(time.Second):
				t.Fatal("close blocked")
			}
		}
		if _, err := c.Write([]byte("x")); err == nil {
			t.Fatal("write after close")
		}
		_ = l.Close()
	})
	t.Run("concurrent", func(t *testing.T) {
		c, s, _ := wssTestPair(t)
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_ = s.SetDeadline(time.Now().Add(5 * time.Second))
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := c.Write(bytes.Repeat([]byte("x"), 1024))
				if err != nil {
					t.Error(err)
				}
			}()
		}
		got := make([]byte, 8*1024)
		if _, err := io.ReadFull(s, got); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if !bytes.Equal(got, bytes.Repeat([]byte("x"), len(got))) {
			t.Fatal("concurrent corruption")
		}
	})
}
func TestWssDialCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); accepted <- c }()
	d := &WssConn{}
	done := make(chan error, 1)
	go func() { _, err := d.Dial(ln.Addr().String()); done <- err }()
	conn := <-accepted
	defer conn.Close()
	_ = d.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("uncanceled dial")
		}
	case <-time.After(time.Second):
		t.Fatal("dial stuck")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&WssConn{}).DialContext(ctx, ln.Addr().String()); err == nil {
		t.Fatal("canceled context ignored")
	}
}
func TestWssReverseProxyTLS(t *testing.T) {
	// A TLS reverse proxy preserves Upgrade and verifies the TLS upstream.
	l, trust := wssTestListener(t)
	proxyURL := "https://" + l.Addr().String()
	target, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	u := httputil.NewSingleHostReverseProxy(target)
	tr := &http.Transport{TLSClientConfig: trust}
	defer tr.CloseIdleConnections()
	u.Transport = tr
	proxy := httptest.NewTLSServer(u)
	defer proxy.Close()
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	d := &WssConn{}
	d.SetConfig(&WssConfig{TLSConfig: &tls.Config{RootCAs: roots}, Path: "/gohome"})
	c, err := d.Dial("wss" + proxy.URL[len("https"):])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = c.(*WssConn).SetDeadline(time.Now().Add(3 * time.Second))
	_ = s.(*WssConn).SetDeadline(time.Now().Add(3 * time.Second))
	go s.Write([]byte("proxy"))
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "proxy" {
		t.Fatal(string(got), err)
	}
}
