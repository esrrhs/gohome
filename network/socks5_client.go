package network

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

/*
socks5_client 封装了 SOCKS5 协议的客户端功能（RFC 1928 CONNECT / UDP ASSOCIATE + RFC 1929）。
*/

const (
	socksVer5          = 5
	socks5AuthNone     = 0
	socks5UserPassAuth = 2
	socks5NoAcceptable = 0xff
	socks5Connect      = Socks5CmdConnect
	socks5UDPAssociate = Socks5CmdUDPAssociate
	Socks5AtypIP4      = 1
	Socks5AtypDomain   = 3
	Socks5AtypIP6      = 4
)

var socks5Errors = []string{
	"",
	"general failure",
	"connection forbidden",
	"network unreachable",
	"host unreachable",
	"connection refused",
	"TTL expired",
	"command not supported",
	"address type not supported",
}

func clearTCPDeadline(conn *net.TCPConn) {
	_ = conn.SetDeadline(time.Time{})
}

func setTCPDeadline(conn *net.TCPConn, timeoutms int) {
	if timeoutms > 0 {
		_ = conn.SetDeadline(time.Now().Add(time.Duration(timeoutms) * time.Millisecond))
	}
}

// Sock5Handshake performs RFC 1928 method negotiation and optional RFC 1929
// username/password authentication against a SOCKS5 proxy.
func Sock5Handshake(conn *net.TCPConn, timeoutms int, username string, password string) (err error) {
	if conn == nil {
		return errors.New("proxy: nil conn")
	}
	defer clearTCPDeadline(conn)

	needAuth := username != "" || password != ""
	if needAuth {
		if len(username) > 255 || len(password) > 255 {
			return errors.New("proxy: SOCKS5 username/password longer than 255 bytes")
		}
	}

	// RFC 1928 §3: VER, NMETHODS, METHODS
	var greeting []byte
	if needAuth {
		greeting = []byte{socksVer5, 1, socks5UserPassAuth}
	} else {
		greeting = []byte{socksVer5, 1, socks5AuthNone}
	}

	setTCPDeadline(conn, timeoutms)
	if _, err := conn.Write(greeting); err != nil {
		return errors.New("proxy: failed to write greeting to SOCKS5 proxy at " + conn.RemoteAddr().String() + ": " + err.Error())
	}

	var methodSel [2]byte
	setTCPDeadline(conn, timeoutms)
	if _, err := io.ReadFull(conn, methodSel[:]); err != nil {
		return errors.New("proxy: failed to read greeting from SOCKS5 proxy at " + conn.RemoteAddr().String() + ": " + err.Error())
	}
	if methodSel[0] != socksVer5 {
		return errors.New("proxy: SOCKS5 proxy at " + conn.RemoteAddr().String() + " has unexpected version " + strconv.Itoa(int(methodSel[0])))
	}
	if methodSel[1] == socks5NoAcceptable {
		return errors.New("proxy: SOCKS5 proxy at " + conn.RemoteAddr().String() + " rejected all authentication methods")
	}

	wantMethod := byte(socks5AuthNone)
	if needAuth {
		wantMethod = socks5UserPassAuth
	}
	if methodSel[1] != wantMethod {
		return fmt.Errorf("proxy: SOCKS5 proxy at %s selected method %d, want %d", conn.RemoteAddr().String(), methodSel[1], wantMethod)
	}

	if !needAuth {
		return nil
	}

	// RFC 1929: VER=0x01, ULEN, UNAME, PLEN, PASSWD
	authReq := make([]byte, 0, 3+len(username)+len(password))
	authReq = append(authReq, userAuthVersion, byte(len(username)))
	authReq = append(authReq, username...)
	authReq = append(authReq, byte(len(password)))
	authReq = append(authReq, password...)

	setTCPDeadline(conn, timeoutms)
	if _, err := conn.Write(authReq); err != nil {
		return errors.New("proxy: failed to write authentication to SOCKS5 proxy at " + conn.RemoteAddr().String() + ": " + err.Error())
	}

	var authResp [2]byte
	setTCPDeadline(conn, timeoutms)
	if _, err := io.ReadFull(conn, authResp[:]); err != nil {
		return errors.New("proxy: failed to read authentication from SOCKS5 proxy at " + conn.RemoteAddr().String() + ": " + err.Error())
	}
	if authResp[0] != userAuthVersion || authResp[1] != authSuccess {
		return errors.New("proxy: SOCKS5 proxy at " + conn.RemoteAddr().String() + " fail authentication")
	}
	return nil
}

// Sock5SetRequest sends an RFC 1928 CONNECT request and consumes the full reply
// (including BND.ADDR/BND.PORT) so the stream is ready for application data.
func Sock5SetRequest(conn *net.TCPConn, host string, port int, timeoutms int) (err error) {
	_, err = sock5SetRequestCmd(conn, socks5Connect, host, port, timeoutms)
	return err
}

