package matcher

import (
	"bufio"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
)

// IPNetList 管理一组 CIDR 子网，支持原子无锁热替换与快速包含性检查
type IPNetList struct {
	value atomic.Value // 存储 []*net.IPNet
}

// NewIPNetList 创建 IPNetList
func NewIPNetList() *IPNetList {
	l := &IPNetList{}
	l.value.Store([]*net.IPNet{})
	return l
}

// Contains 检查 IP 是否落在任意已知子网中
func (l *IPNetList) Contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	nets, ok := l.value.Load().([]*net.IPNet)
	if !ok || len(nets) == 0 {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ContainsStr 检查 IP 字符串是否在子网中
func (l *IPNetList) ContainsStr(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	return l.Contains(ip)
}

// Reset 全量更新 CIDR 列表（原子替换）
func (l *IPNetList) Reset(cidrs []string) int {
	var list []*net.IPNet
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			// 单个 IP 转换为 /32 或 /128
			if strings.Contains(c, ":") {
				c += "/128"
			} else {
				c += "/32"
			}
		}
		_, ipnet, err := net.ParseCIDR(c)
		if err == nil && ipnet != nil {
			list = append(list, ipnet)
		}
	}
	l.value.Store(list)
	return len(list)
}

// LoadFromReader 从 Reader 读取 CIDR 规则并原子更新
func (l *IPNetList) LoadFromReader(r io.Reader) (int, error) {
	var cidrs []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			cidrs = append(cidrs, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return l.Reset(cidrs), nil
}

// LoadFromFile 从文件读取并原子更新
func (l *IPNetList) LoadFromFile(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return l.LoadFromReader(f)
}
