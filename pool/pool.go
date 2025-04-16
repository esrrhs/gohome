package pool

import (
	"container/list"
	"sync"
)

/*
Package pool 实现了一个对象池，用于管理重复使用的对象，减少内存分配和释放带来的开销。
通过预先分配对象并在使用后将其返回池中，可以提高性能并减少垃圾回收压力。

主要功能包括：

- 创建一个对象池，允许用户定义对象的分配方式
- 从池中分配对象，并在不再使用时将对象返回池
- 追踪已使用和可用对象的数量
- 支持线程安全的操作，确保在并发环境下的安全性
- 提供获取当前已使用和可用对象数量的功能
*/

type PoolElement struct {
	Value interface{}
}

type Pool struct {
	use    map[*PoolElement]int
	free   *list.List
	allocf func() interface{}
	lock   sync.Mutex
}

func New(allocf func() interface{}) *Pool {
	p := &Pool{}
	p.allocf = allocf
	p.use = make(map[*PoolElement]int)
	p.free = list.New()
	return p
}

func (p *Pool) Alloc() *PoolElement {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.free.Len() <= 0 {
		pe := PoolElement{Value: p.allocf()}
		p.free.PushBack(&pe)
	}
	e := p.free.Front()
	pe := e.Value.(*PoolElement)
	p.free.Remove(e)
	p.use[pe]++
	return pe
}

func (p *Pool) Free(pe *PoolElement) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if _, ok := p.use[pe]; ok {
		delete(p.use, pe)
		p.free.PushFront(pe)
	}
}

func (p *Pool) UsedSize() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return len(p.use)
}

func (p *Pool) FreeSize() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.free.Len()
}
