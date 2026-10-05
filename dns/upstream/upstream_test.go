package upstream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startLocalUDPDNS 起一个最小 UDP DNS 桩，对收到的查询回一条 A 记录。
// 返回监听地址与停止函数；mode 用于控制特殊行为。
func startLocalUDPDNS(t *testing.T) (string, func()) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Skipf("cannot bind loopback udp: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 2048)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req := new(dns.Msg)
			if err := req.Unpack(buf[:n]); err != nil || len(req.Question) == 0 {
				continue
			}
			resp := new(dns.Msg)
			resp.SetReply(req)
			rr, rrErr := dns.NewRR(fmt.Sprintf("%s 60 IN A 1.2.3.4", req.Question[0].Name))
			if rrErr == nil {
				resp.Answer = append(resp.Answer, rr)
			}
			wire, packErr := resp.Pack()
			if packErr != nil {
				continue
			}
			if _, err := conn.WriteTo(wire, addr); err != nil {
				return
			}
		}
	}()

	return conn.LocalAddr().String(), func() {
		_ = conn.Close()
		wg.Wait()
	}
}

func TestNewSocketUpstream(t *testing.T) {
	cases := []struct {
		in      string
		wantNet string
		wantAd  string
	}{
		{"223.5.5.5:53", "udp", "223.5.5.5:53"},
		{"223.5.5.5", "udp", "223.5.5.5:53"},
		{"udp://119.29.29.29", "udp", "119.29.29.29:53"},
		{"tcp://8.8.8.8:5353", "tcp", "8.8.8.8:5353"},
		{"[2001:db8::1]:53", "udp", "[2001:db8::1]:53"},
	}
	for _, c := range cases {
		u, err := NewSocketUpstream(c.in)
		if err != nil {
			t.Fatalf("NewSocketUpstream(%q) unexpected error: %v", c.in, err)
		}
		if u.network != c.wantNet {
			t.Fatalf("NewSocketUpstream(%q) network = %q, want %q", c.in, u.network, c.wantNet)
		}
		if u.addr != c.wantAd {
			t.Fatalf("NewSocketUpstream(%q) addr = %q, want %q", c.in, u.addr, c.wantAd)
		}
		if u.Address() != fmt.Sprintf("%s://%s", c.wantNet, c.wantAd) {
			t.Fatalf("NewSocketUpstream(%q) Address() = %q", c.in, u.Address())
		}
	}
}

func TestNewSocketUpstreamInvalid(t *testing.T) {
	// 非法地址必须报错，而不是静默变成一个 ":53" 上游
	for _, in := range []string{"", "   ", "223.5.5.5:abc", "223.5.5.5:99999", "2001:db8::1"} {
		if u, err := NewSocketUpstream(in); err == nil {
			t.Fatalf("NewSocketUpstream(%q) expected error, got %+v", in, u)
		}
	}
}

func TestSocketUpstreamExchange(t *testing.T) {
	addr, stop := startLocalUDPDNS(t)
	defer stop()

	u, err := NewSocketUpstream(addr)
	if err != nil {
		t.Fatalf("NewSocketUpstream failed: %v", err)
	}

	req := BuildQuery("example.com", dns.TypeA)
	resp, err := u.Exchange(context.Background(), req)
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	ips := ExtractIPs(resp)
	if len(ips) != 1 || ips[0].String() != "1.2.3.4" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

func TestSocketUpstreamExchangeExpiredCtx(t *testing.T) {
	addr, stop := startLocalUDPDNS(t)
	defer stop()

	u, _ := NewSocketUpstream(addr)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if _, err := u.Exchange(ctx, BuildQuery("example.com", dns.TypeA)); err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestBuildQueryAndExtractIPs(t *testing.T) {
	m := BuildQuery("example.com", dns.TypeAAAA)
	if len(m.Question) != 1 || m.Question[0].Qtype != dns.TypeAAAA {
		t.Fatalf("BuildQuery produced wrong question: %+v", m.Question)
	}
	if !m.RecursionDesired {
		t.Fatalf("expected RecursionDesired to be set")
	}
	if m.Id == 0 {
		t.Fatalf("expected a random query id")
	}

	if got := ExtractIPs(nil); len(got) != 0 {
		t.Fatalf("ExtractIPs(nil) should be empty, got %v", got)
	}

	resp := new(dns.Msg)
	for _, s := range []string{"example.com. 60 IN A 1.2.3.4", "example.com. 60 IN AAAA 2001:db8::1", "example.com. 60 IN CNAME other.example.com."} {
		rr, err := dns.NewRR(s)
		if err != nil {
			t.Fatalf("dns.NewRR(%q): %v", s, err)
		}
		resp.Answer = append(resp.Answer, rr)
	}
	ips := ExtractIPs(resp)
	if len(ips) != 2 {
		t.Fatalf("expected 2 IPs (CNAME must be ignored), got %v", ips)
	}
}

// dohHandler 返回一个 DoH 桩服务；mode 控制是否篡改 id / 返回超长响应
func newDoHServer(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/dns-message" {
			t.Errorf("unexpected content type: %q", ct)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch mode {
		case "status":
			w.WriteHeader(http.StatusInternalServerError)
			return
		case "garbage":
			_, _ = w.Write([]byte("not-a-dns-message"))
			return
		case "toolarge":
			_, _ = w.Write(bytes.Repeat([]byte{0}, 70000))
			return
		}

		req := new(dns.Msg)
		if err := req.Unpack(body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := new(dns.Msg)
		resp.SetReply(req)
		if mode == "wrongid" {
			resp.Id = req.Id + 1
		}
		rr, _ := dns.NewRR(fmt.Sprintf("%s 60 IN A 5.6.7.8", req.Question[0].Name))
		resp.Answer = append(resp.Answer, rr)

		wire, err := resp.Pack()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(wire)
	}))
}

