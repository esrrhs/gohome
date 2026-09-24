package matcher

import (
	"bufio"
	"io"
	"os"
	"strings"
	"sync"
)

// DomainTrie 是支持后缀匹配的高性能域名树
type DomainTrie struct {
	mu   sync.RWMutex
	root *trieNode
}

type trieNode struct {
	children map[string]*trieNode
	isEnd    bool
}

func newTrieNode() *trieNode {
	return &trieNode{
		children: make(map[string]*trieNode),
	}
}

// NewDomainTrie 创建一个新的域名匹配 Trie
func NewDomainTrie() *DomainTrie {
	return &DomainTrie{
		root: newTrieNode(),
	}
}

// Add 添加一个域名或域名后缀（例如 "google.com" 将匹配 "google.com" 和 "*.google.com"）
func (t *DomainTrie) Add(domain string) {
	d := strings.ToLower(strings.Trim(domain, "."))
	if d == "" {
		return
	}
	parts := strings.Split(d, ".")

	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	// 从顶级域名逆序向下插入 (e.g. ["com", "google"])
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" {
			continue
		}
		if _, ok := node.children[part]; !ok {
			node.children[part] = newTrieNode()
		}
		node = node.children[part]
	}
	node.isEnd = true
}

// Has 判断目标域名是否命中（自身匹配或其父域命中）
func (t *DomainTrie) Has(domain string) bool {
	d := strings.ToLower(strings.Trim(domain, "."))
	if d == "" {
		return false
	}
	parts := strings.Split(d, ".")

	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		child, ok := node.children[part]
		if !ok {
			return false
		}
		if child.isEnd {
			return true
		}
		node = child
	}
	return node.isEnd
}

// Reset 清空并重新加载一批域名
func (t *DomainTrie) Reset(domains []string) {
	newRoot := newTrieNode()
	for _, domain := range domains {
		d := strings.ToLower(strings.Trim(domain, "."))
		if d == "" {
			continue
		}
		parts := strings.Split(d, ".")
		node := newRoot
		for i := len(parts) - 1; i >= 0; i-- {
			part := parts[i]
			if part == "" {
				continue
			}
			if _, ok := node.children[part]; !ok {
				node.children[part] = newTrieNode()
			}
			node = node.children[part]
		}
		node.isEnd = true
	}

	t.mu.Lock()
	t.root = newRoot
	t.mu.Unlock()
}

// LoadFromReader 从 Reader 中按行加载域名（支持纯域名列表与 dnsmasq 格式: server=/example.com/...）
func (t *DomainTrie) LoadFromReader(r io.Reader) (int, error) {
	var domains []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 处理 dnsmasq 格式: server=/domain/114.114.114.114
		if strings.HasPrefix(line, "server=/") || strings.HasPrefix(line, "ipset=/") {
			parts := strings.Split(line, "/")
			if len(parts) >= 2 && parts[1] != "" {
				domains = append(domains, parts[1])
				continue
			}
		}
		// 普通域名行 (若带有空格或额外注释，取第一列)
		fields := strings.Fields(line)
		if len(fields) > 0 {
			domains = append(domains, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	for _, d := range domains {
		t.Add(d)
	}
	return len(domains), nil
}

// LoadFromFile 从文件加载域名规则
func (t *DomainTrie) LoadFromFile(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return t.LoadFromReader(f)
}
