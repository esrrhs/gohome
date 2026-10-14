// 与 hash_contract_test.go 同源：本文件调用 crypto/cryptonight/inter 下的上游移植
// 实现，其中 sha3 在 amd64/386/ppc64le 上走 xor_unaligned.go 的 unsafe 非对齐指针
// 转换，Go 1.26 的 -race 内置 checkptr 会直接 fatal（CI 因此把 inter/ 排除在 race
// 门禁之外）。契约检查与并发无关，故只在非 race 下编译。

//go:build !race

package crypto

import (
	"bytes"
	"hash"
	"testing"

	"github.com/esrrhs/gohome/crypto/cryptonight/inter/skein"
	skein1024 "github.com/esrrhs/gohome/crypto/cryptonight/inter/skein/skein1024"
	skein256 "github.com/esrrhs/gohome/crypto/cryptonight/inter/skein/skein256"
)

// fuzzSplit 把 data 切成 segments 段依次写回给定的 write 回调。段数由 fuzz 引擎
// 控制，用来覆盖跨填充边界、跨块边界以及逐字节写入等分块形态。
func fuzzSplit(data []byte, segments int, write func([]byte)) {
	if len(data) == 0 {
		write(nil)
		return
	}
	if segments < 1 {
		segments = 1
	}
	if segments > len(data) {
		segments = len(data)
	}
	step := len(data) / segments
	if step < 1 {
		step = 1
	}
	for off := 0; off < len(data); off += step {
		end := off + step
		if end > len(data) {
			end = len(data)
		}
		write(data[off:end])
	}
}

// FuzzHashContract 对所有哈希原语做契约 fuzzing：任意消息在任意分块形态下都必须
// 与一次性写入等价，且 Sum 必须幂等、保留前缀、可在之后继续写入，Reset 必须回到
// 全新状态。历史上 jh（Write 短分包越界 panic）、skein（Sum 丢弃前缀）、groestl
// （Sum 污染接收者状态）都是这一类缺陷，此 target 用于自动发现同类问题。
func FuzzHashContract(f *testing.F) {
	f.Add([]byte(""), uint8(1))
	f.Add([]byte("a"), uint8(1))
	f.Add([]byte("abc"), uint8(3))
	f.Add(bytes.Repeat([]byte{0x41}, 63), uint8(2))
	f.Add(bytes.Repeat([]byte{0x41}, 64), uint8(2))
	f.Add(bytes.Repeat([]byte{0x41}, 65), uint8(2))
	f.Add(bytes.Repeat([]byte{0x41}, 128), uint8(4))
	f.Add(bytes.Repeat([]byte{0x41}, 1000), uint8(17))

	f.Fuzz(func(t *testing.T, data []byte, segments uint8) {
		for _, c := range contractPrimitives {
			// 基准：一次性写入
			base := c.new()
			base.Write(data)
			one := base.Sum(nil)

			if len(one) != base.Size() {
				t.Fatalf("%s: len(Sum(nil))=%d != Size()=%d", c.name, len(one), base.Size())
			}

			// 1. 分块写入（每段之间插入零长度写入）必须与一次性写入等价
			stream := c.new()
			fuzzSplit(data, int(segments), func(chunk []byte) {
				stream.Write(chunk)
				stream.Write(nil)
			})
			stream.Write([]byte{})
			if got := stream.Sum(nil); !bytes.Equal(got, one) {
				t.Fatalf("%s: segmented write %x != one-shot %x", c.name, got, one)
			}

			// 2. Sum 幂等（收尾必须在状态副本上进行）
			if got := base.Sum(nil); !bytes.Equal(got, one) {
				t.Fatalf("%s: Sum not idempotent: %x != %x", c.name, got, one)
			}

			// 3. Sum(b) 必须保留调用方前缀
			pfx := []byte("fuzz-prefix")
			if got := base.Sum(pfx); !bytes.Equal(got, append(append([]byte{}, pfx...), one...)) {
				t.Fatalf("%s: Sum(b)=%x, want prefix||digest", c.name, got)
			}

			// 4. Sum 之后继续写入
			cont := c.new()
			cont.Write(data)
			cont.Sum(nil)
			cont.Write([]byte("tail"))
			wantTail := c.new()
			wantTail.Write(append(append([]byte{}, data...), []byte("tail")...))
			if got := cont.Sum(nil); !bytes.Equal(got, wantTail.Sum(nil)) {
				t.Fatalf("%s: writing after Sum produced %x", c.name, got)
			}

			// 5. Reset 必须恢复到全新实例的状态
			reused := c.new()
			reused.Write([]byte("junk"))
			reused.Reset()
			reused.Write(data)
			if got := reused.Sum(nil); !bytes.Equal(got, one) {
				t.Fatalf("%s: Reset did not restore pristine state: %x != %x", c.name, got, one)
			}

			// 6. 空消息：Write(nil) 与从未写入过的实例必须一致
			wrote := c.new()
			wrote.Write(nil)
			fresh := c.new()
			if got, want := wrote.Sum(nil), fresh.Sum(nil); !bytes.Equal(got, want) {
				t.Fatalf("%s: Write(nil)=%x != fresh instance=%x", c.name, got, want)
			}
		}
	})
}

// FuzzSkeinHashSize 针对 Skein 可输出任意摘要长度的特性：hashsize 小于、等于、
// 大于一个块时，Sum 的长度与前缀语义都必须成立。
func FuzzSkeinHashSize(f *testing.F) {
	f.Add([]byte("abc"), uint16(32))
	f.Add([]byte("abc"), uint16(64))
	f.Add([]byte(""), uint16(1))
	f.Add(bytes.Repeat([]byte{0x41}, 200), uint16(200))
	f.Add(bytes.Repeat([]byte{0x41}, 1000), uint16(128))

	f.Fuzz(func(t *testing.T, data []byte, hashsize uint16) {
		if hashsize < 1 || hashsize > 512 {
			return
		}
		size := int(hashsize)

		for _, c := range []struct {
			name string
			new  func() hash.Hash
		}{
			{"skein512", func() hash.Hash { return skein.New(size, nil) }},
			{"skein256", func() hash.Hash { return skein256.New(size, nil) }},
			{"skein1024", func() hash.Hash { return skein1024.New(size, nil) }},
		} {
			h := c.new()
			h.Write(data)
			one := h.Sum(nil)

			if len(one) != size {
				t.Fatalf("%s-%d: len(Sum(nil))=%d != %d", c.name, size, len(one), size)
			}
			if len(one) != h.Size() {
				t.Fatalf("%s-%d: len(Sum(nil))=%d != Size()=%d", c.name, size, len(one), h.Size())
			}
			if got := h.Sum(nil); !bytes.Equal(got, one) {
				t.Fatalf("%s-%d: Sum not idempotent", c.name, size)
			}
			pfx := []byte("pfx")
			if got := h.Sum(pfx); !bytes.Equal(got, append(append([]byte{}, pfx...), one...)) {
				t.Fatalf("%s-%d: Sum(b) dropped prefix: %x", c.name, size, got)
			}
		}
	})
}
