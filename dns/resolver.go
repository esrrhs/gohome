package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/esrrhs/gohome/dns/cache"
	"github.com/esrrhs/gohome/dns/fakeip"
	"github.com/esrrhs/gohome/dns/matcher"
	"github.com/esrrhs/gohome/dns/upstream"
	"github.com/esrrhs/gohome/loggo"
	"github.com/miekg/dns"
)

// Config DNS Resolver 全局配置
type Config struct {
	// 直连国内上游（支持 UDP/TCP，例如 "223.5.5.5:53"）
	DirectUpstreams []string
	// 远程海外上游（支持 DoH，例如 "https://1.1.1.1/dns-query"）
	RemoteUpstreams []string
	// 访问远程上游的代理地址（支持 socks5://127.0.0.1:1080 或 http://...）
	ProxyAddr string

	// 是否启用 Fake-IP 模式（默认 false，返回真实解析结果）
	EnableFakeIP bool
	// Fake-IP 网段，默认为 "198.18.0.0/15"
	FakeIPRange string
	// Fake-IP 保留 TTL，默认 2 小时
	FakeIPTTL time.Duration

	// 规则文件与列表
	DirectDomainFiles []string // 额外的国内直连域名文件（如 dnsmasq 格式或逐行域名）
	ProxyDomainFiles  []string // 额外的代理域名文件
	DirectDomains     []string // 额外的直连域名
	ProxyDomains      []string // 额外的代理域名
	DirectCIDRs       []string // 直连 IP/CIDR 网段

	// GeoIP 数据库文件路径（GeoLite2-Country.mmdb）
	GeoIPFile string

	// 查询超时时间，默认 3s
	Timeout time.Duration
	// 缓存容量，默认 2000
	CacheCapacity int
}

// DefaultConfig 提供零配置、开箱即用的默认参数
func DefaultConfig() Config {
	return Config{
		DirectUpstreams: DefaultDomesticDNS,
		RemoteUpstreams: DefaultRemoteDoH,
		EnableFakeIP:    false, // 默认关闭 Fake-IP，业务按需开启
		FakeIPRange:     "198.18.0.0/15",
		FakeIPTTL:       2 * time.Hour,
		Timeout:         3 * time.Second,
		CacheCapacity:   2000,
	}
}

// Resolver 核心接口
type Resolver interface {
	// 基础解析：输入域名返回 IP 列表（支持 Fake-IP 或真实 DNS 智能分流解析）
	Resolve(ctx context.Context, domain string) ([]net.IP, error)
	// 解析单个最优 IP
	ResolveOne(ctx context.Context, domain string) (net.IP, error)
	// 完整 DNS 协议交互处理（用于作为 DNS 服务器监听回包）
	Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error)
	// 快速分流判断：指定域名或 IP 是否应当走代理
	ShouldProxy(domainOrIP string) (bool, error)
	// Fake-IP 反查原始域名
	LookupDomainByFakeIP(ip net.IP) (string, bool)
	LookupDomainByFakeIPStr(ipStr string) (string, bool)
	// 判断指定 IP 是否是 Fake-IP
	IsFakeIP(ip net.IP) bool

	// ---- 动态热更新接口（供业务方定时或事件驱动调用）----
	// 更新直连/私有 CIDR 列表
	UpdateDirectIPs(cidrs []string) int
	// 更新直连域名列表
	UpdateDirectDomains(domains []string)
	// 更新代理域名列表
	UpdateProxyDomains(domains []string)
	// 从文件加载追加直连域名（支持 dnsmasq 格式）
	LoadDirectDomainFile(filePath string) (int, error)
	// 从文件加载追加代理域名
	LoadProxyDomainFile(filePath string) (int, error)
	// 热重载 GeoIP 数据库文件
	ReloadGeoIPDatabase(filePath string) error
	// 动态更新上游代理地址 (如 socks5://127.0.0.1:1080)
	UpdateProxyAddress(proxyAddr string) error
	// 清空 DNS 缓存
	ClearCache()
}

// StandardResolver 实现了 Resolver 接口
type StandardResolver struct {
	cfg Config
	mu  sync.RWMutex

	directUpstreams []upstream.Upstream
	remoteUpstreams []upstream.Upstream

	directDomains *matcher.DomainTrie
	proxyDomains  *matcher.DomainTrie
	directIPs     *matcher.IPNetList
	geodb         *matcher.GeoDB

	fakeIPPool *fakeip.FakeIPPool
	cache      *cache.DNSCache
}

