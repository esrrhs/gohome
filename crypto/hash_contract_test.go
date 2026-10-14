// 本文件调用 crypto/cryptonight/inter 下的上游移植实现，其中 sha3 在 amd64/
// 386/ppc64le 上走 xor_unaligned.go 的 unsafe 非对齐指针转换，Go 1.26 的 -race
// 内置 checkptr 会直接 fatal error。这与 CI 把 crypto/cryptonight/inter/ 排除在
// race 门禁之外是同一个原因。契约检查本身与并发无关，故只在非 race 下编译：
// 常规 Test 步骤（含 s390x Big Endian 工作流）仍会完整执行这些用例。

//go:build !race

package crypto

import (
	"bytes"
	"hash"
	"math/rand/v2"
	"sort"
	"strconv"
	"testing"

	"github.com/esrrhs/gohome/crypto/cryptonight/inter/blake256"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/groestl"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/jh"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/ripemd160"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/sha3"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/skein"
	skein1024 "github.com/esrrhs/gohome/crypto/cryptonight/inter/skein/skein1024"
	skein256 "github.com/esrrhs/gohome/crypto/cryptonight/inter/skein/skein256"
)

// contractLengths 覆盖各原语的填充边界与跨块边界：0 是空消息，
// 55/56/57 与 63/64/65 是 64 字节块族的填充临界点，
// 191/192/255/1000 覆盖多块与多轮输出。
var contractLengths = []int{0, 1, 31, 32, 55, 56, 57, 63, 64, 65, 127, 128, 129, 191, 192, 255, 1000}

// contractShortLengths 是矩阵测试用的缩减集，避免 hashsize × 原语 × 长度
// 的组合爆炸拖慢 CI。
var contractShortLengths = []int{0, 1, 64, 65, 200}

// contractPrefixes 覆盖 Sum 前缀的各种长度：空前缀、跨字节、跨块边界、
// 超过一整块，确保 Sum(b) 一律返回 b||digest 而不是被摘要长度截断。
var contractPrefixes = [][]byte{
	nil,
	[]byte("P"),
	[]byte("prefix"),
	bytes.Repeat([]byte{0xAB}, 63),
	bytes.Repeat([]byte{0xAB}, 64),
	bytes.Repeat([]byte{0xAB}, 65),
	bytes.Repeat([]byte{0xAB}, 128),
}

func contractMsg(n int) []byte {
	msg := make([]byte, n)
	for i := range msg {
		msg[i] = byte(i*31 + n)
	}
	return msg
}

// checkContract 对单个 hash.Hash 实现执行完整的 hash.Hash 契约检查：
//   - 分块写入与一次性写入等价
//   - Sum 幂等（收尾必须在状态副本上进行，不得污染接收者）
//   - Sum(b) 返回 b||digest，保留调用方前缀
//   - len(Sum(nil)) == Size()
//   - Sum 之后继续写入结果仍然正确
//   - Reset 恢复到全新实例的状态
//   - 零长度写入不改变摘要
//   - 空消息：Write(nil) 与从未写入过的实例结果一致
func checkContract(t *testing.T, name string, newh func() hash.Hash, lengths []int) {
	t.Helper()

	for _, n := range lengths {
		msg := contractMsg(n)

		base := newh()
		base.Write(msg)
		one := base.Sum(nil)

		if len(one) != base.Size() {
			t.Fatalf("%s len=%d: len(Sum(nil))=%d != Size()=%d", name, n, len(one), base.Size())
		}

		// 1. 逐字节写入必须与一次性写入等价
		stream := newh()
		for i := 0; i < len(msg); i++ {
			stream.Write(msg[i : i+1])
		}
		if !bytes.Equal(one, stream.Sum(nil)) {
			t.Errorf("%s len=%d: streaming write != one-shot write", name, n)
		}

		// 2. Sum 幂等
		if !bytes.Equal(one, base.Sum(nil)) {
			t.Errorf("%s len=%d: Sum is not idempotent", name, n)
		}

		// 3. Sum(b) 必须保留调用方前缀
		for _, pfx := range contractPrefixes {
			want := append(append([]byte{}, pfx...), one...)
			if got := base.Sum(pfx); !bytes.Equal(got, want) {
				t.Errorf("%s len=%d prefix=%d: Sum(b)=%x, want %x", name, n, len(pfx), got, want)
			}
		}

		// 4. Sum 之后继续写入
		cont := newh()
		cont.Write(msg)
		cont.Sum(nil)
		cont.Write([]byte("tail"))
		wantTail := newh()
		wantTail.Write(append(append([]byte{}, msg...), []byte("tail")...))
		if !bytes.Equal(cont.Sum(nil), wantTail.Sum(nil)) {
			t.Errorf("%s len=%d: writing after Sum produced a different digest", name, n)
		}

		// 5. Reset 恢复到全新状态
		reused := newh()
		reused.Write([]byte("junk that must be discarded"))
		reused.Reset()
		reused.Write(msg)
		if !bytes.Equal(reused.Sum(nil), one) {
			t.Errorf("%s len=%d: Reset did not restore a pristine state", name, n)
		}

		// 6. 零长度写入不得改变摘要
		zero := newh()
		zero.Write(nil)
		zero.Write(msg)
		zero.Write([]byte{})
		if !bytes.Equal(zero.Sum(nil), one) {
			t.Errorf("%s len=%d: zero-length Write changed the digest", name, n)
		}
	}

	// 7. 空消息：Skein 的空消息摘要本身含一个全零填充块，一旦跳过收尾
	// 就会退化成 output(链值)，与从未写入过的实例应有的结果不符。
	wrote := newh()
	wrote.Write(nil)
	fresh := newh()
	if !bytes.Equal(wrote.Sum(nil), fresh.Sum(nil)) {
		t.Errorf("%s: Write(nil)=%x != fresh instance=%x", name, wrote.Sum(nil), fresh.Sum(nil))
	}

	if newh().BlockSize() <= 0 {
		t.Errorf("%s: BlockSize must be > 0", name)
	}
}

