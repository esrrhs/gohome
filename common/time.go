package common

import (
	"sync"
	"sync/atomic"
	"time"
)

var (
	gnowsecond atomic.Value // time.Time
	gnowOnce   sync.Once
)

func GetNowUpdateInSecond() time.Time {
	gnowOnce.Do(func() {
		gnowsecond.Store(time.Now())
		go updateNowInSecond()
	})
	if val := gnowsecond.Load(); val != nil {
		return val.(time.Time)
	}
	return time.Now()
}

func updateNowInSecond() {
	defer CrashLog()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		gnowsecond.Store(now)
	}
}

func Elapsed(f func(d time.Duration)) func() {
	start := time.Now()
	return func() {
		f(time.Since(start))
	}
}

func Sleep(sec int) {
	last := time.Now()
	for time.Since(last) < time.Second*time.Duration(sec) {
		time.Sleep(time.Millisecond * 100)
	}
}
