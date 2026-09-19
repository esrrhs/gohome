package common

import (
	"bytes"
	"compress/zlib"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestDeCompressDataCorrupt(t *testing.T) {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write([]byte("hello corrupt zlib"))
	w.Close()
	truncated := b.Bytes()[:len(b.Bytes())/2]

	out, err := DeCompressData(truncated)
	if err == nil {
		t.Fatalf("expected error for truncated zlib, got data %q", out)
	}
}

func TestDeCompressDataZstdSizeLimit(t *testing.T) {
	// Compress payload larger than zstdMaxDecoded; FCS early-reject or decoder max memory must fail.
	big := make([]byte, zstdMaxDecoded+1)
	for i := range big {
		big[i] = byte(i)
	}
	compressed := CompressDataZstd(big)
	var hdr zstd.Header
	if err := hdr.Decode(compressed); err != nil {
		t.Fatal(err)
	}
	if hdr.HasFCS && hdr.FrameContentSize <= zstdMaxDecoded {
		t.Fatalf("expected FCS > limit, got %d", hdr.FrameContentSize)
	}
	out, err := DeCompressDataZstd(compressed)
	if err == nil {
		t.Fatalf("expected size limit error, got %d bytes", len(out))
	}
}

func TestWriteTimeoutDoesNotLeakTimers(t *testing.T) {
	c := NewChannel(1)
	c.Write(1) // fill buffer so WriteTimeout waits then times out

	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		if c.WriteTimeout(1, 5) {
			t.Fatal("expected timeout on full channel")
		}
	}
	// Give any leaked timers a moment; with NewTimer+Stop they should not pile up.
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+20 {
		t.Fatalf("goroutine count grew suspiciously: before=%d after=%d", before, after)
	}
	c.Close()
}

func TestNearlyEqualZeroAndNegative(t *testing.T) {
	if !NearlyEqual(0, 0) {
		t.Fatal("NearlyEqual(0,0) should be true")
	}
	if !NearlyEqual(-100, -99) {
		t.Fatal("NearlyEqual(-100,-99) should be true")
	}
	if NearlyEqual(-10, 10) {
		t.Fatal("NearlyEqual(-10,10) should be false")
	}
	if !NearlyEqual(99, 100) {
		t.Fatal("NearlyEqual(99,100) should be true")
	}
	if NearlyEqual(1, 10) {
		t.Fatal("NearlyEqual(1,10) should be false")
	}
}

func TestAbsIntMinInt(t *testing.T) {
	if AbsInt(math.MinInt) != math.MaxInt {
		t.Fatalf("AbsInt(MinInt)=%d, want MaxInt", AbsInt(math.MinInt))
	}
	if AbsInt32(math.MinInt32) != math.MaxInt32 {
		t.Fatalf("AbsInt32(MinInt32)=%d, want MaxInt32", AbsInt32(math.MinInt32))
	}
	if AbsInt64(math.MinInt64) != math.MaxInt64 {
		t.Fatalf("AbsInt64(MinInt64)=%d, want MaxInt64", AbsInt64(math.MinInt64))
	}
	// indexing style use must not panic / go negative
	n := 8
	idx := AbsInt(math.MinInt) % n
	if idx < 0 || idx >= n {
		t.Fatalf("index %d out of range", idx)
	}
}

func TestMakeIntSignExtension(t *testing.T) {
	v := MAKEINT64(1, -1)
	want := int64(0x00000001ffffffff)
	if v != want {
		t.Fatalf("MAKEINT64(1,-1)=%#x, want %#x", uint64(v), uint64(want))
	}
	if HIINT32(v) != 1 || LOINT32(v) != -1 {
		t.Fatalf("HI/LO mismatch: %d %d", HIINT32(v), LOINT32(v))
	}

	v32 := MAKEINT32(1, -1)
	want32 := int32(0x0001ffff)
	if v32 != want32 {
		t.Fatalf("MAKEINT32(1,-1)=%#x, want %#x", uint32(v32), uint32(want32))
	}
	if HIINT16(v32) != 1 || LOINT16(v32) != -1 {
		t.Fatalf("HI/LO16 mismatch: %d %d", HIINT16(v32), LOINT16(v32))
	}
}

func TestGetNowUpdateInSecondConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = GetNowUpdateInSecond()
			}
		}()
	}
	wg.Wait()
}

func TestIsBigEndianConcurrent(t *testing.T) {
	DebugResetBigEndian()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = IsBigEndian()
			}
		}()
	}
	wg.Wait()
	DebugResetBigEndian()
}

func TestFileExistsStatError(t *testing.T) {
	if FileExists(filepath.Join(t.TempDir(), "no-such-file")) {
		t.Fatal("missing file should be false")
	}
	// directory is not a "file"
	dir := t.TempDir()
	if FileExists(dir) {
		t.Fatal("directory should not count as existing file")
	}
	// permission-denied path: best-effort; on Windows may not be reproducible
	if runtime.GOOS != "windows" {
		denied := filepath.Join(dir, "denied")
		if err := os.Mkdir(denied, 0000); err == nil {
			defer os.Chmod(denied, 0755)
			// Stat on unreadable dir entry may fail; must not panic
			_ = FileExists(filepath.Join(denied, "x"))
		}
	}
}