func TestDoHUpstreamExchange(t *testing.T) {
	srv := newDoHServer(t, "ok")
	defer srv.Close()

	u, err := NewDoHUpstream(srv.URL, "")
	if err != nil {
		t.Fatalf("NewDoHUpstream failed: %v", err)
	}
	defer u.Close()

	if u.Address() != srv.URL {
		t.Fatalf("Address() = %q, want %q", u.Address(), srv.URL)
	}

	req := BuildQuery("example.com", dns.TypeA)
	resp, err := u.Exchange(context.Background(), req)
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	ips := ExtractIPs(resp)
	if len(ips) != 1 || ips[0].String() != "5.6.7.8" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

// TestDoHUpstreamRejectsIDMismatch RFC 8484 要求回显查询 ID，不一致的响应不能采信
func TestDoHUpstreamRejectsIDMismatch(t *testing.T) {
	srv := newDoHServer(t, "wrongid")
	defer srv.Close()

	u, _ := NewDoHUpstream(srv.URL, "")
	defer u.Close()

	if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
		t.Fatalf("expected an error on id mismatch")
	}
}

func TestDoHUpstreamErrorModes(t *testing.T) {
	for _, mode := range []string{"status", "garbage", "toolarge"} {
		srv := newDoHServer(t, mode)
		u, _ := NewDoHUpstream(srv.URL, "")
		if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
			t.Errorf("mode %q: expected error, got nil", mode)
		}
		u.Close()
		srv.Close()
	}
}

func TestNewDoHUpstreamBadProxy(t *testing.T) {
	// 无法解析的代理地址
	if _, err := NewDoHUpstream("https://1.1.1.1/dns-query", "://not a url"); err == nil {
		t.Fatalf("expected error for unparsable proxy")
	}
	// 不支持的代理协议
	if _, err := NewDoHUpstream("https://1.1.1.1/dns-query", "ftp://127.0.0.1:21"); err == nil {
		t.Fatalf("expected error for unsupported proxy scheme")
	}
}

func TestDoHUpstreamSetProxy(t *testing.T) {
	srv := newDoHServer(t, "ok")
	defer srv.Close()

	u, err := NewDoHUpstream(srv.URL, "")
	if err != nil {
		t.Fatalf("NewDoHUpstream failed: %v", err)
	}
	defer u.Close()

	// 空地址表示取消代理，仍然可用
	if err := u.SetProxy(""); err != nil {
		t.Fatalf("SetProxy(\"\") failed: %v", err)
	}
	if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err != nil {
		t.Fatalf("Exchange after SetProxy failed: %v", err)
	}

	if err := u.SetProxy("http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetProxy(http) failed: %v", err)
	}
	if err := u.SetProxy("://bad"); err == nil {
		t.Fatalf("expected SetProxy to reject an invalid address")
	}
}

// TestDoHUpstreamRebuildClosesIdleConns 反复热更新代理不应遗留空闲连接
func TestDoHUpstreamRebuildClosesIdleConns(t *testing.T) {
	srv := newDoHServer(t, "ok")
	defer srv.Close()

	u, err := NewDoHUpstream(srv.URL, "")
	if err != nil {
		t.Fatalf("NewDoHUpstream failed: %v", err)
	}
	defer u.Close()

	if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := u.SetProxy(""); err != nil {
			t.Fatalf("SetProxy failed: %v", err)
		}
	}
	// 桩服务能正常关闭，说明没有残留的活跃连接
	srv.Close()
}