// NewResolver 创建并初始化一个 Resolver 实例
func NewResolver(cfg Config) (*StandardResolver, error) {
	// 补全默认项
	if len(cfg.DirectUpstreams) == 0 {
		cfg.DirectUpstreams = DefaultDomesticDNS
	}
	if len(cfg.RemoteUpstreams) == 0 {
		cfg.RemoteUpstreams = DefaultRemoteDoH
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.CacheCapacity <= 0 {
		cfg.CacheCapacity = 2000
	}

	r := &StandardResolver{
		cfg:           cfg,
		directDomains: matcher.NewDomainTrie(),
		proxyDomains:  matcher.NewDomainTrie(),
		directIPs:     matcher.NewIPNetList(),
		geodb:         matcher.NewGeoDB(),
		cache:         cache.NewDNSCache(cfg.CacheCapacity),
	}

	// 1. 初始化 Fake-IP 池
	if cfg.EnableFakeIP {
		r.fakeIPPool = fakeip.NewFakeIPPool(fakeip.Config{
			CIDR: cfg.FakeIPRange,
			TTL:  cfg.FakeIPTTL,
		})
	}

	// 2. 初始化直连 CIDR
	cidrs := append([]string{}, DefaultReservedCIDRs...)
	cidrs = append(cidrs, cfg.DirectCIDRs...)
	r.directIPs.Reset(cidrs)

	// 3. 初始化默认顶级域与主干域名
	for _, tld := range DefaultDirectTLDs {
		r.directDomains.Add(tld)
	}
	for _, d := range DefaultChinaMainDomains {
		r.directDomains.Add(d)
	}
	for _, d := range cfg.DirectDomains {
		r.directDomains.Add(d)
	}
	for _, d := range cfg.ProxyDomains {
		r.proxyDomains.Add(d)
	}

	// 4. 加载文件规则
	for _, f := range cfg.DirectDomainFiles {
		if _, err := os.Stat(f); err == nil {
			_, _ = r.directDomains.LoadFromFile(f)
		}
	}
	for _, f := range cfg.ProxyDomainFiles {
		if _, err := os.Stat(f); err == nil {
			_, _ = r.proxyDomains.LoadFromFile(f)
		}
	}

	// 5. 初始化 GeoIP（若文件存在）
	if cfg.GeoIPFile != "" {
		if _, err := os.Stat(cfg.GeoIPFile); err == nil {
			if err := r.geodb.Open(cfg.GeoIPFile); err != nil {
				loggo.Warn("[DNS] Load GeoIP failed: %v", err)
			}
		}
	}

	// 6. 初始化上游
	if err := r.initUpstreams(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *StandardResolver) initUpstreams() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var directs []upstream.Upstream
	for _, addr := range r.cfg.DirectUpstreams {
		if strings.HasPrefix(addr, "https://") || strings.HasPrefix(addr, "http://") {
			u, err := upstream.NewDoHUpstream(addr, "")
			if err == nil {
				directs = append(directs, u)
			}
		} else {
			u, err := upstream.NewSocketUpstream(addr)
			if err == nil {
				directs = append(directs, u)
			}
		}
	}

	var remotes []upstream.Upstream
	for _, addr := range r.cfg.RemoteUpstreams {
		if strings.HasPrefix(addr, "https://") || strings.HasPrefix(addr, "http://") {
			u, err := upstream.NewDoHUpstream(addr, r.cfg.ProxyAddr)
			if err == nil {
				remotes = append(remotes, u)
			}
		} else {
			u, err := upstream.NewSocketUpstream(addr)
			if err == nil {
				remotes = append(remotes, u)
			}
		}
	}

	if len(directs) == 0 {
		return errors.New("no valid direct upstreams available")
	}
	r.directUpstreams = directs
	r.remoteUpstreams = remotes
	return nil
}

// Resolve 解析域名返回 IP 列表
func (r *StandardResolver) Resolve(ctx context.Context, domain string) ([]net.IP, error) {
	d := fakeip.NormalizeDomain(domain)
	if ip := net.ParseIP(d); ip != nil {
		return []net.IP{ip}, nil
	}

	// 若启用了 Fake-IP，且不是白名单直连域名，则直接分配 Fake-IP
	if r.cfg.EnableFakeIP && r.fakeIPPool != nil {
		if !r.directDomains.Has(d) {
			fakeIP := r.fakeIPPool.Allocate(d)
			return []net.IP{fakeIP}, nil
		}
	}

	// 发起 A 与 AAAA 记录查询
	msg := upstream.BuildQuery(d, dns.TypeA)
	resp, err := r.Exchange(ctx, msg)
	if err != nil {
		return nil, err
	}

	ips := upstream.ExtractIPs(resp)
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP answer for %s", domain)
	}
	return ips, nil
}

// ResolveOne 解析返回单个最优 IP
func (r *StandardResolver) ResolveOne(ctx context.Context, domain string) (net.IP, error) {
	ips, err := r.Resolve(ctx, domain)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP resolved for %s", domain)
	}
	// 优先返回 IPv4
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip, nil
		}
	}
	return ips[0], nil
}

