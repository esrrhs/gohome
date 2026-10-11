package list

import (
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// RBuffergo：环形回绕的边界分支与数据完整性
// ===========================================================================

// TestHardenRBufferWrapWriteRead 覆盖 Write/Read 跨物理末尾的两个回绕拷贝分支，
// 旧测试只打印不对账，回绕后字节错位无法被发现。
func TestHardenRBufferWrapWriteRead(t *testing.T) {
	// 带锁模式：datasize 耗尽时不会把 begin/end 复位为 0，容易造回绕
	rb := NewRBuffergo(5, true)

	if !rb.Write([]byte{0, 1, 2}) {
		t.Fatal("first write must succeed")
	}
	got := make([]byte, 3)
	if !rb.Read(got) || string(got) != string([]byte{0, 1, 2}) {
		t.Fatalf("read back = %v, ok path", got)
	}
	// begin=3,end=3,datasize=0（带锁不复位）
	// 再写 3 字节：线性段只剩 2 字节，触发 Write 的回绕双段拷贝
	if !rb.Write([]byte{3, 4, 5}) {
		t.Fatal("wrap write must succeed")
	}
	if rb.Size() != 3 {
		t.Fatalf("size = %d, want 3", rb.Size())
	}
	// begin(3) >= end(1)：读取也要跨物理末尾
	wrapRead := make([]byte, 3)
	if !rb.Read(wrapRead) {
		t.Fatal("wrap read must succeed")
	}
	if string(wrapRead) != string([]byte{3, 4, 5}) {
		t.Fatalf("wrap read = %v, want [3 4 5]", wrapRead)
	}
	if rb.Size() != 0 {
		t.Fatalf("size after drain = %d, want 0", rb.Size())
	}

	// 容量/数据不足必须拒绝
	if rb.Write(make([]byte, 6)) {
		t.Error("write beyond capacity must fail")
	}
	if rb.Read(make([]byte, 1)) {
		t.Error("read on empty buffer must fail")
	}
}

// TestHardenRBufferSkipWrapAndReset 无锁模式下 SkipWrite/SkipRead 回绕，
// 以及读空后 begin/end 复位为 0 的分支。
func TestHardenRBufferSkipWrapAndReset(t *testing.T) {
	rb := NewRBuffergo(5, false)

	// 直接跳过整个容量：end 从 5 回绕到 0
	rb.SkipWrite(5)
	if !rb.Full() || rb.Size() != 5 {
		t.Fatalf("after SkipWrite(5): size=%d full=%v", rb.Size(), rb.Full())
	}
	// 超容量跳过是 no-op
	rb.SkipWrite(1)
	if rb.Size() != 5 {
		t.Fatalf("over-capacity SkipWrite must be no-op, size=%d", rb.Size())
	}
	// 消费超过持有量是 no-op
	rb.SkipRead(6)
	if rb.Size() != 5 {
		t.Fatalf("over-size SkipRead must be no-op, size=%d", rb.Size())
	}
	// 全部消费：begin 回绕到 0，且无锁模式复位 begin=end=0
	rb.SkipRead(5)
	if rb.Size() != 0 || rb.begin != 0 || rb.end != 0 {
		t.Fatalf("drain must reset pointers: size=%d begin=%d end=%d", rb.Size(), rb.begin, rb.end)
	}

	// Read 路径同样需要在无锁模式读空时复位
	if !rb.Write([]byte{1, 2}) {
		t.Fatal("write after reset")
	}
	two := make([]byte, 2)
	if !rb.Read(two) || two[0] != 1 || two[1] != 2 {
		t.Fatalf("read after reset = %v", two)
	}
	if rb.begin != 0 || rb.end != 0 {
		t.Fatalf("read-drain must reset pointers: begin=%d end=%d", rb.begin, rb.end)
	}

	// 带锁模式读空不复位（指针继续前进），覆盖 lock==nil 判断的 false 分支
	locked := NewRBuffergo(5, true)
	locked.Write([]byte{9})
	one := make([]byte, 1)
	locked.Read(one)
	if locked.begin != 1 || locked.end != 1 {
		t.Fatalf("locked buffer must not reset on drain: begin=%d end=%d", locked.begin, locked.end)
	}
}

// TestHardenRBufferLineBuffers 取线性读写窗口在两种布局下的长度与内容。
func TestHardenRBufferLineBuffers(t *testing.T) {
	rb := NewRBuffergo(8, true)
	rb.Write([]byte{0, 1, 2, 3, 4, 5})
	rb.Read(make([]byte, 4))     // begin=4，剩 [4,5]
	rb.Write([]byte{6, 7, 8, 9}) // 回绕写：物理布局 [8,9,_,_,4,5,6,7]

	// [2] 布局：datasize(6) >= len-begin(4)，走 else，
	// 只返回 begin 到物理末尾的连续段
	rline := rb.GetReadLineBuffer()
	if len(rline) != 4 || string(rline) != string([]byte{4, 5, 6, 7}) {
		t.Fatalf("read line = %v, want [4 5 6 7]", rline)
	}

	// [2] 布局空闲区在 [end,begin)：free=2 < len-end=6，走 if 分支
	wline := rb.GetWriteLineBuffer()
	if len(wline) != 2 {
		t.Fatalf("write line len = %d, want 2", len(wline))
	}
	// 该窗口确实落在物理槽 2..3
	if &wline[0] != &rb.GetBuffer()[2] {
		t.Fatal("write line must point at slots [end,begin)")
	}

	// 空缓冲：读窗口为空切片，写窗口为整段
	empty := NewRBuffergo(5, true)
	if len(empty.GetReadLineBuffer()) != 0 {
		t.Error("empty read line must be zero-length")
	}
	if len(empty.GetWriteLineBuffer()) != 5 {
		t.Errorf("empty write line len = %d, want 5", len(empty.GetWriteLineBuffer()))
	}
}

// TestHardenRBufferSkipWriteLocked 带锁模式 SkipWrite 的加锁与 end 回绕分支。
func TestHardenRBufferSkipWriteLocked(t *testing.T) {
	rb := NewRBuffergo(5, true)
	rb.Write([]byte{1, 2, 3})
	rb.Read(make([]byte, 3)) // begin=end=3，datasize=0，带锁不复位
	rb.SkipWrite(3)          // end=6 回绕到 1
	if rb.Size() != 3 || rb.end != 1 {
		t.Fatalf("after locked SkipWrite: size=%d end=%d", rb.Size(), rb.end)
	}
	if !rb.CanRead(3) || rb.CanRead(4) {
		t.Error("skipped bytes must be readable by size accounting")
	}
}

// TestHardenRBufferConcurrent 带锁缓冲在一写一读加观察者下 -race 无竞争。
func TestHardenRBufferConcurrent(t *testing.T) {
	rb := NewRBuffergo(64, true)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if rb.CanWrite(4) {
				rb.Write([]byte{1, 2, 3, 4})
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if rb.CanRead(4) {
				rb.SkipRead(4)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = rb.Size()
			_ = rb.Full()
			_ = rb.Empty()
			_ = rb.Capacity()
			_ = rb.GetReadLineBuffer()
			_ = rb.GetWriteLineBuffer()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	if s := rb.Size(); s < 0 || s > 64 {
		t.Fatalf("invariant broken: size=%d", s)
	}
}

// ===========================================================================
// Rlistgo：空/满错误、回绕与 FIFO 顺序
// ===========================================================================

func TestHardenRListWrapAndErrors(t *testing.T) {
	l := NewRList(3)

	if err, v := l.Front(); err == nil || v != nil {
		t.Fatalf("Front on empty = %v, %v", err, v)
	}
	if err := l.PopFront(); err == nil {
		t.Fatal("PopFront on empty must error")
	}

	for _, v := range []int{1, 2, 3} {
		if err := l.PushBack(v); err != nil {
			t.Fatalf("push %d: %v", v, err)
		}
	}
	if err := l.PushBack(4); err == nil {
		t.Fatal("PushBack on full must error")
	}
	if !l.Full() || l.Size() != 3 || l.Capacity() != 3 {
		t.Fatal("list must be full with 3 items")
	}
	if err, v := l.Front(); err != nil || v.(int) != 1 {
		t.Fatalf("Front = %v, %v", err, v)
	}

	// 弹出 2 个再压入，物理下标发生回绕
	if err := l.PopFront(); err != nil {
		t.Fatal(err)
	}
	if err := l.PopFront(); err != nil {
		t.Fatal(err)
	}
	if err := l.PushBack(4); err != nil {
		t.Fatal(err)
	}
	if err := l.PushBack(5); err != nil {
		t.Fatal(err)
	}

	// FIFO 顺序必须是 3,4,5，且跨物理末尾
	var got []int
	for e := l.FrontInter(); e != nil; e = e.Next() {
		got = append(got, e.Value.(int))
	}
	if len(got) != 3 || got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("wrap iteration = %v, want [3 4 5]", got)
	}

	// 清空（注意 Rlistgo 耗尽后 begin 不复位，只由 size 判定空）
	for i := 0; i < 3; i++ {
		if err := l.PopFront(); err != nil {
			t.Fatal(err)
		}
	}
	if !l.Empty() || l.Size() != 0 {
		t.Fatalf("after drain: empty=%v size=%d", l.Empty(), l.Size())
	}
	// 被弹出的槽必须释放为 nil，防止接口持有大对象
	for _, slot := range l.buffer {
		if slot != nil {
			t.Fatalf("popped slot leaked %v", slot)
		}
	}
}

// ===========================================================================
// ROBuffergo：构造守卫、Get/Set 错误分支、PopFront id 回绕
// ===========================================================================

func TestHardenROBufferCtorGuards(t *testing.T) {
	if NewROBuffer(5, 5, 5) != nil {
		t.Error("startid >= maxid must return nil")
	}
	if NewROBuffer(10, 0, 10) != nil {
		t.Error("len >= maxid must return nil")
	}
	if b := NewROBuffer(5, 0, 10); b == nil {
		t.Error("valid params must succeed")
	}
}

func TestHardenROBufferGetSetErrors(t *testing.T) {
	// 零值对象（未构造）：所有入口都应返回 not init 而不是 panic
	var zero ROBuffergo
	if err, _ := zero.Get(0); err == nil {
		t.Error("Get on zero-value must error")
	}
	if err := zero.Set(0, 1); err == nil {
		t.Error("Set on zero-value must error")
	}
	if err, _ := zero.Front(); err == nil {
		t.Error("Front on zero-value must error")
	}
	if zero.FrontInter() != nil {
		t.Error("FrontInter on zero-value must be nil")
	}
	if zero.FrontInterFromBegin() != nil {
		t.Error("FrontInterFromBegin on zero-value must be nil")
	}

	b := NewROBuffer(3, 0, 10)

	// 空窗口取 Front / PopFront
	if err, _ := b.Front(); err == nil {
		t.Error("Front on empty window must error")
	}
	if err := b.PopFront(); err == nil {
		t.Error("PopFront on empty window must error")
	}

	// nil 数据拒绝
	if err := b.Set(0, nil); err == nil {
		t.Error("Set nil data must error")
	}
	// id 越界（== maxid）
	if err := b.Set(10, 10); err == nil {
		t.Error("Set id == maxid must error")
	}
	if err, _ := b.Get(10); err == nil {
		t.Error("Get id == maxid must error")
	}
	// 未填充槽位：无错误、无数据
	if err, v := b.Get(0); err != nil || v != nil {
		t.Fatalf("unfilled Get = %v, %v", err, v)
	}

	// 正常填充 + 同 id 覆盖不重复计数
	if err := b.Set(0, 100); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(0, 101); err != nil {
		t.Fatal(err)
	}
	if b.Size() != 1 {
		t.Fatalf("overwrite must not grow size, got %d", b.Size())
	}
	if err, v := b.Get(0); err != nil || v.(int) != 101 {
		t.Fatalf("Get after overwrite = %v, %v", v, err)
	}
	if err, v := b.Front(); err != nil || v.(int) != 101 {
		t.Fatalf("Front = %v, %v", v, err)
	}

	// 窗口前移后，落在旧槽位上的远端 id 与槽位 id 不匹配
	if err := b.PopFront(); err != nil { // begin 指向原槽1（id=1）
		t.Fatal(err)
	}
	if err, _ := b.Get(5); err == nil {
		t.Error("Get id whose slot carries a different id must error")
	}
}

// TestHardenROBufferGetIdWrap 窗口 cur 较大、查询 id 回绕到 cur 之前时，
// 走 id+maxid-cur 的下标公式，必须正确取到跨代数据。
func TestHardenROBufferGetIdWrap(t *testing.T) {
	b := NewROBuffer(5, 0, 10)
	// 跑满一代：0..4 全部填充并弹出，槽位 id 推进为 [5,6,7,8,9]
	for i := 0; i < 5; i++ {
		if err := b.Set(i, i); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := b.PopFront(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 5; i <= 9; i++ {
		if err := b.Set(i, i*10); err != nil {
			t.Fatal(err)
		}
	}
	// 弹到 begin=4（cur=9），槽位 id 为 [0,1,2,3,9]
	for i := 0; i < 4; i++ {
		if err := b.PopFront(); err != nil {
			t.Fatal(err)
		}
	}
	// id=0 回绕在物理槽0，小于 cur=9：命中 id<cur 下标公式
	if err := b.Set(0, 999); err != nil {
		t.Fatal(err)
	}
	if err, v := b.Get(0); err != nil || v.(int) != 999 {
		t.Fatalf("wrapped Get(0) = %v, %v", v, err)
	}
	// 空洞 id=1 未填充：无错误无数据
	if err, v := b.Get(1); err != nil || v != nil {
		t.Fatalf("unfilled wrapped Get(1) = %v, %v", v, err)
	}
}

// TestHardenROBufferNilDataDefense 正常 API 无法制造 flag=true 但 buffer=nil
// 的状态（Set 拒绝 nil），白箱置空后三个入口必须返回防御错误而不是 panic。
func TestHardenROBufferNilDataDefense(t *testing.T) {
	b := NewROBuffer(3, 0, 10)
	if err := b.Set(0, 1); err != nil {
		t.Fatal(err)
	}
	b.buffer[0] = nil

	if err, v := b.Get(0); err == nil || v != nil {
		t.Fatalf("Get on nil-data slot = %v, %v", err, v)
	}
	if err, v := b.Front(); err == nil || v != nil {
		t.Fatalf("Front on nil-data slot = %v, %v", err, v)
	}
	if err := b.PopFront(); err == nil {
		t.Fatal("PopFront on nil-data slot must error and must not advance begin")
	}
	if b.begin != 0 || b.Size() != 1 {
		t.Fatalf("failed PopFront must not move window: begin=%d size=%d", b.begin, b.Size())
	}
}

// TestHardenROBufferPopIdWrap 验证 PopFront 后释放槽位的 id 递推与
// 越过 maxid 时的取模回绕（帧号空间复用的核心不变量）。
func TestHardenROBufferPopIdWrap(t *testing.T) {
	// len=3,maxid=5,start=0：槽位 id 初始 0,1,2
	b := NewROBuffer(3, 0, 5)
	for i := 0; i < 3; i++ {
		if err := b.Set(i, i*10); err != nil {
			t.Fatal(err)
		}
	}

	// pop 0：新 begin 是槽1(cur=1)，旧槽0 拿到 1+3-1=3
	if err := b.PopFront(); err != nil {
		t.Fatal(err)
	}
	if b.id[0] != 3 {
		t.Fatalf("freed slot id = %d, want 3", b.id[0])
	}
	// pop 1：新 begin 是槽2(cur=2)，旧槽1 拿到 2+3-1=4
	if err := b.PopFront(); err != nil {
		t.Fatal(err)
	}
	if b.id[1] != 4 {
		t.Fatalf("freed slot id = %d, want 4", b.id[1])
	}
	// pop 2：begin 物理回绕到槽0(cur=3)，旧槽2 拿到 3+3-1=5 → 取模为 0
	if err := b.PopFront(); err != nil {
		t.Fatal(err)
	}
	if b.id[2] != 0 || b.begin != 0 || !b.Empty() {
		t.Fatalf("wrap state: id[2]=%d begin=%d empty=%v", b.id[2], b.begin, b.Empty())
	}

	// 复用的新 id 3 可正常写入读取
	if err := b.Set(3, 30); err != nil {
		t.Fatal(err)
	}
	if err, v := b.Get(3); err != nil || v.(int) != 30 {
		t.Fatalf("Get(3) after wrap = %v, %v", v, err)
	}
}

// TestHardenROBufferIteratorAcrossWrap 窗口在物理环上回绕、含空洞时，
// 迭代器仍按 id 顺序只产出已填充槽位。
func TestHardenROBufferIteratorAcrossWrap(t *testing.T) {
	// len=4,maxid=20：先填满再弹出两个，让 begin 移到槽2
	b := NewROBuffer(4, 0, 20)
	for i := 0; i < 4; i++ {
		if err := b.Set(i, i); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := b.PopFront(); err != nil {
			t.Fatal(err)
		}
	}
	// begin 现期待 id=2；补 id=4（槽0），id=3 缺失形成空洞
	if err := b.Set(4, 4); err != nil {
		t.Fatal(err)
	}
	var got []int
	for e := b.FrontInterFromBegin(); e != nil; e = e.Next() {
		got = append(got, e.Id())
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("iteration across wrapped window = %v, want [2 3 4]", got)
	}
}

// ===========================================================================
// synclist：空 Pop 与并发
// ===========================================================================

func TestHardenSyncListEmptyPopAndConcurrent(t *testing.T) {
	q := NewList()
	if q.Pop() != nil {
		t.Error("Pop on empty list must return nil")
	}
	for _, v := range []int{1, 2, 3} {
		q.Push(v)
	}
	if q.Len() != 3 {
		t.Fatalf("len = %d, want 3", q.Len())
	}
	if !q.Contain(2) || q.Contain(9) {
		t.Error("Contain mismatch")
	}
	if !q.ContainBy(4, func(left, right interface{}) bool {
		return left.(int) > right.(int)
	}) {
		t.Error("ContainBy should find an element smaller than 4")
	}
	var sum int
	q.Range(func(v interface{}) { sum += v.(int) })
	if sum != 6 {
		t.Errorf("range sum = %d, want 6", sum)
	}
	// PushFront 入、Back 出：FIFO 顺序
	if v := q.Pop(); v != 1 {
		t.Errorf("pop order = %v, want 1", v)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				q.Push(i)
				q.Pop()
				_ = q.Len()
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
