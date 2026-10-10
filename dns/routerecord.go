package dns

import (
	"container/list"
	"sync"
	"time"
)

// RouteReason 描述一次分流最终走向的判定依据，是写入记录与上层读取
// 共同依赖的唯一语义契约：新增分流路径时只能复用这里的枚举，
// 不要在调用方各自定义字符串。
type RouteReason string

const (
	// RouteReasonDirectDomain 命中直连域名规则，仅向国内上游查询。
	RouteReasonDirectDomain RouteReason = "direct_domain"
	// RouteReasonProxyDomain 命中代理域名规则，仅向海外加密上游查询。
	RouteReasonProxyDomain RouteReason = "proxy_domain"
	// RouteReasonParallelDirect 竞速路径采纳了直连上游的境内/保留网段结果。
	RouteReasonParallelDirect RouteReason = "parallel_direct"
	// RouteReasonParallelRemote 竞速路径采纳了海外上游结果（直连失败、
	// 直连结果涉境外 IP，或超时后仅有远程应答）。
	RouteReasonParallelRemote RouteReason = "parallel_remote"
	// RouteReasonFakeIP 开启 Fake-IP 且域名未命中直连白名单，直接
	// 分配 Fake-IP，流量必然走代理。
	RouteReasonFakeIP RouteReason = "fakeip"
)

// DomainRoute 是单个域名最近一次分流结论的快照。
// 同一域名重复分流时就地更新（不重复堆积），UpdatedAt 反映最后判定时间。
type DomainRoute struct {
	Domain    string
	Proxy     bool
	Reason    RouteReason
	UpdatedAt time.Time
}

// DefaultRouteRecordCapacity 分流记录默认容量（按域名去重，LRU 淘汰）。
const DefaultRouteRecordCapacity = 1000

// routeRecorder 记录分流过的域名，按域名去重、LRU 淘汰、并发安全。
// capacity <= 0 时 NewResolver 不会创建记录器，recordRoute 为空操作，
// 表示上层不需要该功能时零开销。
type routeRecorder struct {
	capacity int

	mu    sync.Mutex
	lru   *list.List // Front 最近判定，element.Value = *DomainRoute
	index map[string]*list.Element
}

func newRouteRecorder(capacity int) *routeRecorder {
	if capacity <= 0 {
		return nil
	}
	return &routeRecorder{
		capacity: capacity,
		lru:      list.New(),
		index:    make(map[string]*list.Element),
	}
}

// record 在唯一的分流判定点写入/更新一个域名的结论。
func (rr *routeRecorder) record(domain string, proxy bool, reason RouteReason) {
	if rr == nil {
		return
	}
	now := time.Now()
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if el, ok := rr.index[domain]; ok {
		item := el.Value.(*DomainRoute)
		item.Proxy = proxy
		item.Reason = reason
		item.UpdatedAt = now
		rr.lru.MoveToFront(el)
		return
	}
	el := rr.lru.PushFront(&DomainRoute{
		Domain:    domain,
		Proxy:     proxy,
		Reason:    reason,
		UpdatedAt: now,
	})
	rr.index[domain] = el
	for rr.lru.Len() > rr.capacity {
		oldest := rr.lru.Back()
		if oldest == nil {
			break
		}
		rr.lru.Remove(oldest)
		delete(rr.index, oldest.Value.(*DomainRoute).Domain)
	}
}

// snapshot 返回按最近判定时间倒序排列的副本，调用方可自由修改。
func (rr *routeRecorder) snapshot() []DomainRoute {
	if rr == nil {
		return nil
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	out := make([]DomainRoute, 0, rr.lru.Len())
	for el := rr.lru.Front(); el != nil; el = el.Next() {
		item := el.Value.(*DomainRoute)
		out = append(out, *item)
	}
	return out
}

// get 返回单个域名最近一次分流结论。
func (rr *routeRecorder) get(domain string) (DomainRoute, bool) {
	if rr == nil {
		return DomainRoute{}, false
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	el, ok := rr.index[domain]
	if !ok {
		return DomainRoute{}, false
	}
	return *el.Value.(*DomainRoute), true
}

func (rr *routeRecorder) clear() {
	if rr == nil {
		return
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.lru.Init()
	rr.index = make(map[string]*list.Element)
}

func (rr *routeRecorder) len() int {
	if rr == nil {
		return 0
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.lru.Len()
}
