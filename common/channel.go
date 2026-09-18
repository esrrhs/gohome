package common

import (
	"sync"
	"time"
)

type Channel struct {
	ch     chan interface{}
	closed bool
	mu     sync.Mutex
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
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	defer func() {
		if recover() != nil {
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
		}
	}()
	c.ch <- v
}

func (c *Channel) WriteTimeout(v interface{}, timeoutms int) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()

	defer func() {
		if recover() != nil {
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
		}
	}()

	select {
	case c.ch <- v:
		return true
	case <-time.After(time.Duration(timeoutms) * time.Millisecond):
		return false
	}
}

func (c *Channel) Ch() <-chan interface{} {
	return c.ch
}
