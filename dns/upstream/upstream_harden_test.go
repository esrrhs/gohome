package upstream

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// ===========================================================================
// SocketUpstream：残余构造分支与带 deadline 的 Exchange
// ===========================================================================

func TestHardenNewSocketUpstreamMoreCases(t *testing.T) {
	// 合法补充：udp:// 显式前缀带端口、tcp:// 域名主机
	for _, c := range []struct {
		in      string
		network string
		addr    string
	}{
		{"udp://1.2.3.4:5353", "udp", "1.2.3.4:5353"},
		{"tcp://dns.example.com:53", "tcp", "dns.example.com:53"},
		{"udp://[2001:db8::2]:5353", "udp", "[2001:db8::2]:5353"},
	} {
		u, err := NewSocketUpstream(c.in)
		if err != nil {
			t.Fatalf("NewSocketUpstream(%q): %v", c.in, err)
		}
		if u.network != c.network || u.addr != c.addr {
			t.Fatalf("NewSocketUpstream(%q) = %s://%s", c.in, u.network, u.addr)
		}
	}

	// 非法补充
	for _, in := range []string{
		"tcp://:53",   // host 为空
		"[::1",        // 括号不闭合，SplitHostPort 必失败
		"udp://   ",   // trim 后只剩 scheme
		"1.2.3.4:",    // 端口段为空
		"1.2.3.4:53x", // 端口非数字
	} {
		if u, err := NewSocketUpstream(in); err == nil {
			t.Fatalf("NewSocketUpstream(%q) expected error, got %+v", in, u)
		}
	}
}

func TestHardenSocketUpstreamFutureDeadline(t *testing.T) {
	addr, stop := startLocalUDPDNS(t)
	defer stop()

	u, err := NewSocketUpstream(addr)
	if err != nil {
		t.Fatal(err)
	}

	// 带未来 deadline：覆盖按 ctx 剩余时间构造 dns.Client 的分支
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := u.Exchange(ctx, BuildQuery("example.com", dns.TypeA))
	if err != nil {
		t.Fatalf("Exchange with future deadline: %v", err)
	}
	if ips := ExtractIPs(resp); len(ips) != 1 || ips[0].String() != "1.2.3.4" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

// ===========================================================================
// DoH：Exchange 的报文/请求/连接错误路径
// ===========================================================================

func TestHardenDoHExchangePackError(t *testing.T) {
	srv := newDoHServer(t, "ok")
	defer srv.Close()
	u, err := NewDoHUpstream(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	// 单个 label 超过 63 字节，Pack 必失败
	bad := new(dns.Msg)
	bad.SetQuestion(strings.Repeat("a", 64)+".example.", dns.TypeA)
	if _, err := u.Exchange(context.Background(), bad); err == nil {
		t.Fatal("Exchange must surface Pack errors")
	}
}

func TestHardenDoHExchangeRequestAndDialErrors(t *testing.T) {
	// endpoint 不是合法 URL：构造期不校验，请求期必须报错
	u, err := NewDoHUpstream("://bad-endpoint", "")
	if err != nil {
		t.Fatalf("constructor should not parse endpoint: %v", err)
	}
	if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
		t.Fatal("invalid endpoint must fail at request build")
	}

	// 连不上的地址：client.Do 透传网络错误
	u2, err := NewDoHUpstream("http://127.0.0.1:1/dns-query", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u2.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
		t.Fatal("connection refused must surface")
	}

	// ctx 在请求发出前取消
	srv := newDoHServer(t, "ok")
	defer srv.Close()
	u3, _ := NewDoHUpstream(srv.URL, "")
	defer u3.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u3.Exchange(ctx, BuildQuery("example.com", dns.TypeA)); err == nil {
		t.Fatal("pre-canceled ctx must fail")
	}
}

// TestHardenDoHResponseBodyTruncated 响应头宣称的长度没发完就断连，
// 读响应体必须报错而不是当成空报文解包。
func TestHardenDoHResponseBodyTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("hijack unsupported")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		// Content-Length=100 但只发 10 字节后断连
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: application/dns-message\r\n\r\n")
		_, _ = buf.WriteString("0123456789")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	u, _ := NewDoHUpstream(srv.URL, "")
	defer u.Close()
	_, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA))
	if err == nil {
		t.Fatal("truncated body must error")
	}
}

