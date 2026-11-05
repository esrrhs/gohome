package dns

import (
	"context"
	"testing"
)

func TestGetRegionPreset_Builtins(t *testing.T) {
	expectedCodes := []string{"CN", "IR", "RU", "TR", "MM", "PK", "VN", "EG", "AE", "SA", "IN", "NONE"}

	for _, code := range expectedCodes {
		p, ok := GetRegionPreset(code)
		if !ok {
			t.Fatalf("expected preset for %s to exist", code)
		}
		if p.Code != code {
			t.Fatalf("expected code %s, got %s", code, p.Code)
		}
		if code == "NONE" {
			if len(p.DirectTLDs) != 0 || len(p.DirectDomains) != 0 {
				t.Fatalf("expected empty rules for NONE preset")
			}
			continue
		}

		if len(p.DirectTLDs) == 0 {
			t.Fatalf("preset %s missing DirectTLDs", code)
		}
		if len(p.DirectUpstreams) == 0 {
			t.Fatalf("preset %s missing DirectUpstreams", code)
		}
		if len(p.DirectDomains) == 0 {
			t.Fatalf("preset %s missing DirectDomains", code)
		}
	}

	// 验证大小写无关
	if _, ok := GetRegionPreset("ir"); !ok {
		t.Fatalf("expected 'ir' lower case lookup to succeed")
	}
	if _, ok := GetRegionPreset("Global"); !ok {
		t.Fatalf("expected 'Global' lookup to alias to NONE")
	}
}

func TestListRegionPresets(t *testing.T) {
	list := ListRegionPresets()
	if len(list) < 12 {
		t.Fatalf("expected at least 12 presets, got %d", len(list))
	}
	// 首项应该是 CN
	if list[0].Code != "CN" {
		t.Fatalf("expected first preset to be CN, got %s", list[0].Code)
	}
}

func TestRegisterRegionPreset(t *testing.T) {
	custom := RegionPreset{
		Code:            "XX",
		Name:            "Test Land",
		DirectTLDs:      []string{"xx"},
		DirectUpstreams: []string{"1.2.3.4:53"},
		DirectDomains:   []string{"test.xx"},
	}
	RegisterRegionPreset(custom)

	p, ok := GetRegionPreset("xx")
	if !ok || p.Code != "XX" || p.Name != "Test Land" {
		t.Fatalf("custom preset registration failed")
	}
}

func TestResolver_PresetIran(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RegionPreset = "IR"
	cfg.DirectUpstreams = nil // 让其自动继承伊朗预设 DNS

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	if r.cfg.RegionCode != "IR" {
		t.Fatalf("expected RegionCode IR, got %s", r.cfg.RegionCode)
	}
	if len(r.cfg.DirectUpstreams) == 0 || r.cfg.DirectUpstreams[0] != "178.22.122.100:53" {
		t.Fatalf("expected Iran direct upstream, got %v", r.cfg.DirectUpstreams)
	}

	// 应当命中伊朗直连域名，不走代理
	for _, domain := range []string{"digikala.com", "snapp.ir", "aparat.com", "test.ir"} {
		if !r.directDomains.Has(domain) {
			t.Fatalf("expected directDomains to contain %s", domain)
		}
		proxy, err := r.ShouldProxy(domain)
		if err != nil {
			t.Fatalf("ShouldProxy(%s) failed: %v", domain, err)
		}
		if proxy {
			t.Fatalf("expected %s to be direct (ShouldProxy=false)", domain)
		}
	}

	// 伊朗预设下不应该含有中国的私有大厂域名与顶级域
	if r.directDomains.Has("baidu.com") {
		t.Fatalf("expected baidu.com NOT to be in directDomains under Iran preset")
	}
	if r.directDomains.Has("cn") {
		t.Fatalf("expected .cn NOT to be in directDomains under Iran preset")
	}

	// 测试 UpdateDirectDomains 后仍保留伊朗规则
	r.UpdateDirectDomains([]string{"custom-ir-service.local"})
	if !r.directDomains.Has("digikala.com") {
		t.Fatalf("expected digikala.com to stay in directDomains after UpdateDirectDomains")
	}
	if !r.directDomains.Has("custom-ir-service.local") {
		t.Fatalf("expected custom-ir-service.local to be in directDomains")
	}
}

func TestResolver_PresetRussia(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RegionPreset = "RU"
	cfg.DirectUpstreams = nil

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	if r.cfg.RegionCode != "RU" {
		t.Fatalf("expected RegionCode RU, got %s", r.cfg.RegionCode)
	}
	if len(r.cfg.DirectUpstreams) == 0 || r.cfg.DirectUpstreams[0] != "77.88.8.8:53" {
		t.Fatalf("expected Yandex direct upstream, got %v", r.cfg.DirectUpstreams)
	}

	for _, domain := range []string{"yandex.ru", "vk.com", "gosuslugi.ru", "test.ru"} {
		proxy, err := r.ShouldProxy(domain)
		if err != nil {
			t.Fatalf("ShouldProxy(%s) failed: %v", domain, err)
		}
		if proxy {
			t.Fatalf("expected %s to be direct", domain)
		}
	}
}

func TestResolver_RegionCodeAutoInfersPreset(t *testing.T) {
	cfg := DefaultConfig()
	// 用户只设置了 RegionCode: "IR"，未显式指定 RegionPreset
	cfg.RegionCode = "IR"
	cfg.DirectUpstreams = nil

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	if r.cfg.RegionPreset != "IR" {
		t.Fatalf("expected auto inferred preset IR, got %s", r.cfg.RegionPreset)
	}

	proxy, _ := r.ShouldProxy("digikala.com")
	if proxy {
		t.Fatalf("expected digikala.com to be direct when RegionCode is IR")
	}
}

func TestResolver_PresetNoneAndDisableBuiltins(t *testing.T) {
	// 场景 1: RegionPreset = "NONE"
	cfg1 := DefaultConfig()
	cfg1.RegionPreset = "NONE"

	r1, err := NewResolver(cfg1)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r1.Close()

	if r1.directDomains.Has("baidu.com") || r1.directDomains.Has("cn") {
		t.Fatalf("expected no builtin domains in r1 under NONE preset")
	}

	// 场景 2: DisableBuiltinDirectDomains = true
	cfg2 := DefaultConfig()
	cfg2.DisableBuiltinDirectDomains = true

	r2, err := NewResolver(cfg2)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r2.Close()

	if r2.directDomains.Has("baidu.com") || r2.directDomains.Has("cn") {
		t.Fatalf("expected no builtin domains in r2 when DisableBuiltinDirectDomains=true")
	}
}

func TestResolver_Close(t *testing.T) {
	cfg := DefaultConfig()
	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	_ = context.Background()
}
