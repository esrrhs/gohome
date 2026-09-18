package common

import (
	"bytes"
	"compress/zlib"
	"crypto/rc4"
	"io"
	"sync"
	"unsafe"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
)

func CompressData(src []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write(src)
	w.Close()
	return b.Bytes()
}

func DeCompressData(src []byte) ([]byte, error) {
	b := bytes.NewReader(src)
	r, err := zlib.NewReader(b)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	io.Copy(&out, r)
	r.Close()
	return out.Bytes(), nil
}

const zstdBufCap = 128 << 10
const zstdBufMax = 1 << 20
const zstdMaxDecoded = 8 << 20 // reject absurd frames

var (
	zstdEncoder, _ = zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderCRC(false), // proxy frames are already integrity-checked elsewhere
	)
	// DecodeAll of many small frames: concurrency 1 avoids worker-pool overhead.
	zstdDecoder, _ = zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(false),
	)

	zstdEncBufPool = sync.Pool{New: func() any {
		b := make([]byte, 0, zstdBufCap)
		return &b
	}}
	zstdDecBufPool = sync.Pool{New: func() any {
		b := make([]byte, 0, zstdBufCap)
		return &b
	}}
)

func CompressDataZstd(src []byte) []byte {
	bp := zstdEncBufPool.Get().(*[]byte)
	tmp := zstdEncoder.EncodeAll(src, (*bp)[:0])
	out := make([]byte, len(tmp))
	copy(out, tmp)
	if cap(tmp) <= zstdBufMax {
		*bp = tmp[:0]
	} else {
		*bp = make([]byte, 0, zstdBufCap)
	}
	zstdEncBufPool.Put(bp)
	return out
}

func DeCompressDataZstd(src []byte) ([]byte, error) {
	// Fast path: EncodeAll writes FrameContentSize; pre-size dst so DecodeAll
	// does a single allocation and does not grow mid-decode.
	var hdr zstd.Header
	if err := hdr.Decode(src); err == nil && hdr.HasFCS && hdr.FrameContentSize > 0 &&
		hdr.FrameContentSize <= zstdMaxDecoded {
		dst := make([]byte, 0, int(hdr.FrameContentSize))
		return zstdDecoder.DecodeAll(src, dst)
	}

	// Fallback when FCS is absent: decode into pooled scratch, return exact copy.
	bp := zstdDecBufPool.Get().(*[]byte)
	tmp, err := zstdDecoder.DecodeAll(src, (*bp)[:0])
	if err != nil {
		zstdDecBufPool.Put(bp)
		return nil, err
	}
	out := make([]byte, len(tmp))
	copy(out, tmp)
	if cap(tmp) <= zstdBufMax {
		*bp = tmp[:0]
	} else {
		*bp = make([]byte, 0, zstdBufCap)
	}
	zstdDecBufPool.Put(bp)
	return out, nil
}

// rc4State is the initial S-box after KSA (i=j=0), matching crypto/rc4.
type rc4State struct {
	s [256]uint32
}

var rc4States sync.Map // string -> *rc4State

func rc4KSA(key []byte) *rc4State {
	st := &rc4State{}
	for i := 0; i < 256; i++ {
		st.s[i] = uint32(i)
	}
	var j uint8
	k := len(key)
	for i := 0; i < 256; i++ {
		j += uint8(st.s[i]) + key[i%k]
		st.s[i], st.s[j] = st.s[j], st.s[i]
	}
	return st
}

func rc4XOR(s *[256]uint32, dst, src []byte) {
	var i, j uint8
	for k, v := range src {
		i++
		j += uint8(s[i])
		s[i], s[j] = s[j], s[i]
		dst[k] = v ^ uint8(s[uint8(s[i]+s[j])])
	}
}

func Rc4(key string, src []byte) ([]byte, error) {
	if len(key) < 1 || len(key) > 256 {
		return nil, rc4.KeySizeError(len(key))
	}

	var init *rc4State
	if v, ok := rc4States.Load(key); ok {
		init = v.(*rc4State)
	} else {
		init = rc4KSA([]byte(key))
		if actual, loaded := rc4States.LoadOrStore(key, init); loaded {
			init = actual.(*rc4State)
		}
	}

	s := init.s // copy initial S-box; each call starts keystream from 0
	dst := make([]byte, len(src))
	rc4XOR(&s, dst, src)
	return dst, nil
}

func Guid() string {
	return uuid.New().String()
}

var gIsBigEndian int

func IsBigEndian() bool {
	if gIsBigEndian != 0 {
		return gIsBigEndian == 1
	}
	var i uint16 = 0x1
	b := (*[2]byte)(unsafe.Pointer(&i))
	if b[1] == 0 {
		gIsBigEndian = -1 // 小端
	} else {
		gIsBigEndian = 1 // 大端
	}
	return b[0] == 0
}

func DebugSetBigEndian(isBigEndian bool) {
	if isBigEndian {
		gIsBigEndian = 1 // 大端
	} else {
		gIsBigEndian = -1 // 小端
	}
}

func DebugResetBigEndian() {
	gIsBigEndian = 0 // 未设置
}
