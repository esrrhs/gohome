package common

import (
	"sync"
	"sync/atomic"
	"time"
)

type Channel struct {
	ch        chan interface{}
	done      chan struct{}
	closeOnce sync.Once
	closing   atomic.Bool
	closed    bool
	mu        sync.RWMutex
}

func NewChannel(len int) *Channel {
	return &Channel{
		ch:   make(chan interface{}, len),
		done: make(chan struct{}),
	}
}

func (c *Channel) Close() {
	c.closeOnce.Do(func() {
		// Signal closing and close done BEFORE acquiring the write lock so any
		// goroutine blocked on a full c.ch inside Write/WriteTimeout wakes up
		// and releases its RLock instead of deadlocking with Lock().
		c.closing.Store(true)
		if c.done != nil {
			close(c.done)
		}
		c.mu.Lock()
		c.closed = true
		close(c.ch)
		c.mu.Unlock()
	})
}

func (c *Channel) Write(v interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed || c.closing.Load() {
		return
	}
	if c.done == nil {
		c.ch <- v
		return
	}
	select {
	case c.ch <- v:
	case <-c.done:
	}
}

func (c *Channel) WriteTimeout(v interface{}, timeoutms int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed || c.closing.Load() {
		return false
	}

	if timeoutms <= 0 {
		select {
		case c.ch <- v:
			return true
		default:
			return false
		}
	}

	timer := time.NewTimer(time.Duration(timeoutms) * time.Millisecond)
	defer timer.Stop()
	if c.done == nil {
		select {
		case c.ch <- v:
			return true
		case <-timer.C:
			return false
		}
	}
	select {
	case c.ch <- v:
		return true
	case <-c.done:
		return false
	case <-timer.C:
		return false
	}
}

func (c *Channel) Ch() <-chan interface{} {
	return c.ch
}
