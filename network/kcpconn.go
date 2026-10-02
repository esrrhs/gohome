package network

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/xtaci/kcp-go"
)

/*
KcpConn 实现了基于 KCP 协议的Conn。
UDPSession 已是 stream 模式 net.Conn，直接读写（不再叠 smux）。
*/

// KcpConfig tunes the KCP forward error correction. DataShards data packets
// are protected by ParityShards parity packets; the receiver can recover as
// many lost packets as there are parity shards without retransmission.
// FEC changes the on-the-wire packet framing, so both peers must configure
// identical shard counts: a FEC-enabled endpoint cannot talk to a FEC-less
// one. Both shard counts default to 0 (FEC disabled, legacy wire format).
// Reed-Solomon allows at most 256 shards in total (data + parity).
type KcpConfig struct {
	DataShards   int
	ParityShards int
}

// DefaultKcpConfig keeps FEC disabled (0,0) so upgraded peers stay
// interoperable with older binaries. Opt in explicitly via SetConfig on
// both ends, e.g. 10 data + 3 parity shards (~30% redundancy) for weak
// networks.
func DefaultKcpConfig() *KcpConfig {
	return &KcpConfig{
		DataShards:   0,
		ParityShards: 0,
	}
}

type KcpConn struct {
	sess     *kcp.UDPSession
	listener *kcp.Listener

	config *KcpConfig
	cfgMu  sync.RWMutex

	dialMu    sync.Mutex
	cancel    context.CancelFunc
	dialOwned io.Closer // in-flight resource; Close() takes and closes it to abort Dial
}

func (c *KcpConn) checkConfig() {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultKcpConfig()
	}
	cfg := c.config
	// Reject parity without data and Reed-Solomon shard overflow; fall
	// back to the safe default (FEC off) instead of risking a kcp-go error
	// or silently malformed framing.
	if cfg.DataShards <= 0 && cfg.ParityShards > 0 {
		c.config = DefaultKcpConfig()
	} else if cfg.DataShards+cfg.ParityShards > 256 {
		c.config = DefaultKcpConfig()
	}
	c.cfgMu.Unlock()
}

func (c *KcpConn) SetConfig(config *KcpConfig) {
	c.cfgMu.Lock()
	c.config = config
	c.cfgMu.Unlock()
}

func (c *KcpConn) GetConfig() *KcpConfig {
	c.checkConfig()
	c.cfgMu.RLock()
	cfg := c.config
	c.cfgMu.RUnlock()
	return cfg
}

// fecParams returns validated FEC shard counts (0,0 disables FEC and keeps
// the legacy wire format). Both counts must be positive and their sum must
// not exceed Reed-Solomon's 256-shard limit.
func (cfg *KcpConfig) fecParams() (data, parity int) {
	if cfg == nil || cfg.DataShards <= 0 || cfg.ParityShards <= 0 {
		return 0, 0
	}
	if cfg.DataShards+cfg.ParityShards > 256 {
		return 0, 0
	}
	return cfg.DataShards, cfg.ParityShards
}

func (c *KcpConn) Name() string {
	return "kcp"
}

func (c *KcpConn) Read(p []byte) (n int, err error) {
	if c.sess != nil {
		return c.sess.Read(p)
	}
	return 0, errors.New("empty conn")
}

func (c *KcpConn) Write(p []byte) (n int, err error) {
	if c.sess != nil {
		return c.sess.Write(p)
	}
	return 0, errors.New("empty conn")
}

func (c *KcpConn) Close() error {
	c.dialMu.Lock()
	cancel := c.cancel
	owned := c.dialOwned
	c.cancel = nil
	c.dialOwned = nil
	// Cancel while holding dialMu so finishDial observing ctx.Err() cannot
	// race ahead of cancel and hand out a connection that Close is aborting.
	if cancel != nil {
		cancel()
	}
	c.dialMu.Unlock()
	if owned != nil {
		owned.Close()
	}

	if c.sess != nil {
		return c.sess.Close()
	} else if c.listener != nil {
		return c.listener.Close()
	}
	return nil
}

