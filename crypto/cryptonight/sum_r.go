package cryptonight

import (
	"encoding/binary"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/blake256"
	"unsafe"
)

const (
	// Generate code with minimal theoretical latency = 45 cycles, which is equivalent to 15 multiplications
	TOTAL_LATENCY = 15 * 3

	// Always generate at least 60 instructions
	NUM_INSTRUCTIONS_MIN = 60

	// Never generate more than 70 instructions (final RET instruction doesn't count here)
	NUM_INSTRUCTIONS_MAX = 70

	// Available ALUs for MUL
	// Modern CPUs typically have only 1 ALU which can do multiplications
	ALU_COUNT_MUL = 1

	// Total available ALUs
	// Modern CPUs have 4 ALUs, but we use only 3 because random math executes together with other main loop code
	ALU_COUNT = 3
)

const (
	MUL                  = iota // a*b
	ADD                         // a+b + C, C is an unsigned 32-bit constant
	SUB                         // a-b
	ROR                         // rotate right "a" by "b & 31" bits
	ROL                         // rotate left "a" by "b & 31" bits
	XOR                         // a^b
	RET                         // finish execution
	V4_INSTRUCTION_COUNT = RET
)

const (
	V4_OPCODE_BITS    = 3
	V4_DST_INDEX_BITS = 2
	V4_SRC_INDEX_BITS = 3
)

const REG_BITS = 32

type V4_Instruction struct {
	opcode    uint8
	dst_index uint8
	src_index uint8
	C         uint32
}

// MUL is 3 cycles, 3-way addition and rotations are 2 cycles, SUB/XOR are 1 cycle
// These latencies match real-life instruction latencies for Intel CPUs starting from Sandy Bridge and up to Skylake/Coffee lake
//
// AMD Ryzen has the same latencies except 1-cycle ROR/ROL, so it'll be a bit faster than Intel Sandy Bridge and newer processors
// Surprisingly, Intel Nehalem also has 1-cycle ROR/ROL, so it'll also be faster than Intel Sandy Bridge and newer processors
// AMD Bulldozer has 4 cycles latency for MUL (slower than Intel) and 1 cycle for ROR/ROL (faster than Intel), so average performance will be the same
// Source: https://www.agner.org/optimize/instruction_tables.pdf
var op_latency = [V4_INSTRUCTION_COUNT]int{3, 2, 1, 2, 2, 1}

// Instruction latencies for theoretical ASIC implementation
var asic_op_latency = [V4_INSTRUCTION_COUNT]int{3, 1, 1, 1, 1, 1}

// Available ALUs for each instruction
var op_ALUs = [V4_INSTRUCTION_COUNT]int{ALU_COUNT_MUL, ALU_COUNT, ALU_COUNT, ALU_COUNT, ALU_COUNT, ALU_COUNT}

func v4_exec(code []V4_Instruction, r []uint32, i int) bool {
	op := code[i]
	src := r[op.src_index]
	dst := &r[op.dst_index]
	switch op.opcode {
	case MUL:
		*dst *= src
	case ADD:
		*dst += src + op.C
	case SUB:
		*dst -= src
	case ROR:
		shift := src % REG_BITS
		*dst = (*dst >> shift) | (*dst << ((REG_BITS - shift) % REG_BITS))
	case ROL:
		shift := src % REG_BITS
		*dst = (*dst << shift) | (*dst >> ((REG_BITS - shift) % REG_BITS))
	case XOR:
		*dst ^= src
	case RET:
		return false
	default:
		panic("UNREACHABLE_CODE")
		break
	}

	return true
}

// Random math interpreter's loop is fully unrolled and inlined to achieve 100% branch prediction on CPU:
// every switch-case will point to the same destination on every iteration of Cryptonight main loop
//
// This is about as fast as it can get without using low-level machine code generation
func v4_random_math(code []V4_Instruction, r []uint32) {
	// Generated program can have 60 + a few more (usually 2-3) instructions to achieve required latency
	// I've checked all block heights < 10,000,000 and here is the distribution of program sizes:
	//
	// 60      27960
	// 61      105054
	// 62      2452759
	// 63      5115997
	// 64      1022269
	// 65      1109635
	// 66      153145
	// 67      8550
	// 68      4529
	// 69      102

	//loggo.Info("v4_random_math before r %v %v %v %v %v %v %v %v %v", r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8])

	// Unroll 70 instructions here
	for i := 0; i < NUM_INSTRUCTIONS_MAX; i++ {
		if !v4_exec(code, r, i) {
			break
		}
	}

	//loggo.Info("v4_random_math end r %v %v %v %v %v %v %v %v %v", r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8])
}

func check_data(data_index *int, bytes_needed int, data *[]byte) {
	if *data_index+bytes_needed > len(*data) {
		tmp := blake256.Sum256((*data)[:])
		copy((*data)[:], tmp[:])
		*data_index = 0
	}
}

