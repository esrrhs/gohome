//go:build !amd64
// +build !amd64

package cryptonight

import (
	"math"
	"math/bits"
)

func mul128(x, y uint64) (lo, hi uint64) {
	// arm64 上 bits.Mul64 编译为 mul + umulh 两条指令，比手写 4 次乘法
	// 的进位链短约 40%；其它 !amd64 平台由编译器内联等价实现。
	hi, lo = bits.Mul64(x, y)
	return
}

func v2Sqrt(in uint64) uint64 {
	out := uint64(
		math.Sqrt(
			float64(in)+1<<64,
		)*2 - 1<<33,
	)

	s := out >> 1
	b := out & 1
	r := s*(s+b) + (out << 32)
	if r+b > in {
		out--
	}

	// NOTE: the following branch does not seem to be able to be covered,
	//   i.e. it works without the code below.
	//   In case you find any issue, try de-commenting these.

	// if r+1<<32 < in-s {
	// 	out++
	// }

	return out
}
