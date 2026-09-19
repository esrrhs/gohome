package common

import (
	"sync/atomic"
	"time"
)

var gnowsecond atomic.Value // time.Time

func init() {
	gnowsecond.Store(time.Now())
	go updateNowInSecond()
}

func GetNowUpdateInSecond() time.Time {
	return gnowsecond.Load().(time.Time)
}

func updateNowInSecond() {
	defer CrashLog()

	for {
		gnowsecond.Store(time.Now())
		Sleep(1)
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
	for time.Now().Sub(last) < time.Second*time.Duration(sec) {
		time.Sleep(time.Millisecond * 100)
	}
}
