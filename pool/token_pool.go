package pool

import (
	"sync"
)

/*
TokenPool 提供了一个简单的资源池实现，使用 Go 的 channel 功能来管理和复用资源。

TokenPool 结构体表示一个资源池，允许多个 goroutine 并发地从池中获取和释放资源。
该资源池能够管理固定数量的整数资源，并在资源被占用时自动阻塞请求，直至资源可用。

功能包括：

- 初始化资源池以指定的最大资源数量
- 获取可用资源，若无可用资源则会阻塞等待
- 释放资源，将资源返回到池中以供后续使用
*/

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
	return index
}

// Release 释放资源，将资源放回池中
func (rp *TokenPool) Release(index int) {
	rp.indexes <- index
}
