package dns

import "testing"

func TestRouteRecorder_NilSafe(t *testing.T) {
	var rr *routeRecorder // capacity <= 0 时 NewResolver 得到 nil
	rr.record("a.test", false, RouteReasonDirectDomain)
	if got := rr.snapshot(); got != nil {
		t.Fatalf("nil recorder snapshot should be nil, got %v", got)
	}
	if _, ok := rr.get("a.test"); ok {
		t.Fatal("nil recorder get must report missing")
	}
	rr.clear()
	if rr.len() != 0 {
		t.Fatalf("nil recorder len = %d, want 0", rr.len())
	}
}

func TestRouteRecorder_NewWithNonPositiveCapacity(t *testing.T) {
	if rr := newRouteRecorder(0); rr != nil {
		t.Fatalf("capacity 0 should yield nil recorder, got %+v", rr)
	}
	if rr := newRouteRecorder(-1); rr != nil {
		t.Fatalf("negative capacity should yield nil recorder, got %+v", rr)
	}
}

func TestRouteRecorder_RecordAndSnapshotOrder(t *testing.T) {
	rr := newRouteRecorder(10)
	rr.record("a.test", false, RouteReasonDirectDomain)
	rr.record("b.test", true, RouteReasonProxyDomain)
	rr.record("c.test", false, RouteReasonParallelDirect)

	got := rr.snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %v", len(got), got)
	}
	// 最近判定的排在最前
	wantOrder := []string{"c.test", "b.test", "a.test"}
	for i, want := range wantOrder {
		if got[i].Domain != want {
			t.Fatalf("position %d = %s, want %s (full: %v)", i, got[i].Domain, want, got)
		}
	}
	if got[1].Proxy != true || got[1].Reason != RouteReasonProxyDomain {
		t.Fatalf("b.test record mismatch: %+v", got[1])
	}
	if got[1].UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt must be set")
	}
}

func TestRouteRecorder_DedupUpdatesInPlace(t *testing.T) {
	rr := newRouteRecorder(10)
	rr.record("flip.test", false, RouteReasonParallelDirect)
	rr.record("other.test", true, RouteReasonFakeIP)
	// 同一域名再次分流且结论反转：必须就地更新而不是新增，
	// 并因最近被访问而排到最前。
	rr.record("flip.test", true, RouteReasonParallelRemote)

	if rr.len() != 2 {
		t.Fatalf("len = %d, want 2 (dedup)", rr.len())
	}
	got := rr.snapshot()
	if got[0].Domain != "flip.test" || !got[0].Proxy || got[0].Reason != RouteReasonParallelRemote {
		t.Fatalf("updated record should be front with new verdict: %+v", got[0])
	}
	one, ok := rr.get("flip.test")
	if !ok || !one.Proxy || one.Reason != RouteReasonParallelRemote {
		t.Fatalf("get after update mismatch: %+v ok=%v", one, ok)
	}
}

func TestRouteRecorder_LRUEviction(t *testing.T) {
	rr := newRouteRecorder(2)
	rr.record("a.test", false, RouteReasonDirectDomain)
	rr.record("b.test", false, RouteReasonDirectDomain)
	// a 再次发生分流、刷新为最近使用，使 b 成为最久未使用。
	// 注意 get 是只读操作，刻意不刷新淘汰位置：快照顺序只反映判定时间，
	// 不能被上层读取行为污染。
	rr.record("a.test", false, RouteReasonDirectDomain)
	rr.record("c.test", true, RouteReasonProxyDomain)

	if rr.len() != 2 {
		t.Fatalf("len = %d, want 2", rr.len())
	}
	if _, ok := rr.get("b.test"); ok {
		t.Fatal("b.test should have been evicted as LRU")
	}
	if _, ok := rr.get("a.test"); !ok {
		t.Fatal("a.test (recently touched) must survive")
	}
	if _, ok := rr.get("c.test"); !ok {
		t.Fatal("c.test must survive")
	}
}

func TestRouteRecorder_Clear(t *testing.T) {
	rr := newRouteRecorder(5)
	rr.record("a.test", false, RouteReasonDirectDomain)
	rr.clear()
	if rr.len() != 0 || len(rr.snapshot()) != 0 {
		t.Fatal("recorder should be empty after clear")
	}
	// 清空后仍可继续写入
	rr.record("b.test", true, RouteReasonFakeIP)
	if rr.len() != 1 {
		t.Fatalf("len after re-record = %d, want 1", rr.len())
	}
}

func TestRouteRecorder_SnapshotIsolation(t *testing.T) {
	rr := newRouteRecorder(5)
	rr.record("a.test", false, RouteReasonDirectDomain)
	got := rr.snapshot()
	got[0].Domain = "mutated.test"
	got[0].Proxy = true

	again, ok := rr.get("a.test")
	if !ok {
		t.Fatal("internal record lost after caller mutated snapshot")
	}
	if again.Domain != "a.test" || again.Proxy {
		t.Fatalf("snapshot must be a copy, internal state mutated: %+v", again)
	}
}