// ===========================================================================
// SOCKS5 代理：最小本地实现验证 rebuildClient 的 socks5 分支端到端可用
// ===========================================================================

// startMiniSOCKS5 起一个无认证 SOCKS5 CONNECT 代理（支持 ATYP IPv4/域名）。
// failReply=true 时对所有请求回 REP=0x01 失败。
func startMiniSOCKS5(t *testing.T, failReply bool) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen: %v", err)
	}
	var acceptWg sync.WaitGroup

	handle := func(c net.Conn) {
		defer c.Close()
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 5 {
			return
		}
		methods := make([]byte, hdr[1])
		if _, err := io.ReadFull(c, methods); err != nil {
			return
		}
		if _, err := c.Write([]byte{5, 0}); err != nil { // NoAuth
			return
		}
		reqHdr := make([]byte, 4)
		if _, err := io.ReadFull(c, reqHdr); err != nil || reqHdr[0] != 5 || reqHdr[1] != 1 {
			return
		}
		var host string
		switch reqHdr[3] {
		case 1: // IPv4
			buf := make([]byte, 4)
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}
			host = net.IP(buf).String()
		case 3: // domain
			lbuf := make([]byte, 1)
			if _, err := io.ReadFull(c, lbuf); err != nil {
				return
			}
			buf := make([]byte, lbuf[0])
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}
			host = string(buf)
		default:
			return
		}
		pbuf := make([]byte, 2)
		if _, err := io.ReadFull(c, pbuf); err != nil {
			return
		}
		port := int(pbuf[0])<<8 | int(pbuf[1])
		target := net.JoinHostPort(host, strconv.Itoa(port))

		if failReply {
			_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0}) // general failure
			return
		}
		up, err := net.Dial("tcp", target)
		if err != nil {
			_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
			return
		}
		defer up.Close()
		if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
		go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
		<-done
	}

	// handler goroutine 可能在 keep-alive 连接上阻塞，随目标服务关闭自行退出，
	// stop 只回收 listener 与 accept 循环，避免死等。
	acceptWg.Add(1)
	go func() {
		defer acceptWg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()

	return l.Addr().String(), func() {
		_ = l.Close()
		acceptWg.Wait()
	}
}

func TestHardenDoHSOCKS5Proxy(t *testing.T) {
	srv := newDoHServer(t, "ok")
	defer srv.Close()

	proxyAddr, stopProxy := startMiniSOCKS5(t, false)
	defer stopProxy()

	// socks5 与 socks5h 两种 scheme 都必须能建出 dialer 并完成请求
	for _, scheme := range []string{"socks5", "socks5h"} {
		u, err := NewDoHUpstream(srv.URL, scheme+"://"+proxyAddr)
		if err != nil {
			t.Fatalf("NewDoHUpstream via %s: %v", scheme, err)
		}
		resp, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA))
		_ = u.Close()
		if err != nil {
			t.Fatalf("Exchange via %s proxy: %v", scheme, err)
		}
		if ips := ExtractIPs(resp); len(ips) != 1 || ips[0].String() != "5.6.7.8" {
			t.Fatalf("%s proxy unexpected ips: %v", scheme, ips)
		}
	}

	// 代理握手成功但回 CONNECT 失败：错误必须透出
	failAddr, stopFail := startMiniSOCKS5(t, true)
	defer stopFail()
	u, err := NewDoHUpstream(srv.URL, "socks5://"+failAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if _, err := u.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
		t.Fatal("proxy CONNECT failure must surface")
	}

	// x/net v0.59 的 SOCKS5 构造器不校验地址，错误延迟到首次拨号
	for _, p := range []string{"socks5://", "socks5://noport"} {
		uBad, err := NewDoHUpstream(srv.URL, p)
		if err != nil {
			t.Fatalf("dialer for %q builds lazily, got %v", p, err)
		}
		if _, err := uBad.Exchange(context.Background(), BuildQuery("example.com", dns.TypeA)); err == nil {
			t.Fatalf("proxy %q must fail at dial time", p)
		}
		_ = uBad.Close()
	}
}
