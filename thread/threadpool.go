package thread

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
)

/*
ThreadPool 实现了一个简单的线程池，用于并发处理任务。
该线程池允许将任务添加到池中，并基于最大线程数控制并发执行。

主要功能包括：

- 创建和管理固定大小的线程池
- 提供添加任务的接口，包括线程安全的任务队列
- 支持带超时的任务添加
- 提供停止线程池的功能
- 统计每个线程的任务数量和处理结果
- 提供统计信息和重置统计数据的功能
*/

type ThreadPool struct {
	workResultLock sync.WaitGroup
	workerNum      int32
	max            int
	exef           func(interface{})
	ca             []chan interface{}
	control        chan int
	stat           ThreadPoolStat
}

type ThreadPoolStat struct {
	Datalen    []int
	Pushnum    []int
	Processnum []int
}

func NewThreadPool(max int, buffer int, exef func(interface{})) *ThreadPool {
	ca := make([]chan interface{}, max)
	control := make(chan int, max)
	for index, _ := range ca {
		ca[index] = make(chan interface{}, buffer)
	}

	stat := ThreadPoolStat{}
	stat.Datalen = make([]int, max)
	stat.Pushnum = make([]int, max)
	stat.Processnum = make([]int, max)

	tp := &ThreadPool{max: max, exef: exef, ca: ca, control: control, stat: stat}

	for index := range ca {
		go tp.run(index)
	}

	for atomic.LoadInt32(&tp.workerNum) < int32(max) {
		time.Sleep(10 * time.Millisecond) // 等待所有工作线程启动
	}

	return tp
}

func (tp *ThreadPool) AddJob(hash int, v interface{}) {
	tp.ca[common.AbsInt(hash)%len(tp.ca)] <- v
	tp.stat.Pushnum[common.AbsInt(hash)%len(tp.ca)]++
}

func (tp *ThreadPool) AddJobTimeout(hash int, v interface{}, timeoutms int) bool {
	select {
	case tp.ca[common.AbsInt(hash)%len(tp.ca)] <- v:
		tp.stat.Pushnum[common.AbsInt(hash)%len(tp.ca)]++
		return true
	case <-time.After(time.Duration(timeoutms) * time.Millisecond):
		return false
	}
}

func (tp *ThreadPool) Stop() {
	for i := 0; i < tp.max; i++ {
		tp.control <- i
	}
	tp.workResultLock.Wait()
}

func (tp *ThreadPool) run(index int) {
	defer common.CrashLog()

	tp.workResultLock.Add(1)
	defer tp.workResultLock.Done()

	atomic.AddInt32(&tp.workerNum, 1)

	for {
		select {
		case <-tp.control:
			return
		case v := <-tp.ca[index]:
			tp.exef(v)
			tp.stat.Processnum[index]++
		}
	}
}

func (tp *ThreadPool) GetStat() ThreadPoolStat {
	for index := range tp.ca {
		tp.stat.Datalen[index] = len(tp.ca[index])
	}
	return tp.stat
}

func (tp *ThreadPool) ResetStat() {
	for index := range tp.ca {
		tp.stat.Pushnum[index] = 0
		tp.stat.Processnum[index] = 0
	}
}