// Generates as many random math operations as possible with given latency and ALU restrictions
// "code" array must have space for NUM_INSTRUCTIONS_MAX+1 instructions
func v4_random_math_init(code []V4_Instruction, height uint64) {

	data := make([]int8, 32)
	tmp := height
	binary.LittleEndian.PutUint64(*(*[]byte)(unsafe.Pointer(&data)), tmp)
	data[20] = -38 // change seed

	//loggo.Info("before data %d %d %d %d %d %d %d %d", data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7])

	// Set data_index past the last byte in data
	// to trigger full data update with blake hash
	// before we start using it
	data_index := len(data)

	var code_size int

	// There is a small chance (1.8%) that register R8 won't be used in the generated program
	// So we keep track of it and try again if it's not used
	var r8_used bool

	for {
		var latency [9]int
		var asic_latency [9]int

		// Tracks previous instruction and value of the source operand for registers R0-R3 throughout code execution
		// byte 0: current value of the destination register
		// byte 1: instruction opcode
		// byte 2: current value of the source register
		//
		// Registers R4-R8 are constant and are treated as having the same value because when we do
		// the same operation twice with two constant source registers, it can be optimized into a single operation
		var inst_data = [9]uint32{0, 1, 2, 3, 0xFFFFFF, 0xFFFFFF, 0xFFFFFF, 0xFFFFFF, 0xFFFFFF}

		var alu_busy [TOTAL_LATENCY + 1][ALU_COUNT]bool
		var is_rotation [V4_INSTRUCTION_COUNT]bool
		var rotated [4]bool
		var rotate_count int

		is_rotation[ROR] = true
		is_rotation[ROL] = true

		num_retries := 0
		code_size = 0

		total_iterations := 0
		r8_used = false

		// Generate random code to achieve minimal required latency for our abstract CPU
		// Try to get this latency for all 4 registers
		for ((latency[0] < TOTAL_LATENCY) || (latency[1] < TOTAL_LATENCY) || (latency[2] < TOTAL_LATENCY) || (latency[3] < TOTAL_LATENCY)) && (num_retries < 64) {
			// Fail-safe to guarantee loop termination
			total_iterations++
			if total_iterations > 256 {
				break
			}

			check_data(&data_index, 1, (*[]byte)(unsafe.Pointer(&data)))

			c := uint8(data[data_index])
			data_index++

			//loggo.Info("run c %d %d %d", data_index, total_iterations, c)

			// MUL = opcodes 0-2
			// ADD = opcode 3
			// SUB = opcode 4
			// ROR/ROL = opcode 5, shift direction is selected randomly
			// XOR = opcodes 6-7
			opcode := uint8(c & ((1 << V4_OPCODE_BITS) - 1))
			if opcode == 5 {
				check_data(&data_index, 1, (*[]byte)(unsafe.Pointer(&data)))
				if data[data_index] >= 0 {
					opcode = ROR
				} else {
					opcode = ROL
				}
				data_index++
			} else if opcode >= 6 {
				opcode = XOR
			} else {
				if opcode <= 2 {
					opcode = MUL
				} else {
					opcode = opcode - 2
				}
			}

			dst_index := (c >> V4_OPCODE_BITS) & ((1 << V4_DST_INDEX_BITS) - 1)
			src_index := (c >> (V4_OPCODE_BITS + V4_DST_INDEX_BITS)) & ((1 << V4_SRC_INDEX_BITS) - 1)

			a := int(dst_index)
			b := int(src_index)

			// Don't do ADD/SUB/XOR with the same register
			if ((opcode == ADD) || (opcode == SUB) || (opcode == XOR)) && (a == b) {
				// Use register R8 as source instead
				b = 8
				src_index = 8
			}

			// Don't do rotation with the same destination twice because it's equal to a single rotation
			if is_rotation[opcode] && rotated[a] {
				continue
			}

			// Don't do the same instruction (except MUL) with the same source value twice because all other cases can be optimized:
			// 2xADD(a, b, C) = ADD(a, b*2, C1+C2), same for SUB and rotations
			// 2xXOR(a, b) = NOP
			left := inst_data[a] & 0xFFFF00
			right1 := uint32(opcode) << 8
			right2 := (inst_data[b] & 255) << 16
			if (opcode != MUL) && (left == right1+right2) {
				//("run c continue %d %d %d %v %v %v %v", data_index, total_iterations, c, a, b, inst_data[a], inst_data[b])
				continue
			}

			// Find which ALU is available (and when) for this instruction
			var next_latency int
			if latency[a] > latency[b] {
				next_latency = latency[a]
			} else {
				next_latency = latency[b]
			}
			alu_index := -1
			for next_latency < TOTAL_LATENCY {
				for i := op_ALUs[opcode] - 1; i >= 0; i-- {
					if !alu_busy[next_latency][i] {
						// ADD is implemented as two 1-cycle instructions on a real CPU, so do an additional availability check
						if (opcode == ADD) && alu_busy[next_latency+1][i] {
							continue
						}

						// Rotation can only start when previous rotation is finished, so do an additional availability check
						if is_rotation[opcode] && (next_latency < rotate_count*op_latency[opcode]) {
							continue
						}

						alu_index = i
						break
					}
				}

				if alu_index >= 0 {
					break
				}
				next_latency++
			}

			// Don't generate instructions that leave some register unchanged for more than 7 cycles
			if next_latency > latency[a]+7 {
				continue
			}

			next_latency += op_latency[opcode]

			if next_latency <= TOTAL_LATENCY {
				if is_rotation[opcode] {
					rotate_count++
				}

				// Mark ALU as busy only for the first cycle when it starts executing the instruction because ALUs are fully pipelined
				alu_busy[next_latency-op_latency[opcode]][alu_index] = true
				latency[a] = next_latency

				// ASIC is supposed to have enough ALUs to run as many independent instructions per cycle as possible, so latency calculation for ASIC is simple
				if asic_latency[a] > asic_latency[b] {
					asic_latency[a] = asic_latency[a] + asic_op_latency[opcode]
				} else {
					asic_latency[a] = asic_latency[b] + asic_op_latency[opcode]
				}
				//loggo.Info("run c latency step %d %d %d %d %d %d, %d %d %d %d %d %d %d %d %d", data_index, total_iterations, c, opcode, a, b, asic_latency[0], asic_latency[1], asic_latency[2], asic_latency[3], asic_latency[4], asic_latency[5], asic_latency[6], asic_latency[7], asic_latency[8])

				rotated[a] = is_rotation[opcode]

				inst_data[a] = uint32(code_size) + (uint32(opcode) << 8) + ((inst_data[b] & 255) << 16)

				code[code_size].opcode = uint8(opcode)
				code[code_size].dst_index = uint8(dst_index)
				code[code_size].src_index = uint8(src_index)
				code[code_size].C = 0

				if src_index == 8 {
					r8_used = true
				}

				if opcode == ADD {
					// ADD instruction is implemented as two 1-cycle instructions on a real CPU, so mark ALU as busy for the next cycle too
					alu_busy[next_latency-op_latency[opcode]+1][alu_index] = true

					// ADD instruction requires 4 more random bytes for 32-bit constant "C" in "a = a + b + C"
					check_data(&data_index, 4, (*[]byte)(unsafe.Pointer(&data)))
					t := binary.LittleEndian.Uint32((*(*[]byte)(unsafe.Pointer(&data)))[data_index:])
					code[code_size].C = t
					data_index += 4
				}

				code_size++
				if code_size >= NUM_INSTRUCTIONS_MIN {
					break
				}
			} else {
				num_retries++
			}
		}

		//loggo.Info("run c latency %d %d %d %d %d %d %d %d %d", asic_latency[0], asic_latency[1], asic_latency[2], asic_latency[3], asic_latency[4], asic_latency[5], asic_latency[6], asic_latency[7], asic_latency[8])

		// ASIC has more execution resources and can extract as much parallelism from the code as possible
		// We need to add a few more MUL and ROR instructions to achieve minimal required latency for ASIC
		// Get this latency for at least 1 of the 4 registers
		prev_code_size := code_size
		for (code_size < NUM_INSTRUCTIONS_MAX) && (asic_latency[0] < TOTAL_LATENCY) && (asic_latency[1] < TOTAL_LATENCY) && (asic_latency[2] < TOTAL_LATENCY) && (asic_latency[3] < TOTAL_LATENCY) {
			min_idx := 0
			max_idx := 0
			for i := 1; i < 4; i++ {
				if asic_latency[i] < asic_latency[min_idx] {
					min_idx = i
				}
				if asic_latency[i] > asic_latency[max_idx] {
					max_idx = i
				}
			}

			var pattern = [3]uint8{ROR, MUL, MUL}
			opcode := pattern[(code_size-prev_code_size)%3]
			latency[min_idx] = latency[max_idx] + op_latency[opcode]
			asic_latency[min_idx] = asic_latency[max_idx] + asic_op_latency[opcode]

			code[code_size].opcode = opcode
			code[code_size].dst_index = uint8(min_idx)
			code[code_size].src_index = uint8(max_idx)
			code[code_size].C = 0
			code_size++
		}

		// There is ~98.15% chance that loop condition is false, so this loop will execute only 1 iteration most of the time
		// It never does more than 4 iterations for all block heights < 10,000,000
		if !(!r8_used || (code_size < NUM_INSTRUCTIONS_MIN) || (code_size > NUM_INSTRUCTIONS_MAX)) {
			break
		}
	}

	// It's guaranteed that NUM_INSTRUCTIONS_MIN <= code_size <= NUM_INSTRUCTIONS_MAX here
	// Add final instruction to stop the interpreter
	code[code_size].opcode = RET
	code[code_size].dst_index = 0
	code[code_size].src_index = 0
	code[code_size].C = 0
}