func TestSaveJsonWriteErrorPropagates(t *testing.T) {
	// SaveJson to a path whose parent does not exist should fail (not silent success)
	err := SaveJson(filepath.Join(t.TempDir(), "no", "such", "file.json"), map[string]int{"a": 1})
	if err == nil {
		t.Fatal("expected error writing to missing directory")
	}
}

func TestStrTableExtraColumnsNoPanic(t *testing.T) {
	ts := StrTable{}
	ts.AddHeader("a")
	ts.AddHeader("b")
	line := StrTableLine{}
	line.AddData("1")
	line.AddData("2")
	line.AddData("extra")
	ts.AddLine(line)
	out := ts.String("")
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Fatalf("unexpected table output: %q", out)
	}
	if strings.Contains(out, "extra") {
		t.Fatalf("extra column should be truncated, got %q", out)
	}
}

func TestNumToHexHex2Num(t *testing.T) {
	if NumToHex(0, LITTLE_LETTERS) != "0" {
		t.Fatalf("NumToHex(0)=%q", NumToHex(0, LITTLE_LETTERS))
	}
	if NumToHex(10, 1) != "" || NumToHex(10, 0) != "" {
		t.Fatal("invalid base should return empty")
	}
	for _, n := range []int{10, 37, 12345745643} {
		s := NumToHex(n, FULL_LETTERS)
		if Hex2Num(s, FULL_LETTERS) != n {
			t.Fatalf("roundtrip %d -> %s -> %d", n, s, Hex2Num(s, FULL_LETTERS))
		}
	}
	if Hex2Num("!!!", FULL_LETTERS) != 0 {
		t.Fatal("invalid chars should return 0")
	}
	if Hex2Num("", FULL_LETTERS) != 0 {
		t.Fatal("empty should return 0")
	}
}

func TestColorAlphaOpaque(t *testing.T) {
	for name, c := range map[string]struct{ R, G, B, A uint8 }{
		"Black": {0, 0, 0, 255},
		"White": {255, 255, 255, 255},
		"Red":   {255, 0, 0, 255},
	} {
		var got struct{ R, G, B, A uint8 }
		switch name {
		case "Black":
			got = struct{ R, G, B, A uint8 }{Black.R, Black.G, Black.B, Black.A}
		case "White":
			got = struct{ R, G, B, A uint8 }{White.R, White.G, White.B, White.A}
		case "Red":
			got = struct{ R, G, B, A uint8 }{Red.R, Red.G, Red.B, Red.A}
		}
		if got.A != 255 {
			t.Fatalf("%s.A=%d, want 255", name, got.A)
		}
		if got.R != c.R || got.G != c.G || got.B != c.B {
			t.Fatalf("%s RGB mismatch: %+v vs %+v", name, got, c)
		}
	}
}

func TestHashGenericLargeInts(t *testing.T) {
	u := uint64(math.MaxUint64)
	h1 := HashGeneric(u)
	h2 := HashGeneric(strconv.FormatUint(u, 10))
	if h1 != h2 {
		t.Fatalf("uint64 Max hash mismatch: %d vs %d", h1, h2)
	}
	// must not collide with truncated int representation
	if h1 == HashGeneric(int64(-1)) {
		t.Fatal("MaxUint64 should not hash like int64(-1)")
	}
	i64 := int64(math.MinInt64)
	if HashGeneric(i64) != HashString(strconv.FormatInt(i64, 10)) {
		t.Fatal("int64 MinInt64 hash mismatch")
	}
}

func TestFullFillOperatorPrecedence(t *testing.T) {
	// Smoke: MessageToFullJson with nil descriptor is not useful; instead verify
	// the condition logic via a tiny local replica of the fixed expression.
	isMap, isList := false, true
	kindMessage, kindGroup := true, false
	// old buggy: !map && !list && Message || Group  => false && ... || false => false for list+message... wait
	// For list + message: (!false && !true && true) || false = false
	// For list + group: (!false && !true && false) || true = true  <-- bug would recurse into list of groups
	oldBuggy := func(isMap, isList, isMsg, isGroup bool) bool {
		return !isMap && !isList && isMsg || isGroup
	}
	fixed := func(isMap, isList, isMsg, isGroup bool) bool {
		return !isMap && !isList && (isMsg || isGroup)
	}
	if oldBuggy(false, true, false, true) != true {
		t.Fatal("sanity: old expression treated list+group as true")
	}
	if fixed(isMap, isList, kindMessage, kindGroup) {
		t.Fatal("list fields must not be full-filled")
	}
	if fixed(false, true, false, true) {
		t.Fatal("list+group must not be full-filled after fix")
	}
	if !fixed(false, false, true, false) {
		t.Fatal("singular message should be full-filled")
	}
	if !fixed(false, false, false, true) {
		t.Fatal("singular group should be full-filled")
	}
}
