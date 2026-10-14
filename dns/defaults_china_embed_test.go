package dns

import "testing"

func TestDefaultChinaMainDomainsIncludesChinaList(t *testing.T) {
	if EmbeddedChinaDomainCount() < 100000 {
		t.Fatalf("embedded china list too small: %d", EmbeddedChinaDomainCount())
	}
	if len(DefaultChinaMainDomains) < 100000 {
		t.Fatalf("DefaultChinaMainDomains too small: %d", len(DefaultChinaMainDomains))
	}

	need := []string{
		"baidu.com",
		"douyin.com",
		"xiaohongshu.com",
		"xhscdn.com",
		"xhscdn.net",
		"xiaohongshu.net",
	}
	have := make(map[string]struct{}, len(DefaultChinaMainDomains))
	for _, d := range DefaultChinaMainDomains {
		have[d] = struct{}{}
	}
	for _, d := range need {
		if _, ok := have[d]; !ok {
			t.Errorf("missing built-in direct domain %s", d)
		}
	}
}

func TestParseChinaDomainListDnsmasq(t *testing.T) {
	in := "# comment\nserver=/xiaohongshu.com/114.114.114.114\nserver=/xhscdn.com/114.114.114.114\nexample.org\n"
	got := parseChinaDomainList(in)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "xiaohongshu.com" || got[1] != "xhscdn.com" || got[2] != "example.org" {
		t.Fatalf("unexpected parse: %v", got)
	}
}
