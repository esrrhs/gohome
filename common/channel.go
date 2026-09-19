package common

import (
	"sync"
	"time"
)

type Channel struct {
	ch     chan interface{}
	closed bool
	mu     sync.RWMutex
}

func NewChannel(len int) *Channel {
	return &Channel{ch: make(chan interface{}, len)}
}

func (c *Channel) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
}

func (c *Channel) Write(v interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return
	}
	c.ch <- v
}

func (c *Channel) WriteTimeout(v interface{}, timeoutms int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
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
	select {
	case c.ch <- v:
		return true
	case <-timer.C:
		return false
	}
}

func (c *Channel) Ch() <-chan interface{} {
	return c.ch
}
