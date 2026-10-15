package pool

import "testing"

/*
本文件针对 pool 里手写的两个池做模糊测试：对象的复用与归还。
断言的都是"池的语义"，而不是重新实现一遍池：
  - 同一个对象在"使用中"期间绝不能被第二次分配出去；
  - 池必须真的在复用（分配出去的对象总数不超过并发持有的峰值）；
  - 重复释放、释放不属于池的对象、空池释放都必须是无副作用的。
*/

// FuzzPoolAllocFree 用一串 alloc/free 操作驱动对象池。allocf 每次返回递增的
// 唯一整数，于是"一个值是否被重复发放"可以直接看出来。
func FuzzPoolAllocFree(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{255, 255, 0, 0})
	f.Add([]byte{2, 2, 2, 2, 3, 3})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 256 {
			ops = ops[:256]
		}
		allocated := 0
		p := New(func() interface{} {
			allocated++
			return allocated
		})

		var held []*PoolElement
		inuse := make(map[int]bool)
		peak := 0

		for _, b := range ops {
			if b&1 == 0 {
				pe := p.Alloc()
				if pe == nil {
					t.Fatal("Alloc returned nil")
				}
				v, ok := pe.Value.(int)
				if !ok {
					t.Fatalf("Alloc handed out unexpected value %v", pe.Value)
				}
				if inuse[v] {
					t.Fatalf("value %d handed out twice while still in use", v)
				}
				inuse[v] = true
				held = append(held, pe)
				if len(held) > peak {
					peak = len(held)
				}
			} else {
				if len(held) == 0 {
					// 空池释放与释放不属于池的对象都必须无副作用
					p.Free(nil)
					if p.UsedSize() != 0 {
						t.Fatalf("Free(nil) changed used size to %d", p.UsedSize())
					}
					continue
				}
				i := int(b>>1) % len(held)
				pe := held[i]
				held = append(held[:i], held[i+1:]...)
				delete(inuse, pe.Value.(int))
				p.Free(pe)
				// 重复释放必须是无副作用的
				p.Free(pe)
			}

			if got := p.UsedSize(); got != len(held) {
				t.Fatalf("UsedSize = %d, want %d", got, len(held))
			}
			if p.FreeSize() < 0 {
				t.Fatalf("FreeSize = %d", p.FreeSize())
			}
		}

		// 池必须真的在复用：从未同时持有的对象数超过峰值，就说明归还没生效。
		if allocated > peak {
			t.Fatalf("pool allocated %d distinct objects but at most %d were in use at once", allocated, peak)
		}
	})
}

// FuzzTokenPoolAcquireRelease 驱动令牌池。注意 Acquire 在池空时会阻塞，
// 所以用例只在持有数小于容量时才获取——这既避免 fuzz 卡死，也正好覆盖了
// "取满再还"的边界。
func FuzzTokenPoolAcquireRelease(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3}, byte(4))
	f.Add([]byte{0, 0, 0, 0, 1, 1}, byte(1))
	f.Add([]byte{}, byte(8))

	f.Fuzz(func(t *testing.T, ops []byte, cap byte) {
		if len(ops) > 256 {
			ops = ops[:256]
		}
		// 容量必须 >= 1：NewTokenPool(0) 会得到一个永远不会就绪的池。
		max := int(cap)%8 + 1
		rp := NewTokenPool(max)
		held := make(map[int]bool)

		for _, b := range ops {
			if b&1 == 0 && len(held) < max {
				id := rp.Acquire()
				if id < 0 || id >= max {
					t.Fatalf("Acquire returned %d, want range [0,%d)", id, max)
				}
				if held[id] {
					t.Fatalf("token %d handed out twice", id)
				}
				held[id] = true
				continue
			}
			if len(held) == 0 {
				continue
			}
			for id := range held {
				delete(held, id)
				rp.Release(id)
				break
			}
		}
	})
}
