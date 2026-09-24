package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

/*
socks5_server 封装了 SOCKS5 协议的握手和请求处理（RFC 1928 CONNECT / UDP ASSOCIATE + RFC 1929）。
*/

var (
	errAddrType = errors.New("socks addr type not supported")
	errVer      = errors.New("socks version not supported")
	errMethod   = errors.New("socks no acceptable authentication method")
	errAuth     = errors.New("socks authentication failed")
	errCmd      = errors.New("socks command not supported")
	errUDPFrag  = errors.New("socks udp fragmentation not supported")
)

const (
	Socks5CmdConnect      = 1
	Socks5CmdUDPAssociate = 3
	NoAuth                = uint8(0)
	userAuthVersion       = uint8(1)
	UserPassAuth          = uint8(2)
	authSuccess           = uint8(0)
	authFailure           = uint8(1)
)

func methodOffered(methods []byte, want byte) bool {
	for _, m := range methods {
		if m == want {
			return true
		}
	}
	return false
}

// Sock5HandshakeBy performs RFC 1928 method selection and optional RFC 1929 auth.
// When username/password are both empty, NoAuth is required; otherwise UserPassAuth.
func Sock5HandshakeBy(conn io.ReadWriter, username string, password string) (err error) {
	if conn == nil {
		return errors.New("socks: nil conn")
	}

	// RFC 1928 §3: read VER, NMETHODS, then exactly NMETHODS method bytes.
	var head [2]byte
	if _, err = io.ReadFull(conn, head[:]); err != nil {
		return err
	}
	if head[0] != socksVer5 {
		return errVer
	}
	nmethod := int(head[1])
	methods := make([]byte, nmethod)
	if nmethod > 0 {
		if _, err = io.ReadFull(conn, methods); err != nil {
			return err
		}
	}

	needAuth := username != "" || password != ""
	want := NoAuth
	if needAuth {
		want = UserPassAuth
	}

	if !methodOffered(methods, want) {
		// RFC 1928: X'FF' = NO ACCEPTABLE METHODS
		_, _ = conn.Write([]byte{socksVer5, socks5NoAcceptable})
		return errMethod
	}

	if !needAuth {
		_, err = conn.Write([]byte{socksVer5, NoAuth})
		return err
	}

	if _, err = conn.Write([]byte{socksVer5, UserPassAuth}); err != nil {
		return err
	}

	// RFC 1929 sub-negotiation
	var authHead [2]byte
	if _, err = io.ReadFull(conn, authHead[:]); err != nil {
		return err
	}
	if authHead[0] != userAuthVersion {
		return fmt.Errorf("unsupported auth version: %v", authHead[0])
	}

	userLen := int(authHead[1])
	user := make([]byte, userLen)
	if userLen > 0 {
		if _, err = io.ReadFull(conn, user); err != nil {
			return err
		}
	}

	var passLenBuf [1]byte
	if _, err = io.ReadFull(conn, passLenBuf[:]); err != nil {
		return err
	}
	passLen := int(passLenBuf[0])
	pass := make([]byte, passLen)
	if passLen > 0 {
		if _, err = io.ReadFull(conn, pass); err != nil {
			return err
		}
	}

	if username == string(user) && password == string(pass) {
		_, err = conn.Write([]byte{userAuthVersion, authSuccess})
		return err
	}
	_, _ = conn.Write([]byte{userAuthVersion, authFailure})
	return errAuth
}

// Sock5GetRequest parses an RFC 1928 request.
// Accepts CMD=CONNECT (1) and CMD=UDP ASSOCIATE (3); BIND returns errCmd.
// cmd is Socks5CmdConnect or Socks5CmdUDPAssociate.
func Sock5GetRequest(conn io.ReadWriter) (cmd byte, rawaddr []byte, host string, err error) {
	if conn == nil {
		return 0, nil, "", errors.New("socks: nil conn")
	}

	// VER CMD RSV ATYP
	var hdr [4]byte
	if _, err = io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	if hdr[0] != socksVer5 {
		err = errVer
		return
	}
	cmd = hdr[1]
	if cmd != Socks5CmdConnect && cmd != Socks5CmdUDPAssociate {
		err = errCmd
		return
	}
	// hdr[2] is RSV; ignore per RFC (should be 0x00)

	atyp := hdr[3]
	var addr []byte
	switch atyp {
	case Socks5AtypIP4:
		addr = make([]byte, net.IPv4len)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return
		}
		host = net.IP(addr).String()
	case Socks5AtypIP6:
		addr = make([]byte, net.IPv6len)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return
		}
		host = net.IP(addr).String()
	case Socks5AtypDomain:
		var alen [1]byte
		if _, err = io.ReadFull(conn, alen[:]); err != nil {
			return
		}
		if alen[0] == 0 {
			err = errors.New("socks empty domain name")
			return
		}
		addr = make([]byte, int(alen[0]))
		if _, err = io.ReadFull(conn, addr); err != nil {
			return
		}
		host = string(addr)
	default:
		err = errAddrType
		return
	}

	var portBuf [2]byte
	if _, err = io.ReadFull(conn, portBuf[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf[:])
	host = net.JoinHostPort(host, strconv.Itoa(int(port)))

	// rawaddr = ATYP + ADDR + PORT (common for shadowsocks-style callers)
	rawaddr = make([]byte, 0, 1+len(addr)+2)
	if atyp == Socks5AtypDomain {
		rawaddr = append(rawaddr, atyp, byte(len(addr)))
		rawaddr = append(rawaddr, addr...)
	} else {
		rawaddr = append(rawaddr, atyp)
		rawaddr = append(rawaddr, addr...)
	}
	rawaddr = append(rawaddr, portBuf[:]...)
	return
}

