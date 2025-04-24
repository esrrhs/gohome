package network

import (
	"errors"
	"github.com/esrrhs/gohome/common"
	"io"
	"strings"
	"syscall"
)

/*
Conn 提供网络连接的抽象和具体实现，支持多种网络协议的连接管理。

该包定义了 Conn 接口，表示一个通用网络连接，包含连接的基本操作方法，例如读取、写入、建立连接和监听等。

当前支持的协议包括：

- TCP
- UDP
- RUDP
- RICMP
- KCP
- QUIC
- RHTTP
*/

// Conn 接口定义了网络连接的基本操作。
type Conn interface {
	io.ReadWriteCloser

	Name() string

	Info() string

	Dial(dst string) (Conn, error)

	Listen(dst string) (Conn, error)
	Accept() (Conn, error)
}

// NewConn 创建一个新的网络连接，支持的协议包括 TCP, UDP, RUDP, RICMP, KCP, QUIC 及 RHTTP。
func NewConn(proto string) (Conn, error) {
	proto = strings.ToLower(proto)
	if proto == "tcp" {
		return &TcpConn{}, nil
	} else if proto == "udp" {
		return &UdpConn{}, nil
	} else if proto == "rudp" {
		return &RudpConn{}, nil
	} else if proto == "ricmp" {
		return &RicmpConn{id: common.UniqueId()}, nil
	} else if proto == "kcp" {
		return &KcpConn{}, nil
	} else if proto == "quic" {
		return &QuicConn{}, nil
	} else if proto == "rhttp" {
		return &RhttpConn{}, nil
	}
	return nil, errors.New("undefined proto " + proto)
}

// SupportReliableProtos 返回支持的可靠协议列表。
func SupportReliableProtos() []string {
	ret := make([]string, 0)
	ret = append(ret, "tcp")
	ret = append(ret, "rudp")
	ret = append(ret, "ricmp")
	ret = append(ret, "kcp")
	ret = append(ret, "quic")
	ret = append(ret, "rhttp")
	return ret
}

// SupportProtos 返回支持的所有协议列表，包括可靠和不可靠的协议。
func SupportProtos() []string {
	ret := make([]string, 0)
	ret = append(ret, SupportReliableProtos()...)
	ret = append(ret, "udp")
	return ret
}

// HasReliableProto 检查指定的协议是否为支持的可靠协议。
func HasReliableProto(proto string) bool {
	return common.HasString(SupportReliableProtos(), proto)
}

// HasProto 检查指定的协议是否为支持的协议。
func HasProto(proto string) bool {
	return common.HasString(SupportProtos(), proto)
}

// gControlOnConnSetup 用于注册连接设置的控制函数。
var gControlOnConnSetup func(network, address string, c syscall.RawConn) error

// RegisterDialerController 注册一个控制函数，允许在连接设置时执行额外操作。
func RegisterDialerController(fn func(network, address string, c syscall.RawConn) error) {
	gControlOnConnSetup = fn
}
