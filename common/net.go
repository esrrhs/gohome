package common

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP, nil
}

var privatePrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),  // ULA
	netip.MustParsePrefix("fe80::/10"), // Link-local
}

// 判断IP是否为私有局域网IP
func IsPrivateIP(ipStr string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, pfx := range privatePrefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// 定义全局 client，只初始化一次
var dohClient *http.Client

func init() {
	dohClient = &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
			DisableKeepAlives: false, // 关键：开启连接复用
			MaxIdleConns:      100,   // 连接池大小
			IdleConnTimeout:   90 * time.Second,
		},
		Timeout: 5 * time.Second,
	}
}

// ResolveDomainToIP 接收域名或IP，返回解析结果或原始IP
func ResolveDomainToIP(domain string) (string, error) {
	domain = strings.TrimSpace(domain)

	// 如果是合法IP地址，直接返回
	if IsValidIP(domain) {
		return domain, nil
	}

	// 构造请求URL dns.alidns.com (223.5.5.5 具备合法 IP SAN TLS 证书)
	dohURL := "https://223.5.5.5/resolve?name=" + url.QueryEscape(domain) + "&type=1&short=true"

	req, err := http.NewRequestWithContext(context.Background(), "GET", dohURL, nil)
	if err != nil {
		return "", err
	}

	// 2. 使用全局 client
	resp, err := dohClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("DoH request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DoH HTTP status not ok: %d", resp.StatusCode)
	}

	// 解析 JSON 响应
	var result []string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return "", fmt.Errorf("JSON decode failed: %w", err)
	}

	// 返回第一个结果
	if len(result) > 0 {
		return result[0], nil
	}

	return "", fmt.Errorf("no valid A record found for domain: %s", domain)
}

// IsValidIP 判断输入字符串是否为合法IP地址
func IsValidIP(input string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(input))
	return err == nil && addr.IsValid()
}

// GetRootDomain 提取 eTLD+1 (例如: sub.baidu.com -> baidu.com)
func GetRootDomain(host string) (string, error) {
	// 1. 如果包含端口，先去除端口
	// 注意：net.SplitHostPort 强制要求输入包含冒号，如果没有冒号会报错，所以要判断
	if strings.Contains(host, ":") {
		h, _, err := net.SplitHostPort(host)
		if err == nil {
			host = h
		}
	}

	// 1.1 去掉根点：FQDN 常以 "." 结尾（如 "www.baidu.com."），
	// publicsuffix 会因空 label 报错，这里规范化掉
	host = strings.TrimSuffix(host, ".")

	// 2. 特殊处理 IP 地址 (如果是 IP，publicsuffix 会报错或返回空，视具体逻辑而定)
	// 如果业务只要域名，可以忽略这一步；如果想保留 IP 原样返回，加上这个判断
	if net.ParseIP(host) != nil {
		return host, nil
	}

	// 3. 【关键】处理 localhost 或无点的主机名
	// 如果没有 "."，说明它没有后缀，直接返回它自己
	if !strings.Contains(host, ".") {
		return host, nil
	}

	// 4. 使用 eTLD+1 算法提取
	// EffectiveTLDPlusOne 会自动处理 .co.uk, .com 等逻辑
	// 例如: "www.baidu.com" -> "baidu.com", nil
	//      "google.co.uk"  -> "google.co.uk", nil
	root, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", err
	}

	return root, nil
}