// Exchange 处理完整的 DNS Msg
func (r *StandardResolver) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	if req == nil || len(req.Question) == 0 {
		return nil, errors.New("empty dns question")
	}

	q := req.Question[0]
	qName := fakeip.NormalizeDomain(q.Name)

	// 1. 检查 Fake-IP 模式 (仅对 A 记录)
	if r.cfg.EnableFakeIP && r.fakeIPPool != nil && q.Qtype == dns.TypeA {
		if !r.directDomains.Has(qName) {
			fakeIP := r.fakeIPPool.Allocate(qName)
			resp := new(dns.Msg)
			resp.SetReply(req)
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: fakeIP,
			}
			resp.Answer = append(resp.Answer, rr)
			return resp, nil
		}
	}

	// 2. PTR 反向查询处理 (当开启 Fake-IP 时)
	if r.cfg.EnableFakeIP && r.fakeIPPool != nil && q.Qtype == dns.TypePTR {
		if strings.HasSuffix(qName, ".in-addr.arpa") {
			ipStr := arpaToIPv4(qName)
			if d, ok := r.fakeIPPool.LookupDomainByIPStr(ipStr); ok {
				resp := new(dns.Msg)
				resp.SetReply(req)
				rr := &dns.PTR{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypePTR,
						Class:  dns.ClassINET,
						Ttl:    60,
					},
					Ptr: dns.Fqdn(d),
				}
				resp.Answer = append(resp.Answer, rr)
				return resp, nil
			}
		}
	}

	// 3. 检查缓存
	cacheKey := cache.KeyForMsg(qName, q.Qtype, q.Qclass)
	if cached, ok := r.cache.Get(cacheKey); ok {
		resp := cached.Copy()
		resp.Id = req.Id
		return resp, nil
	}

	// 4. 防并发击穿 SingleFlight
	result, err := r.cache.DoSingleFlight(cacheKey, func() (*dns.Msg, error) {
		res, err := r.exchangeInternal(ctx, req, qName)
		if err == nil && res != nil && len(res.Answer) > 0 {
			r.cache.Set(cacheKey, res)
		}
		return res, err
	})
	if err != nil {
		return nil, err
	}

	resp := result.Copy()
	resp.Id = req.Id
	return resp, nil
}

func (r *StandardResolver) exchangeInternal(ctx context.Context, req *dns.Msg, qName string) (*dns.Msg, error) {
	// A. 显式命中白名单直连域名 -> 仅走国内上游
	if r.directDomains.Has(qName) {
		return r.queryDirect(ctx, req)
	}

	// B. 显式命中黑名单代理域名 -> 仅走海外加密上游
	if r.proxyDomains.Has(qName) && len(r.remoteUpstreams) > 0 {
		return r.queryRemote(ctx, req)
	}

	// C. 未命中域名：并发竞速（Dual-Stack Parallel Query）
	// 同时向国内直连上游和海外远程上游发起请求
	return r.queryParallel(ctx, req)
}