type contractCase struct {
	name string
	new  func() hash.Hash
}

var contractPrimitives = []contractCase{
	{"blake256", blake256.New},
	{"blake256-224", blake256.New224},
	{"groestl256", groestl.New256},
	{"jh256", jh.New256},
	{"ripemd160", ripemd160.New},
	{"sha3-224", sha3.New224},
	{"sha3-256", sha3.New256},
	{"sha3-384", sha3.New384},
	{"sha3-512", sha3.New512},
	{"keccak256", sha3.NewLegacyKeccak256},
	{"skein256", func() hash.Hash { return skein256.New256(nil) }},
	{"skein512", func() hash.Hash { return skein.New512(nil) }},
	{"skein1024", func() hash.Hash { return skein1024.New512(nil) }},
}

// TestHashContract 对 crypto/cryptonight/inter 下所有对外暴露的哈希原语执行
// 统一契约检查。历史上 jh（Write 短分包越界 panic）、skein（Sum 丢弃前缀）、
// groestl（Sum 污染接收者状态）都是同一类契约违反，此用例防止它们复发。
func TestHashContract(t *testing.T) {
	for _, c := range contractPrimitives {
		checkContract(t, c.name, c.new, contractLengths)
	}
}

// TestHashContractSkeinHashSizes 针对 Skein 可输出任意摘要长度的特性：
// hashsize 小于、等于、大于一个块时，Sum 的前缀语义都必须成立。
func TestHashContractSkeinHashSizes(t *testing.T) {
	for _, size := range []int{1, 20, 32, 48, 63, 64, 65, 100, 128, 200} {
		for _, f := range []contractCase{
			{"skein512-" + strconv.Itoa(size), func() hash.Hash { return skein.New(size, nil) }},
			{"skein256-" + strconv.Itoa(size), func() hash.Hash { return skein256.New(size, nil) }},
			{"skein1024-" + strconv.Itoa(size), func() hash.Hash { return skein1024.New(size, nil) }},
		} {
			checkContract(t, f.name, f.new, contractShortLengths)
		}
	}
}

// TestHashContractRandomChunking 用固定种子的随机长度 + 随机切点，覆盖穷举
// 边界之外的组合，并在每段之间插入零长度写入。
func TestHashContractRandomChunking(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261013, 9721))

	for _, c := range contractPrimitives {
		t.Run(c.name, func(t *testing.T) {
			for round := 0; round < 200; round++ {
				n := int(rng.UintN(1001))
				msg := make([]byte, n)
				for i := range msg {
					msg[i] = byte(rng.UintN(256))
				}

				base := c.new()
				base.Write(msg)
				one := base.Sum(nil)

				// 随机切成 1..6 段依次写入，段间插入零长度写入
				stream := c.new()
				chunks := int(rng.UintN(6)) + 1
				cuts := make([]int, 0, chunks)
				for i := 0; i < chunks-1; i++ {
					cuts = append(cuts, int(rng.IntN(n+1)))
				}
				sort.Ints(cuts)
				prev := 0
				for _, cut := range cuts {
					stream.Write(msg[prev:cut])
					stream.Write(nil)
					prev = cut
				}
				stream.Write(msg[prev:])

				if !bytes.Equal(stream.Sum(nil), one) {
					t.Fatalf("round=%d n=%d: random chunking != one-shot", round, n)
				}
				if !bytes.Equal(one, base.Sum(nil)) {
					t.Errorf("round=%d n=%d: Sum is not idempotent", round, n)
				}
				pfx := []byte("random-prefix")
				if !bytes.Equal(base.Sum(pfx), append(append([]byte{}, pfx...), one...)) {
					t.Errorf("round=%d n=%d: Sum(b) dropped the prefix", round, n)
				}
			}
		})
	}
}
