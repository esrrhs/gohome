package list

import "sync"

/*
RBuffergo 实现了一个线程安全的循环缓冲区（Ring Buffer），用于高效地存储和读取字节数据。
该缓冲区支持并发访问，并提供对数据的读写操作，以及对缓冲区状态的管理。

主要功能包括：

- 创建固定大小的循环缓冲区，并支持可选的锁机制以确保线程安全
- 检查缓冲区是否可以写入或读取指定数量的数据
- 将字节数据写入到缓冲区，同时处理循环缓冲的逻辑
- 从缓冲区读取字节数据，并管理读指针
- 提供存储和恢复缓冲区状态的功能
- 提供清空缓冲区、获取缓冲区大小和容量的功能
- 允许获取当前读写的数据缓冲区
*/

/*
type:		   [1]
iter:	 begin(2)	 end(8)
			|		   |
data:   _ _ * * * * * * _ _ _
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
type:		   [2]
iter:	  end(2)   begin(7)
			|		 |
data:   * * _ _ _ _ _ * * * *
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
type:		   [3]
iter:	  begin(4),end(4)
				|
data:   _ _ _ _ _ _ _ _ _ _ _
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
type:		   [4]
iter:	  begin(4),end(4)
|				 |
data:   * * * * * * * * * * *
buffer: _ _ _ _ _ _ _ _ _ _ _
index:  0 1 2 3 4 5 6 7 8 9 10
*/

type RBuffergo struct {
	buffer        []byte
	capacity      int
	datasize      int
	begin         int
	end           int
	storeDatasize int
	storeBegin    int
	storeEnd      int
	lock          sync.Locker
}

const rbufferEagerThreshold = 4096

func NewRBuffergo(len int, lock bool) *RBuffergo {
	if len < 0 {
		len = 0
	}
	buffer := &RBuffergo{
		capacity: len,
	}
	// Small ring buffers allocate upfront; large megabyte-scale session buffers
	// allocate lazily on first write so unauthenticated pre-handshake sessions
	// cannot trigger multi-megabyte amplification per packet.
	if len <= rbufferEagerThreshold {
		buffer.buffer = make([]byte, len)
	}
	if lock {
		buffer.lock = &sync.Mutex{}
	}
	return buffer
}

func (b *RBuffergo) ensureBuffer() {
	if b.buffer == nil && b.capacity > 0 {
		b.buffer = make([]byte, b.capacity)
	}
}

func (b *RBuffergo) CanWrite(size int) bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}
	if size < 0 {
		return false
	}
	return b.datasize+size <= b.capacity
}

func (b *RBuffergo) SkipWrite(size int) {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if size <= 0 || !(b.datasize+size <= b.capacity) {
		return
	}
	b.ensureBuffer()

	b.datasize += size
	b.end += size
	if b.end >= b.capacity {
		b.end -= b.capacity
	}
}

func (b *RBuffergo) Write(data []byte) bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if !(b.datasize+len(data) <= b.capacity) {
		return false
	}
	if len(data) == 0 {
		return true
	}
	b.ensureBuffer()
	// [1][3]
	if b.end >= b.begin {
		// 能装下
		if b.capacity-b.end >= len(data) {
			copy(b.buffer[b.end:], data)
		} else {
			copy(b.buffer[b.end:], data[0:b.capacity-b.end])
			copy(b.buffer, data[b.capacity-b.end:])
		}
	} else /*[2]*/ {
		copy(b.buffer[b.end:], data)
	}

	b.datasize += len(data)
	b.end += len(data)
	if b.end >= b.capacity {
		b.end -= b.capacity
	}

	return true
}

func (b *RBuffergo) CanRead(size int) bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}
	if size < 0 {
		return false
	}

	return b.datasize >= size
}

func (b *RBuffergo) SkipRead(size int) {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if size < 0 || !(b.datasize >= size) {
		return
	}

	b.datasize -= size
	if b.capacity > 0 {
		b.begin += size
		if b.begin >= b.capacity {
			b.begin -= b.capacity
		}
	}

	if b.lock == nil {
		if b.datasize == 0 {
			b.begin = 0
			b.end = 0
		}
	}
}

func (b *RBuffergo) Read(data []byte) bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if !(b.datasize >= len(data)) {
		return false
	}
	if len(data) == 0 {
		return true
	}
	b.ensureBuffer()

	// [2][4]
	if b.begin >= b.end {
		// 能读完
		if b.capacity-b.begin >= len(data) {
			copy(data, b.buffer[b.begin:])
		} else {
			copy(data[0:b.capacity-b.begin], b.buffer[b.begin:])
			copy(data[b.capacity-b.begin:], b.buffer)
		}
	} else /* [1]*/ {
		copy(data, b.buffer[b.begin:])
	}

	b.datasize -= len(data)
	b.begin += len(data)
	if b.begin >= b.capacity {
		b.begin -= b.capacity
	}

	if b.lock == nil {
		if b.datasize == 0 {
			b.begin = 0
			b.end = 0
		}
	}

	return true
}

func (b *RBuffergo) Store() {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	b.storeDatasize = b.datasize
	b.storeBegin = b.begin
	b.storeEnd = b.end
}

func (b *RBuffergo) Restore() {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	b.datasize = b.storeDatasize
	b.begin = b.storeBegin
	b.end = b.storeEnd
}

func (b *RBuffergo) Clear() {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	b.datasize = 0
	b.begin = 0
	b.end = 0
}

func (b *RBuffergo) Size() int {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	return b.datasize
}

func (b *RBuffergo) Capacity() int {
	return b.capacity
}

func (b *RBuffergo) Empty() bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	return b.datasize == 0
}

func (b *RBuffergo) Full() bool {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	return b.datasize == b.capacity
}

func (b *RBuffergo) GetReadLineBuffer() []byte {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if b.datasize == 0 {
		return nil
	}
	b.ensureBuffer()
	if b.datasize < b.capacity-b.begin {
		return b.buffer[b.begin : b.begin+b.datasize]
	} else {
		return b.buffer[b.begin:b.capacity]
	}
}

func (b *RBuffergo) GetWriteLineBuffer() []byte {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}

	if b.capacity == 0 || b.datasize == b.capacity {
		return nil
	}
	b.ensureBuffer()
	if b.capacity-b.datasize < b.capacity-b.end {
		return b.buffer[b.end : b.end+b.capacity-b.datasize]
	} else {
		return b.buffer[b.end:b.capacity]
	}
}

func (b *RBuffergo) GetBuffer() []byte {
	if b.lock != nil {
		b.lock.Lock()
		defer b.lock.Unlock()
	}
	b.ensureBuffer()
	return b.buffer
}
