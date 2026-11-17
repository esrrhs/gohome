package network

import (
	"fmt"
	"github.com/esrrhs/gohome/list"
	"math"
	"strconv"
	"time"
)

/*
BBCongestion 实现了基于带宽的拥塞控制算法，该算法用于网络传输中的流量管理和拥塞控制。

该包定义了 BBCongestion 结构体，其中封装了控制网络数据传输的逻辑，通过动态调整最大飞行数据量来适应网络条件。

算法原理：

BBCongestion 算法利用动态窗口调整策略来实现拥塞控制，具体流程如下：

1. 初始化状态：算法开始时，设置最大允许飞行数据量为初始值（如 1 MB），并创建滑动窗口以追踪传输速率和历史数据量。

2. 接收确认 ACK：当接收到数据确认时，更新已发送的数据量。如果接收到的大小不合法（如小于等于 0），则抛出错误。

3. 发送控制：在尝试发送数据前，算法检查当前正在发送的数据量是否小于或等于最大飞行数据量。如果超出最大值，拒绝发送。

4. 更新算法状态：
   - 计算当前的发送速率，作为当前正在发送数据量与已发送数据量的比值。如果速率小于 1，则将其设置为 1。
   - 在每次更新时，加入新的发送速率到速率窗口，并计算历史速率的最小值，以便根据最新的网络情况调整最大飞行数据量。
   - 如果当前状态为初始化且已发送数据量低于最大飞行数据量的一个比较值，算法将缩减最大飞行数据量；否则，它将增加最大飞行数据量。
   - 如果当前状态是增长，算法根据最新计算出的最小速率与已发送数据量动态调整最大飞行数据量。

5. 处理状态：算法通过状态机的方式在初始化和增长状态之间切换，以应对网络条件的变化。状态变化时更新数据量和比例序列索引。

该算法旨在动态调整网络流量，以实现高效的带宽利用，减少数据丢包的情况，从而提高网络传输的可靠性和效率。
*/

const (
	bbc_status_init = 0
	bbc_status_prop = 1

	bbc_win            = 5
	bbc_maxfly_grow    = 2.1
	bbc_maxfly_compare = float64(1.5)
)

var prop_seq = []float64{1.20, 1.05, 1.15, 1.05}

type BBCongestion struct {
	status        int
	maxfly        int
	flyeddata     int
	lastflyeddata int
	flyingdata    int
	totalsent     int
	resendsSent   int
	rateflywin    *list.Rlistgo
	flyedwin      *list.Rlistgo
	propindex     int
	last          time.Time
	lastratewin   float64
	lastflyedwin  int
	rtt           time.Duration
	minRtt        time.Duration
	minRttTime    time.Time
	lastPaceTime  time.Time
	paceTokens    float64
	pacingRate    float64
}

func (bb *BBCongestion) Init() {
	now := time.Now()
	bb.status = bbc_status_init
	bb.maxfly = 1024 * 1024
	bb.rateflywin = list.NewRList(bbc_win)
	bb.flyedwin = list.NewRList(bbc_win)
	bb.last = now
	bb.rtt = 0
	bb.minRtt = 0
	bb.minRttTime = now
	bb.lastPaceTime = now
	bb.paceTokens = float64(64 * 1024)
	bb.pacingRate = 0
}

func (bb *BBCongestion) SetRtt(rtt time.Duration) {
	if rtt > 0 {
		bb.rtt = rtt
		now := time.Now()
		if bb.minRtt == 0 || (rtt < bb.minRtt && rtt > time.Millisecond*5) || now.Sub(bb.minRttTime) > 30*time.Second {
			bb.minRtt = rtt
			bb.minRttTime = now
		}
		if bb.pacingRate == 0 && bb.maxfly > 0 {
			initialRate := float64(1024*1024) / bb.rtt.Seconds()
			if initialRate < 5*1024*1024 {
				initialRate = 5 * 1024 * 1024
			} else if initialRate > 10*1024*1024 {
				initialRate = 10 * 1024 * 1024
			}
			bb.pacingRate = initialRate
			if bb.maxfly < 4*1024*1024 {
				bb.maxfly = 4 * 1024 * 1024
			}
		}
	}
}

func (bb *BBCongestion) RecvAck(id int, size int) {
	if size < 0 {
		panic("error size")
	}
	bb.flyeddata += size
	bb.flyingdata -= size
	if bb.flyingdata < 0 {
		bb.flyingdata = 0
	}
}

func (bb *BBCongestion) CanSend(id int, size int) bool {
	if size < 0 {
		panic("error size")
	}
	if bb.flyingdata > bb.maxfly {
		return false
	}
	if bb.rtt > 0 && bb.pacingRate > 0 {
		now := time.Now()
		elapsed := now.Sub(bb.lastPaceTime)
		if elapsed > 0 {
			bb.lastPaceTime = now
			bb.paceTokens += elapsed.Seconds() * bb.pacingRate
			maxBurst := float64(256 * 1024)
			if bb.paceTokens > maxBurst {
				bb.paceTokens = maxBurst
			}
		}
		if bb.paceTokens < float64(size) {
			return false
		}
		bb.paceTokens -= float64(size)
	}
	bb.flyingdata += size
	bb.totalsent += size
	return true
}

