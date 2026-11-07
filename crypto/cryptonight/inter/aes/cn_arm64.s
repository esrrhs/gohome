//go:build arm64

#include "textflag.h"

// func CnRoundsAsm(dst, src *uint64, rkeys *[40]uint32)
//
// 等价于 x86 的 10 轮 AESENC：每轮 state = MixColumns(SubBytes(ShiftRows(state))) XOR rkey。
// ARMv8 的 AESE 语义是 state = SubBytes(ShiftRows(state XOR key))，密钥在轮函数之前，
// 与 AESENC 的"密钥在轮函数之后"不同。因此这里用零密钥 AESE 得到 SubBytes(ShiftRows(state))，
// 再 AESMC 完成 MixColumns，最后 VEOR 异或轮密钥。
// rkeys 必须是 CnExpandKeyGoAsm 生成的硬件格式。
TEXT ·CnRoundsAsm(SB), NOSPLIT, $0
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	rkeys+16(FP), R2
	VEOR	V2.B16, V2.B16, V2.B16
	VLD1	(R1), [V0.B16]
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VLD1.P	16(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VST1	[V0.B16], (R0)
	RET

// func CnSingleRoundAsm(dst, src *uint64, rkey *[2]uint64)
//
// 单轮语义同 AESENC（密钥在轮函数之后），实现方式同上。
TEXT ·CnSingleRoundAsm(SB), NOSPLIT, $0
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	rkey+16(FP), R2
	VEOR	V2.B16, V2.B16, V2.B16
	VLD1	(R1), [V0.B16]
	VLD1	(R2), [V1.B16]
	AESE	V2.B16, V0.B16
	AESMC	V0.B16, V0.B16
	VEOR	V1.B16, V0.B16, V0.B16
	VST1	[V0.B16], (R0)
	RET
