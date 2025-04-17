package list

import (
	"errors"
)

/*
Rlistgo 提供了循环链表的实现，允许在固定大小的缓冲区中存储和管理数据。
该缓冲区支持后进先出（FIFO）的数据操作，并提供对数据的访问和管理功能。

主要功能包括：

- 创建固定大小的循环链表
- 支持从链表前端访问和弹出数据
- 支持向链表后端添加数据
- 提供链表的当前大小和容量信息
- 提供检查链表是否已满或为空的功能
- 支持通过迭代器遍历链表中的元素
*/

/*
type:		   [1]
iter:	 begin(2)	  end(8)
			|		    |
data:   _ _ 1 2 3 4 5 6 _ _ _
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
type:		   [2]
iter:	  end(2)    begin(7)
			|		  |
data:   7 8 _ _ _ _ _ 3 4 5 6
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
*/

type Rlistgo struct {
	buffer []interface{}
	len    int
	maxid  int
	begin  int
	end    int
	size   int
}

func NewRList(len int) *Rlistgo {
	buffer := &Rlistgo{}
	buffer.buffer = make([]interface{}, len)
	buffer.len = len
	return buffer
}

func (b *Rlistgo) Front() (error, interface{}) {
	if b.Empty() {
		return errors.New("empty"), nil
	}
	return nil, b.buffer[b.begin]
}

func (b *Rlistgo) PopFront() error {
	if b.Empty() {
		return errors.New("empty")
	}
	old := b.begin
	b.begin++
	if b.begin >= b.len {
		b.begin = 0
	}
	b.buffer[old] = nil
	b.size--
	return nil
}

func (b *Rlistgo) PushBack(d interface{}) error {
	if b.Full() {
		return errors.New("full")
	}
	b.buffer[b.end] = d
	b.end++
	if b.end >= b.len {
		b.end = 0
	}
	b.size++
	return nil
}

func (b *Rlistgo) Size() int {
	return b.size
}

func (b *Rlistgo) Capacity() int {
	return b.len
}

func (b *Rlistgo) Full() bool {
	return b.size == b.len
}

func (b *Rlistgo) Empty() bool {
	return b.size <= 0
}

type RlistgoInter struct {
	n     int
	index int
	Value interface{}
	b     *Rlistgo
}

func (b *Rlistgo) FrontInter() *RlistgoInter {
	if b.Empty() {
		return nil
	}
	return &RlistgoInter{
		n:     0,
		index: b.begin,
		Value: b.buffer[b.begin],
		b:     b,
	}
}

func (bi *RlistgoInter) Next() *RlistgoInter {
	bi.index++
	bi.n++
	if bi.index >= bi.b.len {
		bi.index %= bi.b.len
	}
	if bi.n >= bi.b.size {
		return nil
	}
	bi.Value = bi.b.buffer[bi.index]
	return bi
}
