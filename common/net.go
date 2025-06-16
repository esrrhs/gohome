package common

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
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

// 判断IP是否为私有局域网IP
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	privateIPv4Blocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	privateIPv6Blocks := []string{
		"fc00::/7",  // ULA
		"fe80::/10", // Link-local
	}

	var blocks []string
	if ip.To4() != nil {
		blocks = privateIPv4Blocks
	} else {
		blocks = privateIPv6Blocks
	}

	for _, cidr := range blocks {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ResolveDomainToIP 接收域名或IP，返回解析结果或原始IP
func ResolveDomainToIP(domain string) (string, error) {
	domain = strings.TrimSpace(domain)

	// 如果是合法IP地址，直接返回
	if net.ParseIP(domain) != nil {
		return domain, nil
	}

	// 构造请求URL
	dohURL := "https://dns.alidns.com/resolve?name=" + url.QueryEscape(domain) + "&type=1&short=true"

	// 创建一个跳过 TLS 验证的 HTTP 客户端
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 5 * time.Second,
	}

	// 发起 GET 请求
	resp, err := client.Get(dohURL)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// 解析 JSON 响应
	var result []string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("JSON decode failed: %w", err)
	}

	// 返回第一个结果
	if len(result) > 0 {
		return result[0], nil
	}

	return "", fmt.Errorf("no valid A record found for domain: %s", domain)
}