func (bb *BBCongestion) CanSendResend(id int, size int) bool {
	if size < 0 {
		panic("error size")
	}
	if bb.rtt > 0 && bb.pacingRate > 0 {
		now := time.Now()
		elapsed := now.Sub(bb.lastPaceTime)
		if elapsed > 0 {
			bb.lastPaceTime = now
			bb.paceTokens += elapsed.Seconds() * bb.pacingRate
			maxBurst := float64(256 * 1024)
			if bb.paceTokens > maxBurst {
				bb.paceTokens = maxBurst
			}
		}
		// Retransmissions are prioritized to unblock head-of-line blocking:
		// allow overdraft down to -128KB, while sharing the total wire pacing budget.
		if bb.paceTokens < -float64(128*1024) {
			return false
		}
		bb.paceTokens -= float64(size)
	}
	bb.resendsSent += size
	return true
}

func (bb *BBCongestion) Update() {
	now := time.Now()
	elapsed := now.Sub(bb.last).Seconds()
	bb.last = now
	if elapsed < 0.2 {
		elapsed = 0.2
	} else if elapsed > 5.0 {
		elapsed = 5.0
	}

	if bb.flyeddata <= 0 {
		bb.flyingdata = 0
		bb.totalsent = 0
		bb.resendsSent = 0
		return
	}

	// Skip idle/keepalive intervals so bursty traffic (e.g. video chunk pauses)
	// does not decay deliveryRate and pacingRate back down to the floor.
	if bb.totalsent < 128*1024 && bb.flyeddata < 128*1024 {
		bb.flyeddata = 0
		bb.flyingdata = 0
		bb.totalsent = 0
		bb.resendsSent = 0
		return
	}

	sent := bb.totalsent
	if sent <= 0 {
		sent = bb.flyingdata
	}
	currate := float64(sent) / float64(bb.flyeddata)
	if currate < 1 {
		currate = 1
	} else if currate > 4 {
		currate = 4
	}

	if bb.rateflywin.Full() {
		bb.rateflywin.PopFront()
	}
	bb.rateflywin.PushBack(currate)

	lastratewin := math.MaxFloat64
	sumRate := 0.0
	cntRate := 0
	for e := bb.rateflywin.FrontInter(); e != nil; e = e.Next() {
		rate := e.Value.(float64)
		if rate < lastratewin {
			lastratewin = rate
		}
		sumRate += rate
		cntRate++
	}
	avgRate := currate
	if cntRate > 0 {
		avgRate = sumRate / float64(cntRate)
	}

	// Loss compensation factor (sent/acked), bounded so a transient ACK
	// outage cannot blow the pacing rate up.
	lossComp := avgRate
	if lossComp < 1.0 {
		lossComp = 1.0
	} else if lossComp > 1.8 {
		lossComp = 1.8
	}

	flyedRate := int(float64(bb.flyeddata) / elapsed)
	sentRate := int(float64(sent) / elapsed)
	effectiveFlyed := flyedRate
	if sentRate > 0 && effectiveFlyed > int(float64(sentRate)*1.15) {
		effectiveFlyed = int(float64(sentRate) * 1.15)
	}

	if bb.flyedwin.Full() {
		bb.flyedwin.PopFront()
	}
	bb.flyedwin.PushBack(effectiveFlyed)

	lastflyedwin := 0
	for e := bb.flyedwin.FrontInter(); e != nil; e = e.Next() {
		flyed := e.Value.(int)
		if flyed > lastflyedwin {
			lastflyedwin = flyed
		}
	}

	if bb.status == bbc_status_init {
		growFactor := 1.40
		shouldExit := false
		if bb.rtt > 0 {
			queueDelay := time.Duration(0)
			if bb.minRtt > 0 && bb.rtt > bb.minRtt {
				queueDelay = bb.rtt - bb.minRtt
			}
			// Loss is NOT a congestion signal here: on lossy cross-ocean paths
			// random loss of 15-30% is independent of send rate. Only RTT
			// inflation (standing queue) or a delivery plateau ends startup.
			plateau := bb.lastflyeddata > 0 && float64(flyedRate) < float64(bb.lastflyeddata)*1.03 && flyedRate > 6*1024*1024 && queueDelay > 30*time.Millisecond
			severe := queueDelay > 100*time.Millisecond
			shouldExit = severe || plateau
		} else {
			growFactor = bbc_maxfly_grow
			shouldExit = currate > 1.20 || (bb.lastflyeddata > 0 && float64(bb.flyeddata) <= bbc_maxfly_compare*float64(bb.lastflyeddata))
		}

		if shouldExit {
			bb.status = bbc_status_prop
			if bb.rtt > 0 {
				// Wire rate = acked rate scaled back up by the observed loss
				// ratio, so random loss does not shrink the pacing rate.
				deliveryRate := float64(flyedRate) * lossComp
				if deliveryRate < 2*1024*1024 {
					deliveryRate = 2 * 1024 * 1024
				}
				bdp := int(deliveryRate * bb.rtt.Seconds())
				if bdp < 1024*1024 {
					bdp = 1024 * 1024
				}
				bb.maxfly = int(float64(bdp) * 3.0)
				if bb.maxfly < 4*1024*1024 {
					bb.maxfly = 4 * 1024 * 1024
				}
				bb.pacingRate = deliveryRate * 1.05
			} else {
				oldmaxfly := bb.maxfly
				bb.maxfly = int(float64(oldmaxfly) / growFactor)
			}
		} else {
			oldmaxfly := bb.maxfly
			bb.maxfly = int(float64(oldmaxfly) * growFactor)
			if bb.rtt > 0 {
				if bb.pacingRate <= 0 {
					bb.pacingRate = 5 * 1024 * 1024
				} else {
					nextRate := bb.pacingRate * growFactor
					if capRate := float64(flyedRate) * 1.6; capRate > 5*1024*1024 && nextRate > capRate {
						nextRate = capRate
					}
					bb.pacingRate = nextRate
				}
				minFly := int(bb.pacingRate * bb.rtt.Seconds() * 3.0)
				if minFly < 4*1024*1024 {
					minFly = 4 * 1024 * 1024
				}
				if bb.maxfly < minFly {
					bb.maxfly = minFly
				}
			}
		}
		bb.lastflyeddata = flyedRate
	} else if bb.status == bbc_status_prop {
		if bb.rtt > 0 {
			// deliveryRate is the ACKed rate. With random loss p the wire must
			// carry deliveryRate/(1-p) = deliveryRate*lossComp to sustain it,
			// otherwise a probe gain of ~1.1 against a delivery ratio of 0.83
			// shrinks pacing every cycle and throughput decays to a floor.
			deliveryRate := float64(lastflyedwin)
			if deliveryRate < 1024*1024 {
				deliveryRate = 1024 * 1024
			}
			wireRate := deliveryRate * lossComp
			bdp := int(wireRate * bb.rtt.Seconds())
			if bdp < 1024*1024 {
				bdp = 1024 * 1024
			}

			queueDelay := time.Duration(0)
			if bb.minRtt > 0 && bb.rtt > bb.minRtt {
				queueDelay = bb.rtt - bb.minRtt
			}

			if queueDelay > 110*time.Millisecond {
				// Standing queue: loss alone never triggers this, only RTT inflation.
				bb.pacingRate = wireRate * 0.85
				bb.maxfly = int(float64(bdp) * 2.5)
			} else if queueDelay < 70*time.Millisecond {
				// Healthy queue: probe up on the loss-compensated wire rate.
				probeFactor := prop_seq[bb.propindex]
				bb.pacingRate = wireRate * probeFactor
				bb.maxfly = int(float64(bdp) * 3.5)
			} else {
				// Moderate queue delay: hold steady.
				bb.pacingRate = wireRate * 1.0
				bb.maxfly = int(float64(bdp) * 3.0)
			}
			if bb.pacingRate < 4*1024*1024 {
				bb.pacingRate = 4 * 1024 * 1024
			}
			if bb.maxfly < 4*1024*1024 {
				bb.maxfly = 4 * 1024 * 1024
			}
		} else {
			curmaxfly := int(float64(lastflyedwin) * lastratewin)
			if currate < 1.08 && curmaxfly < lastflyedwin {
				curmaxfly = lastflyedwin
			}
			if curmaxfly > bb.maxfly {
				bb.maxfly = curmaxfly
			} else if currate > 1.25 {
				bb.maxfly = int(float64(bb.maxfly) * 0.85)
			}
			bb.maxfly = int(float64(bb.maxfly) * prop_seq[bb.propindex])
		}
		bb.propindex++
		bb.propindex = bb.propindex % len(prop_seq)
	} else {
		panic("error status " + strconv.Itoa(bb.status))
	}

	bb.flyeddata = 0
	bb.flyingdata = 0
	bb.totalsent = 0
	bb.resendsSent = 0
	bb.lastratewin = lastratewin
	bb.lastflyedwin = lastflyedwin

	if bb.maxfly < 1024*1024 {
		bb.maxfly = 1024 * 1024
	}
}

func (bb *BBCongestion) Info() string {
	return fmt.Sprintf("status %v maxfly %v flyeddata %v rtt %v lastratewin %v lastflyedwin %v", bb.status, bb.maxfly,
		bb.flyeddata, bb.rtt, bb.lastratewin, bb.lastflyedwin)
}
