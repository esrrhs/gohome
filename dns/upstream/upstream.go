package upstream

import (
	"context"
	"net"
	"time"

	"github.com/miekg/dns"
)

// Upstream 是 DNS 上游的通用抽象接口
type Upstream interface {
	// Address 返回上游地址表示
	Address() string
	// Exchange 发送 DNS 查询并等待回包
	Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error)
}

// ExtractIPs 从返回的 dns.Msg 中提取所有 A 和 AAAA 记录中的 IP
func ExtractIPs(resp *dns.Msg) []net.IP {
	if resp == nil {
		return nil
	}
	var ips []net.IP
	for _, ans := range resp.Answer {
		if a, ok := ans.(*dns.A); ok {
			ips = append(ips, a.A)
		} else if aaaa, ok := ans.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA)
		}
	}
	return ips
}

// BuildQuery 构建指定域名与类型的 DNS 查询报文
func BuildQuery(domain string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)
	m.RecursionDesired = true
	m.Id = dns.Id()
	return m
}

// DefaultTimeout 默认查询超时时间
const DefaultTimeout = 4 * time.Second
