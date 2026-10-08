package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/proxy"
)

// maxDoHResponseSize 是接受的 DoH 响应体上限（DNS 报文最大 65535 字节）
const maxDoHResponseSize = 65535

// DoHUpstream 实现了基于 DNS-over-HTTPS (RFC 8484) 的加密 DNS 上游，支持 Socks5/HTTP 代理
type DoHUpstream struct {
	mu         sync.RWMutex
	endpoint   string
	proxyAddr  string
	httpClient *http.Client
}

// NewDoHUpstream 创建 DoH 上游 (e.g. "https://1.1.1.1/dns-query", proxyAddr: "socks5://127.0.0.1:1080" 或空)
func NewDoHUpstream(endpoint string, proxyAddr string) (*DoHUpstream, error) {
	u := &DoHUpstream{
		endpoint:  endpoint,
		proxyAddr: proxyAddr,
	}
	if err := u.rebuildClient(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *DoHUpstream) rebuildClient() error {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		DisableKeepAlives: false,
		MaxIdleConns:      100,
		IdleConnTimeout:   90 * time.Second,
	}

	if u.proxyAddr != "" {
		proxyURL, err := url.Parse(u.proxyAddr)
		if err != nil {
			return fmt.Errorf("invalid proxy address: %w", err)
		}

		switch proxyURL.Scheme {
		case "socks5", "socks5h":
			dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
			if err != nil {
				return fmt.Errorf("create socks5 dialer failed: %w", err)
			}
			// 优先使用带 ctx 的拨号，让上游超时/取消能真正中断建连
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				transport.DialContext = cd.DialContext
			} else {
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				}
			}
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		default:
			return fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
		}
	}

	old := u.httpClient
	u.httpClient = &http.Client{
		Transport: transport,
		Timeout:   DefaultTimeout,
	}

	// 释放旧 transport 上的空闲连接，否则每次热更新都会留下一批僵尸连接
	if old != nil && old.Transport != nil {
		if t, ok := old.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
	return nil
}

// SetProxy 动态更新代理地址并热重载 HTTP Client
func (u *DoHUpstream) SetProxy(proxyAddr string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.proxyAddr = proxyAddr
	return u.rebuildClient()
}

// Close 释放 DoH 上游占用的空闲连接。
// 返回 error 是为了满足 io.Closer，使 resolver 重建上游时能通过类型断言关闭本类型。
func (u *DoHUpstream) Close() error {
	u.mu.RLock()
	client := u.httpClient
	u.mu.RUnlock()

	if client != nil && client.Transport != nil {
		if t, ok := client.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
	return nil
}

func (u *DoHUpstream) Address() string {
	return u.endpoint
}

func (u *DoHUpstream) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	wire, err := req.Pack()
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint, bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	u.mu.RLock()
	client := u.httpClient
	u.mu.RUnlock()

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh status not ok: %s", httpResp.Status)
	}

	// DoH 响应大小有界，超长直接判定为异常，避免被恶意/异常上游拖进大内存分配
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxDoHResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDoHResponseSize {
		return nil, fmt.Errorf("doh response too large: %d bytes", len(body))
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(body); err != nil {
		return nil, err
	}

	// RFC 8484 要求服务端回显查询 ID；不一致说明响应与本次查询不对应，不能采信
	if resp.Id != req.Id {
		return nil, fmt.Errorf("doh response id mismatch: got %d, want %d", resp.Id, req.Id)
	}
	return resp, nil
}
