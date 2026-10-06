package thread

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tp := NewThreadPool(100, 100, func(i interface{}) {
		v := i.(int)
		fmt.Println(v)
	})
	tp.AddJob(1, 1)
	tp.AddJob(2, 2)
	tp.AddJob(101, 101)
	tp.AddJob(3, 3)
	tp.AddJob(4, 4)
	tp.AddJob(201, 201)
	tp.Stop()
	fmt.Println("Stop")
}

func Test2(t *testing.T) {
	tp := NewThreadPool(2, 1, func(i interface{}) {
		v := i.(int)
		fmt.Println(v)
		time.Sleep(time.Second)
	})
	tp.AddJob(0, 0)
	tp.AddJob(1, 1)
	tp.AddJob(2, 2)
	tp.AddJob(3, 3)
	tp.AddJob(4, 4)
	tp.AddJob(5, 5)
	time.Sleep(time.Second * 2)
	tp.Stop()
	fmt.Println("Stop")
	fmt.Println(tp.GetStat())
	tp.ResetStat()
	fmt.Println(tp.GetStat())
	tp.AddJob(5, 5)
	fmt.Println(tp.GetStat())
}

func Test3(t *testing.T) {
	tp := NewThreadPool(1, 1, func(i interface{}) {
		v := i.(int)
		fmt.Println(v)
		time.Sleep(time.Second * 10)
	})
	ret := tp.AddJobTimeout(0, 0, 1000)
	fmt.Println("0 Stop ", ret)
	ret = tp.AddJobTimeout(1, 1, 1000)
	fmt.Println("1 Stop", ret)
	ret = tp.AddJobTimeout(2, 2, 1000)
	fmt.Println("2 Stop", ret)
	ret = tp.AddJobTimeout(3, 3, 1000)
	fmt.Println("3 Stop", ret)
	ret = tp.AddJobTimeout(4, 4, 1000)
	fmt.Println("4 Stop", ret)
	tp.Stop()
	fmt.Println("Stop")
}

// TestConcurrentStat 并发 AddJob/GetStat 下统计不能丢计数，-race 下不能报竞争。
func TestConcurrentStat(t *testing.T) {
	var processed atomic.Int32
	tp := NewThreadPool(8, 64, func(i interface{}) {
		processed.Add(1)
	})

	const writers = 8
	const perWriter = 500
	const total = writers * perWriter

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				tp.AddJob(seed*perWriter+i, i)
			}
		}(w)
	}

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				tp.GetStat()
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Wait()
	for processed.Load() < total {
		time.Sleep(time.Millisecond)
	}
	close(stop)
	<-readerDone
	tp.Stop()

	stat := tp.GetStat()
	totalPush := 0
	totalProc := 0
	for i := range stat.Pushnum {
		totalPush += stat.Pushnum[i]
		totalProc += stat.Processnum[i]
	}
	if totalPush != total {
		t.Errorf("Pushnum total = %d, want %d", totalPush, total)
	}
	if totalProc != total {
		t.Errorf("Processnum total = %d, want %d", totalProc, total)
	}

	tp.ResetStat()
	stat = tp.GetStat()
	for i := range stat.Pushnum {
		if stat.Pushnum[i] != 0 || stat.Processnum[i] != 0 {
			t.Fatalf("ResetStat did not clear counters at %d: %+v", i, stat)
		}
	}
}
