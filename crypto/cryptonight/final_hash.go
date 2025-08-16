package cryptonight

import (
	"encoding/binary"
	"hash"
	"sync"
	"unsafe"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/blake256"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/groestl"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/jh"
	"github.com/esrrhs/gohome/crypto/cryptonight/inter/skein"
)

var hashPool = [...]*sync.Pool{
	{New: func() interface{} { return blake256.New() }},
	{New: func() interface{} { return groestl.New256() }},
	{New: func() interface{} { return jh.New256() }},
	{New: func() interface{} { return skein.New256(nil) }},
}

func (cc *CryptoNight) finalHash() []byte {
	hp := hashPool[cc.finalState[0]&0x03]
	h := hp.Get().(hash.Hash)
	h.Reset()
	if common.IsBigEndian() {
		buf := make([]byte, 200)
		for i, v := range cc.finalState {
			binary.LittleEndian.PutUint64(buf[i*8:i*8+8], v)
		}
		h.Write(buf)
	} else {
		h.Write((*[200]byte)(unsafe.Pointer(&cc.finalState))[:])
	}
	sum := h.Sum(nil)
	hp.Put(h)

	return sum
}
