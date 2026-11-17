package list

import (
	"errors"
	"fmt"
	"testing"
)

func TestNew1(t *testing.T) {
	rob := NewROBuffer(10, 1, 20)
	rob.Set(1, 1)
	rob.Set(1, 1)
	err := rob.Set(10, 10)
	if err != nil {
		fmt.Println(err)
	}
	err = rob.Set(11, 11)
	if err != nil {
		fmt.Println(err)
	}
	err = rob.Set(13, 13)
	if err != nil {
		fmt.Println(err)
	}
	err = rob.Set(19, 19)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(rob.size)
	for e := rob.FrontInter(); e != nil; e = e.Next() {
		fmt.Println(e.Value)
	}

	err, d := rob.Front()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(d)
	rob.PopFront()

	err, _ = rob.Front()
	if err != nil {
		fmt.Println(err)
	}

	rob.Set(2, 2)
	rob.Set(3, 3)
	rob.Set(4, 4)
	rob.Set(5, 5)
	rob.Set(6, 6)
	rob.Set(7, 7)
	rob.Set(8, 8)
	rob.Set(9, 9)
	err = rob.Set(10, 10)
	if err != nil {
		fmt.Println(err)
	}
	err = rob.Set(11, 11)
	if err != nil {
		fmt.Println(err)
	}
	err = rob.Set(12, 12)
	if err != nil {
		fmt.Println(err)
	}

	err, _ = rob.Front()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(d)
	rob.PopFront()
	err = rob.Set(12, 12)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(rob.size)
	fmt.Println("---")
	for i := 0; i < 100; i++ {
		err, d := rob.Front()
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(d)
		rob.PopFront()
		err = rob.Set((13+i)%20, (13+i)%20)
		if err != nil {
			fmt.Println(err)
		}
	}
	fmt.Println("---")
	for e := rob.FrontInter(); e != nil; e = e.Next() {
		fmt.Println(e.Value)
	}
	fmt.Println(rob.Full())
}

func TestNew2(t *testing.T) {
	rob := NewROBuffer(10, 1, 20)
	rob.Set(1, 1)
	err, d := rob.Get(1)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(d)
	err, _ = rob.Get(11)
	if err != nil {
		fmt.Println(err)
	}
	err, d = rob.Get(2)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(d)
	fmt.Println(rob.Full())
}

func TestNew3(t *testing.T) {
	rob := NewROBuffer(5, 0, 10)
	rob.Set(0, 0)
	rob.Set(1, 1)
	rob.Set(2, 2)
	rob.Set(3, 3)
	rob.Set(4, 4)
	fmt.Println(rob.Full())
	err := rob.Set(5, 5)
	fmt.Println(err)
}