func (r *StandardResolver) queryDirect(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	r.mu.RLock()
	upstreams := r.directUpstreams
	r.mu.RUnlock()

	var lastErr error
	for _, u := range upstreams {
		resp, err := u.Exchange(ctx, req)
		if err == nil && resp != nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all direct upstreams failed: %w", lastErr)
}

func (r *StandardResolver) queryRemote(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	r.mu.RLock()
	upstreams := r.remoteUpstreams
	r.mu.RUnlock()

	if len(upstreams) == 0 {
		// 若未配置远程上游，回退到国内
		return r.queryDirect(ctx, req)
	}

	var lastErr error
	for _, u := range upstreams {
		resp, err := u.Exchange(ctx, req)
		if err == nil && resp != nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all remote upstreams failed: %w", lastErr)
}

// queryParallel 并发查询国内与海外上游
func (r *StandardResolver) queryParallel(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	r.mu.RLock()
	hasRemote := len(r.remoteUpstreams) > 0
	r.mu.RUnlock()

	if !hasRemote {
		return r.queryDirect(ctx, req)
	}

	type queryResult struct {
		isDirect bool
		resp     *dns.Msg
		err      error
	}

	ch := make(chan queryResult, 2)
	queryCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	// 协程 1: 国内查询
	go func() {
		resp, err := r.queryDirect(queryCtx, req)
		ch <- queryResult{isDirect: true, resp: resp, err: err}
	}()

	// 协程 2: 海外安全查询
	go func() {
		resp, err := r.queryRemote(queryCtx, req)
		ch <- queryResult{isDirect: false, resp: resp, err: err}
	}()

	var remoteResp *dns.Msg
	received := 0

	for received < 2 {
		select {
		case res := <-ch:
			received++
			if res.err != nil || res.resp == nil {
				continue
			}

			if !res.isDirect {
				remoteResp = res.resp
				// 如果国内已经失败且海外成功，直接采纳海外
				if received == 2 {
					return remoteResp, nil
				}
			} else {
				// 国内返回，验证 IP 合法性
				ips := upstream.ExtractIPs(res.resp)
				if len(ips) > 0 {
					// 检查 IP 是否全都在中国境内或私有网段
					if r.isAllDomestic(ips) {
						return res.resp, nil
					}
					// 命中境外 IP 或疑似污染，放弃国内结果，等待/使用海外结果
					loggo.Info("[DNS] Domestic answer contains foreign IP for %s, discarding", req.Question[0].Name)
				}
			}
		case <-queryCtx.Done():
			if remoteResp != nil {
				return remoteResp, nil
			}
			return nil, queryCtx.Err()
		}
	}

	if remoteResp != nil {
		return remoteResp, nil
	}
	return nil, errors.New("parallel dns query failed")
}

func (r *StandardResolver) isAllDomestic(ips []net.IP) bool {
	for _, ip := range ips {
		// 1. 如果属于私有保留网段，允许
		if r.directIPs.Contains(ip) {
			continue
		}
		// 2. 如果配置了 GeoIP，检测是否在 CN
		code, err := r.geodb.GetCountryCode(ip)
		if err == nil && len(code) > 0 {
			if code != "CN" {
				return false
			}
		}
	}
	return true
}

// ShouldProxy 判断目标（域名或 IP）是否应该走代理
func (r *StandardResolver) ShouldProxy(target string) (bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, nil
	}

	// 1. 若是 IP
	if ip := net.ParseIP(target); ip != nil {
		// 是 Fake-IP 必然走代理
		if fakeip.IsFakeIP(ip) {
			return true, nil
		}
		// 是私有网段或直连 CIDR，直接直连
		if r.directIPs.Contains(ip) {
			return false, nil
		}
		// GeoIP 判定
		code, err := r.geodb.GetCountryCode(ip)
		if err == nil && len(code) > 0 {
			return code != "CN", nil
		}
		return false, nil
	}

	// 2. 若是域名
	d := fakeip.NormalizeDomain(target)
	if r.directDomains.Has(d) {
		return false, nil
	}
	if r.proxyDomains.Has(d) {
		return true, nil
	}

	// 未直接命中规则，通过解析出的 IP 进行判断
	ips, err := r.Resolve(context.Background(), d)
	if err != nil {
		return true, err // 解析失败默认走代理尝试
	}
	if len(ips) > 0 {
		return r.ShouldProxy(ips[0].String())
	}
	return false, nil
}

// LookupDomainByFakeIP 反查 Fake-IP
func (r *StandardResolver) LookupDomainByFakeIP(ip net.IP) (string, bool) {
	if r.fakeIPPool == nil {
		return "", false
	}
	return r.fakeIPPool.LookupDomainByIP(ip)
}

func (r *StandardResolver) LookupDomainByFakeIPStr(ipStr string) (string, bool) {
	if r.fakeIPPool == nil {
		return "", false
	}
	return r.fakeIPPool.LookupDomainByIPStr(ipStr)
}

func (r *StandardResolver) IsFakeIP(ip net.IP) bool {
	return fakeip.IsFakeIP(ip)
}

// ---- 动态热更新接口实现 ----

func (r *StandardResolver) UpdateDirectIPs(cidrs []string) int {
	return r.directIPs.Reset(cidrs)
}

func (r *StandardResolver) UpdateDirectDomains(domains []string) {
	r.directDomains.Reset(domains)
	for _, tld := range DefaultDirectTLDs {
		r.directDomains.Add(tld)
	}
}

func (r *StandardResolver) UpdateProxyDomains(domains []string) {
	r.proxyDomains.Reset(domains)
}

func (r *StandardResolver) LoadDirectDomainFile(filePath string) (int, error) {
	return r.directDomains.LoadFromFile(filePath)
}

func (r *StandardResolver) LoadProxyDomainFile(filePath string) (int, error) {
	return r.proxyDomains.LoadFromFile(filePath)
}

func (r *StandardResolver) ReloadGeoIPDatabase(filePath string) error {
	return r.geodb.Open(filePath)
}

func (r *StandardResolver) UpdateProxyAddress(proxyAddr string) error {
	r.mu.Lock()
	r.cfg.ProxyAddr = proxyAddr
	r.mu.Unlock()
	return r.initUpstreams()
}

func (r *StandardResolver) ClearCache() {
	r.cache.Clear()
}

func arpaToIPv4(arpa string) string {
	parts := strings.Split(strings.TrimSuffix(arpa, ".in-addr.arpa"), ".")
	if len(parts) != 4 {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])
}
