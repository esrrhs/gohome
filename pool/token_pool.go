package pool

import (
	"sync"
)

// TokenPool 定义一个结构体表示资源池
type TokenPool struct {
	mu      sync.Mutex // 用于保护池的访问
	indexes chan int   // 使用一个 channel 作为资源池
}

// NewResourcePool 初始化一个资源池，最多容纳 N 个资源
func NewTokenPool(max int) *TokenPool {
	rp := &TokenPool{
		indexes: make(chan int, max),
	}
	for i := 0; i < max; i++ {
		rp.indexes <- i // 初始化池中所有资源
	}
	return rp
}

// Acquire 获取一个资源，如果没有资源，则会等待直到有可用资源
func (rp *TokenPool) Acquire() int {
	var index int
	index = <-rp.indexes
	//logInfof("Acquire pool %d", index)
	return index
}

// Release 释放资源，将资源放回池中
func (rp *TokenPool) Release(index int) {
	//logInfof("Release pool %d", index)
	rp.indexes <- index
}
