package common

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/OneOfOne/xxhash"
	"hash/crc32"
	"hash/fnv"
	"strconv"
)

func GetMd5String(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func GetXXHashString(s string) string {
	h := xxhash.New64()
	h.WriteString(s)
	return strconv.FormatUint(h.Sum64(), 10)
}

func GetCrc32String(s string) string {
	hash := crc32.New(crc32.IEEETable)
	hash.Write([]byte(s))
	hashInBytes := hash.Sum(nil)[:]
	return hex.EncodeToString(hashInBytes)
}

func GetCrc32(data []byte) string {
	hash := crc32.New(crc32.IEEETable)
	hash.Write(data)
	hashInBytes := hash.Sum(nil)[:]
	return hex.EncodeToString(hashInBytes)
}

// 计算字符串的FNV哈希值
func HashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// 计算整数的FNV哈希值
func HashInt(i int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(fmt.Sprint(i)))
	return h.Sum64()
}

// HashGeneric 计算泛型类型的哈希值
func HashGeneric[T any](key T) uint64 {
	switch v := any(key).(type) {
	case int:
		return HashInt(v)
	case int8:
		return HashInt(int(v))
	case int16:
		return HashInt(int(v))
	case int32:
		return HashInt(int(v))
	case int64:
		return HashInt(int(v))
	case uint:
		return HashInt(int(v))
	case uint8:
		return HashInt(int(v))
	case uint16:
		return HashInt(int(v))
	case uint32:
		return HashInt(int(v))
	case uint64:
		return HashInt(int(v))
	case float32:
		return HashString(fmt.Sprintf("%f", v))
	case float64:
		return HashString(fmt.Sprintf("%f", v))
	case bool:
		if v {
			return HashString("true")
		} else {
			return HashString("false")
		}
	case []byte:
		return HashString(string(v))
	case nil:
		return HashString("nil")
	case string:
		return HashString(v)
	default:
		panic("unsupported type for hashing %v" + fmt.Sprintf("%T", v))
	}
}
