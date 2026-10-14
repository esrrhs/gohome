package dns

import (
	"bufio"
	_ "embed"
	"strings"
)

//go:embed data/accelerated-domains.china.conf
var embeddedChinaDomainsFile string

// buildDefaultChinaMainDomains merges the curated seed list with the embedded
// felixonmars/dnsmasq-china-list accelerated-domains file.
func buildDefaultChinaMainDomains() []string {
	seen := make(map[string]struct{}, 120000)
	out := make([]string, 0, 120000)

	add := func(domain string) {
		d := strings.ToLower(strings.Trim(domain, "."))
		if d == "" {
			return
		}
		if _, ok := seen[d]; ok {
			return
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}

	for _, d := range defaultChinaMainDomainsSeed {
		add(d)
	}
	for _, d := range parseChinaDomainList(embeddedChinaDomainsFile) {
		add(d)
	}
	return out
}

// parseChinaDomainList parses dnsmasq china-list lines or plain domain lines.
func parseChinaDomainList(content string) []string {
	out := make([]string, 0, 110000)
	sc := bufio.NewScanner(strings.NewReader(content))
	// Some lines can be long; raise the limit beyond the default 64K.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "server=/"); ok {
			domain, _, found := strings.Cut(rest, "/")
			if !found || domain == "" {
				continue
			}
			line = domain
		}
		line = strings.ToLower(strings.Trim(line, "."))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// EmbeddedChinaDomainCount returns how many domains were parsed from the embed
// file (before dedup with the seed). Useful for tests and diagnostics.
func EmbeddedChinaDomainCount() int {
	return len(parseChinaDomainList(embeddedChinaDomainsFile))
}
