package network

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 可靠协议带宽饱和度 e2e：模拟限速信道（默认 2/5/8 Mbps，全双工），验证各协议
// 能否跑满带宽。goodput = 校验通过的应用层字节 / 时长；利用率 = goodput / 链路速率。
//
//	Part A（可移植）：{tcp, rhttp, rudp, kcp, quic} × 每档速率 × loss0 × 无时延，
//	                纯限速走用户态代理，macOS/无特权环境可跑。
//	Part B（BDP 维度）：同速率 + 单向 100ms 时延。rudp/kcp/quic 仍走用户态代理；
//	                tcp/rhttp/ricmp 需内核 tc netem，非 Linux/无特权自动 skip。
//
// 断言：loss0 下利用率 >= 80%（预留慢启动、协议头开销与限速器粒度余量）。
//
//	单位：Mbps（bit/s）。可用 GOHOME_BW_RATES="2,5,8" 覆盖速率档（如测 MB/s 瓶颈
//	可传 "16,40,64"），GOHOME_BW_DURATION=10s 覆盖单档时长。
//
//	go test ./network/ -run 'TestReliableBandwidthSaturation$' -v -timeout 30m
//
// 注：rdns 为 DNS 隧道传输，帧极小且受 DNS 报文形态约束，不属于带宽型协议，不测。

// bwSatOneWayDelay 是 Part B 的单向时延。RTT 200ms 下 8Mbps 的 BDP 仅 200KB，
// 远小于各协议窗口（rudp 4000 帧×1200B、kcp 10000 包、quic/tcp 自动调窗），
// 因此 Part B 隔离的是「限速 + 时延积」组合下的饱和能力。
const bwSatOneWayDelay = 100 * time.Millisecond

// bwSatMinUtilization 是 loss0 信道的饱和度下限。
const bwSatMinUtilization = 0.80

func bwSatRates(t *testing.T) []int64 {
	t.Helper()
	raw := os.Getenv("GOHOME_BW_RATES")
	if strings.TrimSpace(raw) == "" {
		return []int64{2 * 1000 * 1000, 5 * 1000 * 1000, 8 * 1000 * 1000} // 2, 5, 8 Mbps in bps
	}
	var rates []int64
	for _, f := range strings.Split(raw, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil || v <= 0 {
			t.Fatalf("invalid GOHOME_BW_RATES %q: %v", raw, err)
		}
		rates = append(rates, int64(v*1000*1000))
	}
	return rates
}

func bwSatDuration() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GOHOME_BW_DURATION")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Second
}

func TestReliableBandwidthSaturation(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	rates := bwSatRates(t)
	duration := bwSatDuration()

	// Part A：纯限速，用户态代理，全平台可跑。
	for _, proto := range []string{"tcp", "rhttp", "rudp", "kcp", "quic"} {
		proto := proto
		for _, rateBps := range rates {
			rateBps := rateBps
			t.Run(fmt.Sprintf("%s/%dMbps", proto, rateBps/1000/1000), func(t *testing.T) {
				runBandwidthSaturationCase(t, proto, channelSpec{rateBps: rateBps}, duration)
			})
		}
	}

	// Part B：限速 + 单向时延（BDP 维度）。tcp/rhttp/ricmp 依赖 tc，无特权自动 skip。
	for _, proto := range []string{"rudp", "kcp", "quic", "tcp", "rhttp", "ricmp"} {
		proto := proto
		for _, rateBps := range rates {
			rateBps := rateBps
			t.Run(fmt.Sprintf("%s/%dMbps+delay100ms", proto, rateBps/1000/1000), func(t *testing.T) {
				runBandwidthSaturationCase(t, proto, channelSpec{oneWayDelay: bwSatOneWayDelay, rateBps: rateBps}, duration)
			})
		}
	}
}

func runBandwidthSaturationCase(t *testing.T, proto string, cs channelSpec, duration time.Duration) {
	t.Helper()
	gotMBps, total, err := runFixedPayloadDownloadWithSpec(proto, cs, duration, cs.rateBps/1000/1000)
	if err != nil {
		if isEnvironmentUnavailable(err) {
			t.Skipf("%s channel sim unavailable in unprivileged environment: %v", proto, err)
		}
		t.Fatal(err)
	}
	rateMbps := float64(cs.rateBps) / (1000 * 1000)
	gotMbps := gotMBps * 8
	util := gotMbps / rateMbps
	t.Logf("%s @ %dMbps: goodput %.2fMbps (%.2f MB/s), utilization %.1f%%",
		proto, cs.rateBps/1000/1000, gotMbps, gotMBps, util*100)
	if total <= 0 {
		t.Fatal("no verified bytes")
	}
	if util < bwSatMinUtilization {
		t.Errorf("bandwidth not saturated: %.1f%% < %.0f%% of %dMbps link",
			util*100, bwSatMinUtilization*100, cs.rateBps/1000/1000)
	}
}

// startTCPRateProxy 启动一个用户态限速 TCP 代理：监听 127.0.0.1:0，每个进入的
// 客户端连接都会拨通 backend，然后双向字节流各自经过一个 ratePacer 转发。
// 用于 macOS / 无特权环境下给 tcp、rhttp 提供纯限速信道（不支持时延/丢包，
// 那两项需要内核 tc netem）。全双工：两个方向独立按 rateBps 限速。
func startTCPRateProxy(backend string, rateBps int64) (proxyAddr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	var closed atomic.Bool
	var wg = new(sync.WaitGroup)

	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				proxyTCPPipe(c, backend, rateBps)
			}()
		}
	}()

	stop = func() {
		if closed.Swap(true) {
			return
		}
		_ = ln.Close()
		wg.Wait()
	}
	return ln.Addr().String(), stop, nil
}

func proxyTCPPipe(client net.Conn, backend string, rateBps int64) {
	b, err := net.Dial("tcp", backend)
	if err != nil {
		client.Close()
		return
	}
	defer client.Close()
	defer b.Close()

	upPacer := &ratePacer{rateBps: float64(rateBps)}   // client -> backend
	downPacer := &ratePacer{rateBps: float64(rateBps)} // backend -> client

	done := make(chan struct{}, 2)
	go func() {
		pacedTCPForward(b, client, downPacer)
		_ = b.Close()
		_ = client.Close()
		done <- struct{}{}
	}()
	go func() {
		pacedTCPForward(client, b, upPacer)
		_ = b.Close()
		_ = client.Close()
		done <- struct{}{}
	}()
	<-done
	<-done
}

// pacedTCPForward 从 src 读、经 pacer 限速后写 dst。读块大小 16KB：
// 8Mbps 时每块约 16ms 时隙，pacer 粒度足够，socket 缓冲吸收抖动。
func pacedTCPForward(dst net.Conn, src net.Conn, pacer *ratePacer) {
	buf := make([]byte, 16<<10)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if w := pacer.book(n); w > 0 {
				time.Sleep(w)
			}
			written := 0
			for written < n {
				wn, werr := dst.Write(buf[written:n])
				written += wn
				if werr != nil {
					return
				}
			}
		}
		if rerr != nil {
			return
		}
	}
}
