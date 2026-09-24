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

		if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
			dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
			if err != nil {
				return fmt.Errorf("create socks5 dialer failed: %w", err)
			}
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		} else if proxyURL.Scheme == "http" || proxyURL.Scheme == "https" {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	u.httpClient = &http.Client{
		Transport: transport,
		Timeout:   DefaultTimeout,
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

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(body); err != nil {
		return nil, err
	}
	return resp, nil
}