// Sock5SendConnectReply writes an RFC 1928 reply (CONNECT or UDP ASSOCIATE).
// rep is the REP field (0 = succeeded). bnd should be "host:port"; on parse
// failure a zero IPv4 bind address is sent.
// For UDP ASSOCIATE success, bnd is the UDP relay address the client must use.
func Sock5SendConnectReply(conn io.Writer, rep byte, bnd string) error {
	if conn == nil {
		return errors.New("socks: nil conn")
	}

	reply := []byte{socksVer5, rep, 0 /* RSV */}
	host, portStr, err := net.SplitHostPort(bnd)
	if err != nil {
		reply = append(reply, Socks5AtypIP4, 0, 0, 0, 0, 0, 0)
		_, werr := conn.Write(reply)
		return werr
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		reply = append(reply, Socks5AtypIP4, 0, 0, 0, 0, 0, 0)
		_, werr := conn.Write(reply)
		return werr
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			reply = append(reply, Socks5AtypIP4)
			reply = append(reply, ip4...)
		} else {
			ip6 := ip.To16()
			reply = append(reply, Socks5AtypIP6)
			reply = append(reply, ip6...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			reply = append(reply, Socks5AtypIP4, 0, 0, 0, 0, 0, 0)
			_, werr := conn.Write(reply)
			return werr
		}
		reply = append(reply, Socks5AtypDomain, byte(len(host)))
		reply = append(reply, host...)
	}
	reply = append(reply, byte(port>>8), byte(port))
	_, err = conn.Write(reply)
	return err
}

// Sock5PackUDP builds an RFC 1928 UDP request datagram:
// RSV(2) | FRAG(1) | ATYP | DST.ADDR | DST.PORT | DATA.
// Fragmentation is not supported (FRAG is always 0).
func Sock5PackUDP(host string, port int, data []byte) ([]byte, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("socks: invalid udp destination port %d", port)
	}
	addr, err := encodeSocksAddr(host, port)
	if err != nil {
		return nil, err
	}
	pkt := make([]byte, 0, 3+len(addr)+len(data))
	pkt = append(pkt, 0, 0, 0) // RSV + FRAG
	pkt = append(pkt, addr...)
	pkt = append(pkt, data...)
	return pkt, nil
}

// Sock5UnpackUDP parses an RFC 1928 UDP request datagram.
// Fragmented packets (FRAG != 0) are rejected.
func Sock5UnpackUDP(pkt []byte) (host string, port int, data []byte, err error) {
	if len(pkt) < 4 {
		return "", 0, nil, errors.New("socks: udp packet too short")
	}
	if pkt[2] != 0 {
		return "", 0, nil, errUDPFrag
	}
	host, port, rest, err := decodeSocksAddr(pkt[3:])
	if err != nil {
		return "", 0, nil, err
	}
	return host, port, rest, nil
}

// encodeSocksAddr encodes ATYP + ADDR + PORT for host:port.
func encodeSocksAddr(host string, port int) ([]byte, error) {
	buf := make([]byte, 0, 1+255+2)
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			buf = append(buf, Socks5AtypIP4)
			buf = append(buf, ip4...)
		} else {
			ip6 := ip.To16()
			if ip6 == nil {
				return nil, errors.New("socks: invalid IP address: " + host)
			}
			buf = append(buf, Socks5AtypIP6)
			buf = append(buf, ip6...)
		}
	} else {
		if len(host) == 0 {
			return nil, errors.New("socks: empty destination hostname")
		}
		if len(host) > 255 {
			return nil, errors.New("socks: destination hostname too long: " + host)
		}
		buf = append(buf, Socks5AtypDomain, byte(len(host)))
		buf = append(buf, host...)
	}
	buf = append(buf, byte(port>>8), byte(port))
	return buf, nil
}

// decodeSocksAddr parses ATYP + ADDR + PORT from b, returning host, port and leftover bytes.
func decodeSocksAddr(b []byte) (host string, port int, rest []byte, err error) {
	if len(b) < 1 {
		return "", 0, nil, errors.New("socks: truncated address")
	}
	atyp := b[0]
	off := 1
	switch atyp {
	case Socks5AtypIP4:
		if len(b) < off+net.IPv4len+2 {
			return "", 0, nil, errors.New("socks: truncated ipv4 address")
		}
		host = net.IP(b[off : off+net.IPv4len]).String()
		off += net.IPv4len
	case Socks5AtypIP6:
		if len(b) < off+net.IPv6len+2 {
			return "", 0, nil, errors.New("socks: truncated ipv6 address")
		}
		host = net.IP(b[off : off+net.IPv6len]).String()
		off += net.IPv6len
	case Socks5AtypDomain:
		if len(b) < off+1 {
			return "", 0, nil, errors.New("socks: truncated domain length")
		}
		n := int(b[off])
		off++
		if n == 0 {
			return "", 0, nil, errors.New("socks: empty domain name")
		}
		if len(b) < off+n+2 {
			return "", 0, nil, errors.New("socks: truncated domain address")
		}
		host = string(b[off : off+n])
		off += n
	default:
		return "", 0, nil, errAddrType
	}
	port = int(binary.BigEndian.Uint16(b[off : off+2]))
	off += 2
	return host, port, b[off:], nil
}
