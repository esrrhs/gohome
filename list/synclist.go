package list

import (
	"container/list"
	"sync"
)

/*
synclist 实现了一个线程安全的双向链表。
该链表封装了 Go 标准库中的 container/list，提供了基本的链表操作，同时确保在并发环境下的安全性。

主要功能包括：

- 创建和管理一个线程安全的双向链表
- 提供向链表添加元素的 Push 操作
- 提供从链表移除并返回最后一个元素的 Pop 操作
- 获取链表中元素的数量
- 遍历链表并对每个元素执行指定的函数
- 检查链表中是否包含特定元素
- 支持根据自定义函数检查链表元素的包含关系
*/

type List struct {
	data *list.List
	lock sync.Mutex
}

func NewList() *List {
	q := new(List)
	q.data = list.New()
	return q
}

func (q *List) Push(v interface{}) {
	defer q.lock.Unlock()
	q.lock.Lock()
	q.data.PushFront(v)
}

func (q *List) Pop() interface{} {
	defer q.lock.Unlock()
	q.lock.Lock()
	iter := q.data.Back()
	if iter == nil {
		return nil
	}
	v := iter.Value
	q.data.Remove(iter)
	return v
}

func (q *List) Len() int {
	defer q.lock.Unlock()
	q.lock.Lock()
	return q.data.Len()
}

func (q *List) Range(f func(value interface{})) {
	defer q.lock.Unlock()
	q.lock.Lock()
	for iter := q.data.Back(); iter != nil; iter = iter.Prev() {
		f(iter.Value)
	}
}

func (q *List) Contain(v interface{}) bool {
	defer q.lock.Unlock()
	q.lock.Lock()
	for iter := q.data.Back(); iter != nil; iter = iter.Prev() {
		if v == iter.Value {
			return true
		}
	}
	return false
}

func (q *List) ContainBy(v interface{}, f func(left interface{}, right interface{}) bool) bool {
	defer q.lock.Unlock()
	q.lock.Lock()
	for iter := q.data.Back(); iter != nil; iter = iter.Prev() {
		if f(v, iter.Value) {
			return true
		}
	}
	return false
}
