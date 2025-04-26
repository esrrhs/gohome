package network

import (
	"fmt"
	"github.com/esrrhs/gohome/common"
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

var prop_seq = []float64{1, 1, 1.5, 1}

type BBCongestion struct {
	status        int
	maxfly        int
	flyeddata     int
	lastflyeddata int
	flyingdata    int
	rateflywin    *list.Rlistgo
	flyedwin      *list.Rlistgo
	propindex     int
	last          time.Time
	lastratewin   float64
	lastflyedwin  int
}

func (bb *BBCongestion) Init() {
	bb.status = bbc_status_init
	bb.maxfly = 1024 * 1024
	bb.rateflywin = list.NewRList(bbc_win)
	bb.flyedwin = list.NewRList(bbc_win)
	bb.last = time.Now()
}

func (bb *BBCongestion) RecvAck(id int, size int) {
	if size < 0 {
		panic("error size")
	}
	bb.flyeddata += size
}

func (bb *BBCongestion) CanSend(id int, size int) bool {
	if size < 0 {
		panic("error size")
	}
	if bb.flyingdata > bb.maxfly {
		return false
	}
	bb.flyingdata += size
	return true
}

func (bb *BBCongestion) Update() {

	if bb.flyeddata <= 0 {
		return
	}

	currate := float64(bb.flyingdata) / float64(bb.flyeddata)
	if currate < 1 {
		currate = 1
	}

	if bb.rateflywin.Full() {
		bb.rateflywin.PopFront()
	}
	bb.rateflywin.PushBack(currate)

	lastratewin := math.MaxFloat64
	for e := bb.rateflywin.FrontInter(); e != nil; e = e.Next() {
		rate := e.Value.(float64)
		if rate < lastratewin {
			lastratewin = rate
		}
	}

	if bb.flyedwin.Full() {
		bb.flyedwin.PopFront()
	}
	bb.flyedwin.PushBack(bb.flyeddata)

	lastflyedwin := 0
	for e := bb.flyedwin.FrontInter(); e != nil; e = e.Next() {
		flyed := e.Value.(int)
		if flyed > lastflyedwin {
			lastflyedwin = flyed
		}
	}

	if bb.status == bbc_status_init {
		if float64(bb.flyeddata) <= bbc_maxfly_compare*float64(bb.lastflyeddata) {
			oldmaxfly := bb.maxfly
			bb.maxfly = int(float64(oldmaxfly) / bbc_maxfly_grow)
			bb.status = bbc_status_prop
			//loggo.Debug("bbc_status_init flyeddata %d maxfly %d change", bb.flyeddata, bb.maxfly)
		} else {
			oldmaxfly := bb.maxfly
			bb.maxfly = int(float64(oldmaxfly) * bbc_maxfly_grow)
			//loggo.Debug("bbc_status_init grow flyeddata %d oldmaxfly %d maxfly %d", bb.flyeddata, oldmaxfly, bb.maxfly)
		}
		bb.lastflyeddata = bb.flyeddata
	} else if bb.status == bbc_status_prop {
		maxfly := float64(lastflyedwin) * lastratewin
		curmaxfly := int(maxfly)
		if curmaxfly > bb.maxfly {
			bb.maxfly = curmaxfly
		} else {
			if common.NearlyEqual(bb.flyingdata, bb.maxfly) {
				bb.maxfly = curmaxfly
			}
		}
		bb.maxfly = int(float64(bb.maxfly) * prop_seq[bb.propindex])
		//loggo.Debug("bbc_status_prop lastflyedwin %v lastrate %v maxfly %d prop %v", lastflyedwin, lastrate, bb.maxfly, prop_seq[bb.propindex])
		bb.propindex++
		bb.propindex = bb.propindex % len(prop_seq)
	} else {
		panic("error status " + strconv.Itoa(bb.status))
	}

	bb.flyeddata = 0
	bb.flyingdata = 0
	bb.lastratewin = lastratewin
	bb.lastflyedwin = lastflyedwin

	if bb.maxfly < 1024*1024 {
		bb.maxfly = 1024 * 1024
	}
}

func (bb *BBCongestion) Info() string {
	return fmt.Sprintf("status %v maxfly %v flyeddata %v lastratewin %v lastflyedwin %v", bb.status, bb.maxfly,
		bb.flyeddata, bb.lastratewin, bb.lastflyedwin)
}
