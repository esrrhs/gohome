package list

import (
	"bytes"
	"testing"
)

// FuzzRBuffergo 对循环字节缓冲做 fuzzing：用一个字节队列作为参考模型，随机
// 容量与随机步长下反复写入、读空、再写入以制造指针环绕，验证 FIFO 顺序、
// 容量边界与 Size/Empty/Full/CanRead/CanWrite 的一致性。
func FuzzRBuffergo(f *testing.F) {
	f.Add([]byte("hello world"), uint8(8), uint8(3))
	f.Add([]byte(""), uint8(1), uint8(1))
	f.Add([]byte("abcdefghijklmnop"), uint8(4), uint8(4))
	f.Add(bytes.Repeat([]byte{0x5A}, 64), uint8(64), uint8(64))
	f.Add(bytes.Repeat([]byte{0x5A}, 64), uint8(1), uint8(7))
	f.Add([]byte("wrap around"), uint8(0), uint8(5))

	f.Fuzz(func(t *testing.T, data []byte, capacity uint8, step uint8) {
		// 无锁与加锁两条分支的内部状态机有差异（读空时是否复位指针），都跑
		for _, lock := range []bool{false, true} {
			buf := NewRBuffergo(int(capacity), lock)
			var model []byte

			n := int(step)
			if n < 1 {
				n = 1
			}

			// 三轮写满-读空，确保覆盖 begin/end 环绕的各种相对位置
			for round := 0; round < 3; round++ {
				for off := 0; off < len(data); {
					size := len(data) - off
					if size > n {
						size = n
					}
					chunk := data[off : off+size]

					want := len(model)+size <= int(capacity)
					if got := buf.Write(chunk); got != want {
						t.Fatalf("lock=%v cap=%d: Write(%d)=%v, want %v (size=%d)",
							lock, capacity, size, got, want, len(model))
					}
					if want {
						model = append(model, chunk...)
					}
					off += size

					if buf.Size() != len(model) {
						t.Fatalf("lock=%v cap=%d: Size()=%d, want %d",
							lock, capacity, buf.Size(), len(model))
					}
				}

				// 参考模型必须与缓冲区状态一致
				if buf.Empty() != (len(model) == 0) {
					t.Fatalf("lock=%v: Empty()=%v but model has %d bytes", lock, buf.Empty(), len(model))
				}
				if buf.Full() != (len(model) == int(capacity)) {
					t.Fatalf("lock=%v cap=%d: Full()=%v but model has %d bytes", lock, capacity, buf.Full(), len(model))
				}
				if buf.CanRead(len(model)) != true {
					t.Fatalf("lock=%v: CanRead(%d) must be true", lock, len(model))
				}
				if buf.CanWrite(1) != (len(model)+1 <= int(capacity)) {
					t.Fatalf("lock=%v: CanWrite(1) inconsistent with size %d", lock, len(model))
				}

				// 读空，逐段比对 FIFO 顺序
				out := make([]byte, n)
				for len(model) > 0 {
					size := len(model)
					if size > n {
						size = n
					}
					got := out[:size]
					if !buf.Read(got) {
						t.Fatalf("lock=%v: Read(%d) failed with size=%d", lock, size, buf.Size())
					}
					if !bytes.Equal(got, model[:size]) {
						t.Fatalf("lock=%v cap=%d: read %x, want %x", lock, capacity, got, model[:size])
					}
					model = model[size:]

					if buf.Size() != len(model) {
						t.Fatalf("lock=%v: Size()=%d after read, want %d", lock, buf.Size(), len(model))
					}
				}

				// 读空后不得再读出任何东西
				if buf.CanRead(1) {
					t.Fatalf("lock=%v: CanRead(1) must be false after draining", lock)
				}
			}
		}
	})
}

// FuzzRlistgo 对循环链表做 fuzzing：随机容量下逐项入队再逐项出队，验证 FIFO
// 顺序、Front 与 PopFront 的返回值、以及空/满边界的 error 约定。
func FuzzRlistgo(f *testing.F) {
	f.Add([]byte("hello"), uint8(8))
	f.Add([]byte(""), uint8(4))
	f.Add(bytes.Repeat([]byte{0x31}, 32), uint8(4))
	f.Add(bytes.Repeat([]byte{0x31}, 32), uint8(1))
	f.Add([]byte("overflow"), uint8(0))

	f.Fuzz(func(t *testing.T, data []byte, capacity uint8) {
		rl := NewRList(int(capacity))
		var model []byte

		for _, v := range data {
			want := len(model) < int(capacity)
			err := rl.PushBack(int(v))
			if (err == nil) != want {
				t.Fatalf("cap=%d: PushBack err=%v, want ok=%v (size=%d)", capacity, err, want, len(model))
			}
			if err == nil {
				model = append(model, v)
			}
			if rl.Size() != len(model) {
				t.Fatalf("cap=%d: Size()=%d, want %d", capacity, rl.Size(), len(model))
			}
			if rl.Full() != (len(model) == int(capacity)) {
				t.Fatalf("cap=%d: Full()=%v but size=%d", capacity, rl.Full(), len(model))
			}
		}

		for len(model) > 0 {
			err, front := rl.Front()
			if err != nil {
				t.Fatalf("cap=%d: Front() err=%v with size=%d", capacity, err, rl.Size())
			}
			if front != int(model[0]) {
				t.Fatalf("cap=%d: Front()=%v, want %d", capacity, front, model[0])
			}
			if err := rl.PopFront(); err != nil {
				t.Fatalf("cap=%d: PopFront() err=%v with size=%d", capacity, err, rl.Size())
			}
			model = model[1:]
			if rl.Size() != len(model) {
				t.Fatalf("cap=%d: Size()=%d after pop, want %d", capacity, rl.Size(), len(model))
			}
		}

		// 排空后 Front/PopFront 必须报错
		if !rl.Empty() {
			t.Fatalf("cap=%d: Empty() must be true after draining", capacity)
		}
		if err, _ := rl.Front(); err == nil {
			t.Fatalf("cap=%d: Front() on empty list must return error", capacity)
		}
		if err := rl.PopFront(); err == nil {
			t.Fatalf("cap=%d: PopFront() on empty list must return error", capacity)
		}
	})
}
