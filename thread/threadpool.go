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
	// pushNum/processNum 会被多个 AddJob 调用方与 worker 并发更新，
	// GetStat/ResetStat 也会并发读取，必须走原子操作，
	// 否则 -race 下报 DATA RACE 且自增丢计数。
	pushNum    []atomic.Int64
	processNum []atomic.Int64
}

type ThreadPoolStat struct {
	Datalen    []int
	Pushnum    []int
	Processnum []int
}

func NewThreadPool(max int, buffer int, exef func(interface{})) *ThreadPool {
	ca := make([]chan interface{}, max)
	control := make(chan int, max)
	for index := range ca {
		ca[index] = make(chan interface{}, buffer)
	}

	tp := &ThreadPool{
		max:        max,
		exef:       exef,
		ca:         ca,
		control:    control,
		pushNum:    make([]atomic.Int64, max),
		processNum: make([]atomic.Int64, max),
	}

	for index := range ca {
		go tp.run(index)
	}

	for atomic.LoadInt32(&tp.workerNum) < int32(max) {
		time.Sleep(10 * time.Millisecond) // 等待所有工作线程启动
	}

	return tp
}

func (tp *ThreadPool) AddJob(hash int, v interface{}) {
	index := common.AbsInt(hash) % len(tp.ca)
	tp.ca[index] <- v
	tp.pushNum[index].Add(1)
}

func (tp *ThreadPool) AddJobTimeout(hash int, v interface{}, timeoutms int) bool {
	index := common.AbsInt(hash) % len(tp.ca)
	select {
	case tp.ca[index] <- v:
		tp.pushNum[index].Add(1)
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
			tp.processNum[index].Add(1)
		}
	}
}

// GetStat 返回此刻统计值的快照，不复用池内部的计数存储。
func (tp *ThreadPool) GetStat() ThreadPoolStat {
	stat := ThreadPoolStat{
		Datalen:    make([]int, len(tp.ca)),
		Pushnum:    make([]int, len(tp.ca)),
		Processnum: make([]int, len(tp.ca)),
	}
	for index := range tp.ca {
		stat.Datalen[index] = len(tp.ca[index])
		stat.Pushnum[index] = int(tp.pushNum[index].Load())
		stat.Processnum[index] = int(tp.processNum[index].Load())
	}
	return stat
}

func (tp *ThreadPool) ResetStat() {
	for index := range tp.ca {
		tp.pushNum[index].Store(0)
		tp.processNum[index].Store(0)
	}
}