func TestNew4(t *testing.T) {
	rob := NewROBuffer(5, 0, 10)
	id := 0
	index := 0
	for i := 0; i < 100; i++ {
		for rob.Size() < 5 {
			err := rob.Set(id, index)
			index++
			if err != nil {
				fmt.Println(err)
			}
			id++
			if id >= 10 {
				id = 0
			}
		}

		for !rob.Empty() {
			err, value := rob.Front()
			if err != nil {
				fmt.Println(err)
			}
			fmt.Println(value)
			err = rob.PopFront()
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func TestNew5(t *testing.T) {
	for i := 0; i < 100; i++ {
		fmt.Printf("%d inrange %v isold %v\n", i,
			isIdInRange(0, 10, i, 100),
			isIdOld(0, 10, i, 100))
	}
}

func TestNew6(t *testing.T) {
	for i := 0; i < 100; i++ {
		fmt.Printf("%d inrange %v isold %v\n", i,
			isIdInRange(95, 10, i, 100),
			isIdOld(95, 10, i, 100))
	}
}

func TestNew7(t *testing.T) {
	for i := 0; i < 100; i++ {
		fmt.Printf("%d inrange %v isold %v\n", i,
			isIdInRange(55, 10, i, 100),
			isIdOld(55, 10, i, 100))
	}
}

func collectInterFromBegin(rob *ROBuffergo) []int {
	var got []int
	for e := rob.FrontInterFromBegin(); e != nil; e = e.Next() {
		got = append(got, e.Id())
	}
	return got
}

func TestROBufferFrontInterFromBeginEmpty(t *testing.T) {
	rob := NewROBuffer(5, 0, 10)
	if e := rob.FrontInterFromBegin(); e != nil {
		t.Fatalf("empty window should yield nil iterator, got id=%d", e.Id())
	}
}

func TestROBufferFrontInterFromBeginFilled(t *testing.T) {
	rob := NewROBuffer(5, 0, 10)
	for _, id := range []int{0, 1, 2, 3, 4} {
		if err := rob.Set(id, id*10); err != nil {
			t.Fatal(err)
		}
	}
	got := collectInterFromBegin(rob)
	want := []int{0, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	e := rob.FrontInterFromBegin()
	if e.Id() != 0 || e.Value.(int) != 0 {
		t.Fatalf("first element id=%d value=%v", e.Id(), e.Value)
	}
}

func TestROBufferFrontInterFromBeginHoleAtBegin(t *testing.T) {
	// The begin slot is empty but later slots are filled. FrontInter gives
	// up; FrontInterFromBegin must still iterate the arrived slots.
	rob := NewROBuffer(5, 0, 10)
	if err := rob.Set(2, 20); err != nil {
		t.Fatal(err)
	}
	if err := rob.Set(3, 30); err != nil {
		t.Fatal(err)
	}
	if e := rob.FrontInter(); e != nil {
		t.Fatalf("FrontInter must be nil when begin is a hole, got id=%d", e.Id())
	}
	got := collectInterFromBegin(rob)
	want := []int{2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestROBufferFrontInterFromBeginOnlyLastSlotFilled(t *testing.T) {
	// The physical slot right before begin must not be skipped.
	rob := NewROBuffer(5, 0, 10)
	if err := rob.Set(4, 40); err != nil {
		t.Fatal(err)
	}
	got := collectInterFromBegin(rob)
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("got %v want [4]", got)
	}
}

func TestROBufferFrontInterFromBeginAfterPop(t *testing.T) {
	rob := NewROBuffer(5, 0, 10)
	for _, id := range []int{0, 1, 2} {
		if err := rob.Set(id, id*10); err != nil {
			t.Fatal(err)
		}
	}
	if err := rob.PopFront(); err != nil {
		t.Fatal(err)
	}
	if err := rob.PopFront(); err != nil {
		t.Fatal(err)
	}
	// begin now expects id 2, which has arrived; the two reused slots are
	// holes for the future window.
	got := collectInterFromBegin(rob)
	want := []int{2}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v want %v", got, want)
	}

	// After consuming id 2, begin expects id 3. Arrive 4 and 5 while 3 is
	// missing: begin is a hole and iteration must still reach 4, 5.
	if err := rob.PopFront(); err != nil {
		t.Fatal(err)
	}
	if err := rob.Set(4, 40); err != nil {
		t.Fatal(err)
	}
	if err := rob.Set(5, 50); err != nil {
		t.Fatal(err)
	}
	got = collectInterFromBegin(rob)
	want = []int{4, 5}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func isIdInRange(recvid int, windowsize int, id int, maxid int) bool {
	begin := recvid
	end := recvid + windowsize
	if end >= maxid {
		if id >= 0 && id < end-maxid {
			return true
		}
		end = maxid
	}
	if id >= begin && id < end {
		return true
	}
	return false
}

func isIdOld(recvid int, windowsize int, id int, maxid int) bool {
	begin := recvid - windowsize
	if begin < 0 {
		begin += maxid
	}
	end := recvid

	if begin < end {
		return id >= begin && id < end
	} else {
		return (id >= begin && id < maxid) || (id >= 0 && id < end)
	}
}

// 已被 ACK 弹出的槽位会把自己的环 id 改写为 cur+len-1，此时旧 id 的
// Get 必须报告 ErrOutOfWin，而不是命中仍在使用的槽位后报内部不一致。
// 线上表现就是 "set id error 21 20021 21" 这类噪声日志。
func TestGetStaleIdReturnsOutOfWin(t *testing.T) {
	const win, maxid = 20000, 1000000
	b := NewROBuffer(win, 0, maxid)
	for id := 0; id <= 21; id++ {
		if err := b.Set(id, id); err != nil {
			t.Fatalf("set %d: %v", id, err)
		}
	}
	for i := 0; i <= 21; i++ {
		if err := b.PopFront(); err != nil {
			t.Fatalf("pop %d: %v", i, err)
		}
	}

	err, v := b.Get(21)
	if !errors.Is(err, ErrOutOfWin) {
		t.Fatalf("stale get want ErrOutOfWin, got err=%v value=%v", err, v)
	}
	if v != nil {
		t.Fatalf("stale get want nil value, got %v", v)
	}

	// 窗口前端 22 仍在窗口内且为空槽：应当是 nil,nil。
	err, v = b.Get(22)
	if err != nil || v != nil {
		t.Fatalf("front slot want (nil,nil), got err=%v value=%v", err, v)
	}
}

// 超前到窗口末端之外的 id 同样属于越界。
func TestGetAheadIdReturnsOutOfWin(t *testing.T) {
	b := NewROBuffer(4, 0, 100)
	if err := b.Set(0, 0); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err, _ := b.Get(4); !errors.Is(err, ErrOutOfWin) {
		t.Fatalf("ahead get want ErrOutOfWin, got %v", err)
	}
	// 窗口内的空槽不算越界。
	if err, v := b.Get(3); err != nil || v != nil {
		t.Fatalf("in-win empty slot want (nil,nil), got err=%v value=%v", err, v)
	}
}

// Set 只允许写入窗口内 id，落后于前端时同样报 ErrOutOfWin。
func TestSetBehindReturnsOutOfWin(t *testing.T) {
	b := NewROBuffer(4, 0, 100)
	for id := 0; id < 4; id++ {
		if err := b.Set(id, id); err != nil {
			t.Fatalf("set %d: %v", id, err)
		}
	}
	if err := b.PopFront(); err != nil {
		t.Fatalf("pop: %v", err)
	}
	if err := b.Set(0, 0); !errors.Is(err, ErrOutOfWin) {
		t.Fatalf("set behind want ErrOutOfWin, got %v", err)
	}
	if err := b.Set(4, 4); err != nil {
		t.Fatalf("set front+len-1 should be in win: %v", err)
	}
}

// 小 maxid 下窗口跨过环回点后，窗口内 id 仍可读写，窗口外仍报越界。
func TestWindowWrapAroundBoundary(t *testing.T) {
	const maxid = 10
	b := NewROBuffer(4, 0, maxid)
	for id := 0; id < 8; id++ {
		if err := b.Set(id, id); err != nil {
			t.Fatalf("set %d: %v", id, err)
		}
		if err := b.PopFront(); err != nil {
			t.Fatalf("pop %d: %v", id, err)
		}
	}

	// 窗口覆盖 8,9,0,1
	for _, id := range []int{8, 9, 0, 1} {
		if err := b.Set(id, id*10); err != nil {
			t.Fatalf("set in win %d: %v", id, err)
		}
	}
	for _, id := range []int{8, 9, 0, 1} {
		err, v := b.Get(id)
		if err != nil || v != id*10 {
			t.Fatalf("get in win %d: err=%v value=%v", id, err, v)
		}
	}

	if err, _ := b.Get(7); !errors.Is(err, ErrOutOfWin) {
		t.Fatalf("stale across wrap want ErrOutOfWin, got %v", err)
	}
	if err, _ := b.Get(2); !errors.Is(err, ErrOutOfWin) {
		t.Fatalf("ahead across wrap want ErrOutOfWin, got %v", err)
	}
}