// Sock5SetUDPRequest sends an RFC 1928 UDP ASSOCIATE request.
// host/port are the expected client UDP address (use "0.0.0.0"/0 if unknown).
// On success, bnd is the proxy UDP relay address ("host:port") to send datagrams to.
// The TCP control connection must be kept open for the association lifetime.
func Sock5SetUDPRequest(conn *net.TCPConn, host string, port int, timeoutms int) (bnd string, err error) {
	return sock5SetRequestCmd(conn, socks5UDPAssociate, host, port, timeoutms)
}

func sock5SetRequestCmd(conn *net.TCPConn, cmd byte, host string, port int, timeoutms int) (bnd string, err error) {
	if conn == nil {
		return "", errors.New("proxy: nil conn")
	}
	defer clearTCPDeadline(conn)

	if port < 0 || port > 65535 {
		return "", fmt.Errorf("proxy: invalid destination port %d", port)
	}

	addr, err := encodeSocksAddr(host, port)
	if err != nil {
		return "", errors.New("proxy: " + err.Error())
	}

	buf := make([]byte, 0, 3+len(addr))
	buf = append(buf, socksVer5, cmd, 0 /* RSV */)
	buf = append(buf, addr...)

	cmdName := "connect"
	if cmd == socks5UDPAssociate {
		cmdName = "udp associate"
	}

	setTCPDeadline(conn, timeoutms)
	if _, err = conn.Write(buf); err != nil {
		return "", errors.New("proxy: failed to write " + cmdName + " request to SOCKS5 proxy: " + err.Error())
	}

	// RFC 1928 §6 reply: VER REP RSV ATYP BND.ADDR BND.PORT
	var hdr [4]byte
	setTCPDeadline(conn, timeoutms)
	if _, err = io.ReadFull(conn, hdr[:]); err != nil {
		return "", errors.New("proxy: failed to read " + cmdName + " reply from SOCKS5 proxy: " + err.Error())
	}
	if hdr[0] != socksVer5 {
		return "", fmt.Errorf("proxy: SOCKS5 %s reply has unexpected version %d", cmdName, hdr[0])
	}

	setTCPDeadline(conn, timeoutms)
	bndHost, err := readSocksHost(conn, hdr[3])
	if err != nil {
		return "", fmt.Errorf("proxy: invalid reply: fail to read bnd host: %s", err)
	}
	setTCPDeadline(conn, timeoutms)
	bndPort, err := readSocksPort(conn)
	if err != nil {
		return "", fmt.Errorf("proxy: invalid reply: fail to read bnd port: %s", err)
	}

	rep := hdr[1]
	if rep != 0 {
		failure := "unknown error"
		if int(rep) < len(socks5Errors) && socks5Errors[rep] != "" {
			failure = socks5Errors[rep]
		} else {
			failure = fmt.Sprintf("reply code %d", rep)
		}
		return "", errors.New("proxy: SOCKS5 proxy failed to " + cmdName + ": " + failure)
	}
	return net.JoinHostPort(bndHost, strconv.Itoa(int(bndPort))), nil
}

func ntohs(data [2]byte) uint16 {
	return uint16(data[0])<<8 | uint16(data[1])
}

func readSocksIPv4Host(r io.Reader) (host string, err error) {
	var buf [4]byte
	_, err = io.ReadFull(r, buf[:])
	if err != nil {
		return
	}
	host = net.IP(buf[:]).String()
	return
}

func readSocksIPv6Host(r io.Reader) (host string, err error) {
	var buf [16]byte
	_, err = io.ReadFull(r, buf[:])
	if err != nil {
		return
	}
	host = net.IP(buf[:]).String()
	return
}

func readSocksDomainHost(r io.Reader) (host string, err error) {
	var lengthBuf [1]byte
	if _, err = io.ReadFull(r, lengthBuf[:]); err != nil {
		return
	}
	length := int(lengthBuf[0])
	if length == 0 {
		return "", errors.New("empty domain name")
	}
	buf := make([]byte, length)
	if _, err = io.ReadFull(r, buf); err != nil {
		return
	}
	host = string(buf)
	return
}

func readSocksHost(r io.Reader, hostType byte) (string, error) {
	switch hostType {
	case Socks5AtypIP4:
		return readSocksIPv4Host(r)
	case Socks5AtypIP6:
		return readSocksIPv6Host(r)
	case Socks5AtypDomain:
		return readSocksDomainHost(r)
	default:
		return "", fmt.Errorf("unknown address type 0x%02x", hostType)
	}
}

func readSocksPort(r io.Reader) (port uint16, err error) {
	var buf [2]byte
	_, err = io.ReadFull(r, buf[:])
	if err != nil {
		return
	}
	port = ntohs(buf)
	return
}
