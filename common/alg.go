package common

import (
	"bytes"
	"compress/zlib"
	"crypto/rc4"
	"io"
	"unsafe"

	"github.com/google/uuid"
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

func Rc4(key string, src []byte) ([]byte, error) {
	c, err := rc4.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	dst := make([]byte, len(src))
	c.XORKeyStream(dst, src)
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