func (c *KcpConn) Info() string {
	if c.sess != nil {
		return c.sess.LocalAddr().String() + "<--kcp-->" + c.sess.RemoteAddr().String()
	}
	if c.listener != nil {
		return "kcp--" + c.listener.Addr().String()
	}
	return "empty kcp conn"
}

func (c *KcpConn) setDialOwned(closer io.Closer) {
	c.dialMu.Lock()
	c.dialOwned = closer
	c.dialMu.Unlock()
}

// finishDial clears dial abort state. Succeeds only if ctx is still live and
// we still own closer (Close has not stolen dialOwned).
func (c *KcpConn) finishDial(ctx context.Context, closer io.Closer) bool {
	c.dialMu.Lock()
	stillOwn := c.dialOwned == closer
	canceled := ctx.Err() != nil || !stillOwn
	c.cancel = nil
	c.dialOwned = nil
	c.dialMu.Unlock()
	if canceled {
		if closer != nil && stillOwn {
			closer.Close()
		}
		return false
	}
	return true
}

func (c *KcpConn) Dial(dst string) (Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c.dialMu.Lock()
	c.cancel = cancel
	c.dialOwned = nil
	c.dialMu.Unlock()
	defer func() {
		c.dialMu.Lock()
		owned := c.dialOwned
		c.cancel = nil
		c.dialOwned = nil
		c.dialMu.Unlock()
		if owned != nil {
			owned.Close()
		}
		cancel()
	}()

	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}
	// Resolve before binding so cancel has a chance before NewConn.
	if _, err := net.ResolveUDPAddr("udp", dst); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	var lc net.ListenConfig
	if gControlOnConnSetup != nil {
		lc.Control = gControlOnConnSetup
	}

	laddr := &net.UDPAddr{}
	pconn, err := lc.ListenPacket(ctx, "udp", laddr.String())
	if err != nil {
		return nil, err
	}
	c.setDialOwned(pconn)
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	udpConn, ok := pconn.(*net.UDPConn)
	if !ok {
		return nil, errors.New("kcp dial: ListenPacket did not return *net.UDPConn")
	}

	dataShards, parityShards := c.GetConfig().fecParams()
	conn, err := kcp.NewConn(dst, nil, dataShards, parityShards, udpConn)
	if err != nil {
		return nil, err
	}
	// Wrap so finishDial ownership checks stay pointer-comparable if dialOwned
	// is ever a non-comparable closer elsewhere.
	owned := &struct{ io.Closer }{conn}
	c.dialMu.Lock()
	c.dialOwned = owned
	canceled := ctx.Err() != nil
	c.dialMu.Unlock()
	if canceled {
		return nil, errors.New("dial canceled")
	}

	c.setParam(conn)

	if !c.finishDial(ctx, owned) {
		return nil, errors.New("dial canceled")
	}
	return &KcpConn{sess: conn}, nil
}

func (c *KcpConn) Listen(dst string) (Conn, error) {
	dataShards, parityShards := c.GetConfig().fecParams()
	kl, err := kcp.ListenWithOptions(dst, nil, dataShards, parityShards)
	if err != nil {
		return nil, err
	}

	_ = kl.SetReadBuffer(4 * 1024 * 1024)
	_ = kl.SetWriteBuffer(4 * 1024 * 1024)
	_ = kl.SetDSCP(46)

	return &KcpConn{listener: kl, config: c.GetConfig()}, nil
}

func (c *KcpConn) Accept() (Conn, error) {
	if c.listener == nil {
		return nil, errors.New("not listen")
	}

	conn, err := c.listener.Accept()
	if err != nil {
		return nil, err
	}

	sess, ok := conn.(*kcp.UDPSession)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("kcp accept: unexpected session type")
	}
	c.setParam(sess)
	return &KcpConn{sess: sess}, nil
}

func (c *KcpConn) setParam(conn *kcp.UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(10000, 10000)
	conn.SetReadBuffer(16 * 1024 * 1024)
	conn.SetWriteBuffer(16 * 1024 * 1024)
	// nodelay on, 20ms update, fast resend, non-congestion-control nc=1
	conn.SetNoDelay(1, 20, 2, 1)
	conn.SetMtu(1200)
	conn.SetACKNoDelay(true)
}
