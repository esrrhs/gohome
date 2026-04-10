package common

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func elapsed() {
	defer Elapsed(func(d time.Duration) {
		fmt.Println("use time " + d.String())
	})()

	time.Sleep(time.Second)
}

func Test0001(t *testing.T) {

	a := RandStr(5)
	a1 := RandStr(5)
	fmt.Println(a)
	fmt.Println(a1)

	fmt.Println(GetOutboundIP())

	fmt.Println(GetNowUpdateInSecond())

	d, _ := Rc4("123456", []byte("asdgdsagdsag435t43321dsgesg"))
	fmt.Println(string(d))

	d, _ = Rc4("123456", d)
	fmt.Println(string(d))

	dd := MAKEINT64(12345, 7890)
	fmt.Println(dd)
	fmt.Println(HIINT32(dd))
	fmt.Println(LOINT32(dd))
	ddd := MAKEINT32(12345, 7890)
	fmt.Println(ddd)
	fmt.Println(HIINT16(ddd))
	fmt.Println(LOINT16(ddd))

	fmt.Println(IsInt(3.0002))
	fmt.Println(IsInt(3))
	fmt.Println(strconv.FormatFloat(3.1415, 'E', -1, 64))

	aa := []int{1, 2, 3, 4, 5, 6, 7, 8}
	Shuffle(len(aa), func(i, j int) { aa[i], aa[j] = aa[j], aa[i] })
	fmt.Println(aa)

	fmt.Println(RandInt())
	fmt.Println(RandInt31n(10))

	fmt.Println(WrapString("abc", 10))

	ts := StrTable{}
	ts.AddHeader("a")
	ts.AddHeader("b")
	ts.AddHeader("c")
	tsl := StrTableLine{}
	tsl.AddData("1234")
	tsl.AddData("123421412")
	ts.AddLine(tsl)
	tsl = StrTableLine{}
	tsl.AddData("aaa")
	ts.AddLine(tsl)
	fmt.Println(WrapString("abc", 10))
	fmt.Println(ts.String("\t"))

	elapsed()
}

type TestStruct struct {
	A int
	B int64
	C string
}

func Test0002(t *testing.T) {
	ts := TestStruct{1, 2, "3"}
	st := StrTable{}
	st.AddHeader("AA")
	st.FromStruct(&ts, func(name string) bool {
		return name != "A"
	})
	stl := StrTableLine{}
	stl.AddData("a")
	stl.FromStruct(&st, &ts, func(name string, v interface{}) interface{} {
		if name == "B" {
			return time.Duration(v.(int64)).String()
		}
		return v
	})
	st.AddLine(stl)
	ts = TestStruct{12, 214124, "124123"}
	stl = StrTableLine{}
	stl.AddData("aaa")
	stl.FromStruct(&st, &ts, func(name string, v interface{}) interface{} {
		if name == "B" {
			return time.Duration(v.(int64)).String()
		}
		return v
	})
	st.AddLine(stl)
	fmt.Println(st.String(""))

	SaveJson("test.json", &ts)
	ts1 := TestStruct{}
	err := LoadJson("test.json", &ts1)
	fmt.Println(err)
	fmt.Println(ts1.C)
}

func Test0003(t *testing.T) {
	a := NumToHex(12345745643, LITTLE_LETTERS)
	b := NumToHex(12345745643, FULL_LETTERS)
	fmt.Println(a)
	fmt.Println(b)
	fmt.Println(Hex2Num(a, LITTLE_LETTERS))
	fmt.Println(Hex2Num(b, FULL_LETTERS))
	aa := NumToHex(37, LITTLE_LETTERS)
	bb := NumToHex(37, FULL_LETTERS)
	fmt.Println(aa)
	fmt.Println(bb)
	cc := Hex2Num("1i39pJZR", FULL_LETTERS)
	fmt.Println(cc)
	fmt.Println(NumToHex(cc, FULL_LETTERS))
	fmt.Println(NumToHex(cc+1, FULL_LETTERS))

	dd := Hex2Num("ZZZZZZZZ", FULL_LETTERS)
	fmt.Println(dd)
	fmt.Println(NumToHex(dd, FULL_LETTERS))
}

type TestStruct1 struct {
	TestStruct
	D int64
}

func Test0004(t *testing.T) {
	ts := TestStruct{1, 2, "3"}
	ts1 := TestStruct1{ts, 3}
	fmt.Println(StructToTable(&ts1))
}

func Test0005(t *testing.T) {
	fmt.Println(GetXXHashString("1"))
	fmt.Println(GetXXHashString("2"))
	fmt.Println(GetXXHashString("asfaf"))
	fmt.Println(GetXXHashString("dffd43321"))
}

func Test0006(t *testing.T) {
	src := "safa3232sgsgd343q421dsdgsddsgsarwdsddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddgdsgsgewrngfxgcjfhrsgcbxgfhreu658545ghuj,hgfdtsz nsdtzbntshjtwg,tu523jlikr[]iwsfffffds23525ewfsu45632rqwfsgrxy4353rsfzshrey4324fASffdjftui4e22=-"
	fmt.Println(len(src))
	a := GzipString(src)
	fmt.Println(len(a))
	b := GzipStringBestCompression(src)
	fmt.Println(len(b))
	c := GzipStringBestSpeed(src)
	fmt.Println(len(c))

	if src != GunzipString(a) {
		t.Error("fail")
	}
	if src != GunzipString(b) {
		t.Error("fail")
	}
	if src != GunzipString(c) {
		t.Error("fail")
	}
}

func Test0007(t *testing.T) {
	fmt.Println(GetCrc32String(""))
	fmt.Println(GetCrc32String("1"))
	fmt.Println(GetCrc32String("2"))
	fmt.Println(GetCrc32String("asfsadgewwe"))
}

func Test0008(t *testing.T) {
	c := NewChannel(10)
	c.Write(1)
	i := <-c.Ch()
	fmt.Println(i)
	c.Close()
	c.Close()
	c.Write(1)
	c.Write(1)
}

func Test0009(t *testing.T) {
	c := NewChannel(1)
	c.Write(1)
	fmt.Println(c.WriteTimeout(1, 1000))
	fmt.Println(c.WriteTimeout(1, 1000))
	i := <-c.ch
	fmt.Println(i)
	fmt.Println(c.WriteTimeout(1, 1000))
	time.Sleep(time.Second)
}

func Test0010(t *testing.T) {
	a := make([]int, 3)
	a[0] = 1
	a[1] = 111
	a[2] = 1111
	fmt.Println(HasInt(a, 1))
	fmt.Println(HasInt(a, 12))
}

func Test0011(t *testing.T) {
	a := make([]string, 3)
	a[0] = "1"
	a[1] = "111"
	a[2] = "1111"
	fmt.Println(HasString(a, "1"))
	fmt.Println(HasString(a, "12"))
}

func Test0012(t *testing.T) {
	Copy("common.go", "common.go.1")
	fmt.Println(FileExists("common.go.1"))
	fmt.Println(FileMd5("common.go.1"))
	fmt.Println(FileReplace("common.go.1", "func", "fuck"))
	fmt.Println(IsSymlink("common.go.1"))
	fmt.Println(FileFind("common.go.1", "fuck ini()"))
	fmt.Println(FileLineCount("common.go.1"))
}

func Test0013(t *testing.T) {
	Walk("./", func(path string, info os.FileInfo, err error) error {
		fmt.Println(path)
		return nil
	})
}

func Test0014(t *testing.T) {
	fmt.Println(NearlyEqual(1, 10))
	fmt.Println(NearlyEqual(8, 10))
	fmt.Println(NearlyEqual(9, 10))
	fmt.Println(NearlyEqual(99, 100))
	fmt.Println(NearlyEqual(90000, 100000))
	fmt.Println(NearlyEqual(80000, 100000))
}

func Test0015(t *testing.T) {
	fmt.Println("start")
	Sleep(3)
	fmt.Println("end")
}

func Test00016(t *testing.T) {
	testIPs := []string{
		"192.168.1.1",
		"3.4.1.1",
		"10.0.0.55",
		"172.16.5.4",
		"8.8.8.8",
		"fc00::1",
		"fe80::abcd",
		"240e::1",
	}

	for _, ip := range testIPs {
		fmt.Printf("IP: %-15s → IsPrivate: %v\n", ip, IsPrivateIP(ip))
	}
}

func Test00017(t *testing.T) {
	fmt.Println(ResolveDomainToIP("www.baidu.com"))
	fmt.Println(ResolveDomainToIP("www.google.com"))
	fmt.Println(ResolveDomainToIP("www.qq.com"))
	fmt.Println(ResolveDomainToIP("www.taobao.com"))
	fmt.Println(ResolveDomainToIP("www.bing.com"))
}

func Test00018(t *testing.T) {
	fmt.Println(IsBigEndian())
	fmt.Println(IsBigEndian())
	fmt.Println(IsBigEndian())
}

func Test00019(t *testing.T) {
	fmt.Println(GetRootDomain("www.baidu.com"))
	fmt.Println(GetRootDomain("sub.domain.example.co.uk"))
	fmt.Println(GetRootDomain("example.com"))
	fmt.Println(GetRootDomain("localhost"))
	fmt.Println(GetRootDomain("a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.q.r.s.t.u.v.w.x.y.z.com"))
}

func TestMinOfInt(t *testing.T) {
	result := MinOfInt(3, 1, 4, 1, 5, 9)
	fmt.Println("MinOfInt(3,1,4,1,5,9):", result)
	if result != 1 {
		t.Errorf("MinOfInt(3,1,4,1,5,9) = %d, want 1", result)
	}

	result = MinOfInt(42)
	fmt.Println("MinOfInt(42):", result)
	if result != 42 {
		t.Errorf("MinOfInt(42) = %d, want 42", result)
	}

	result = MinOfInt(-5, -1, -10)
	fmt.Println("MinOfInt(-5,-1,-10):", result)
	if result != -10 {
		t.Errorf("MinOfInt(-5,-1,-10) = %d, want -10", result)
	}
}

func TestMaxOfInt(t *testing.T) {
	result := MaxOfInt(3, 1, 4, 1, 5, 9)
	fmt.Println("MaxOfInt(3,1,4,1,5,9):", result)
	if result != 9 {
		t.Errorf("MaxOfInt(3,1,4,1,5,9) = %d, want 9", result)
	}

	result = MaxOfInt(42)
	fmt.Println("MaxOfInt(42):", result)
	if result != 42 {
		t.Errorf("MaxOfInt(42) = %d, want 42", result)
	}

	result = MaxOfInt(-5, -1, -10)
	fmt.Println("MaxOfInt(-5,-1,-10):", result)
	if result != -1 {
		t.Errorf("MaxOfInt(-5,-1,-10) = %d, want -1", result)
	}
}

func TestMinOfInt64(t *testing.T) {
	result := MinOfInt64(100, 200, 50, 300)
	fmt.Println("MinOfInt64(100,200,50,300):", result)
	if result != 50 {
		t.Errorf("MinOfInt64(100,200,50,300) = %d, want 50", result)
	}

	result = MinOfInt64(int64(7))
	fmt.Println("MinOfInt64(7):", result)
	if result != 7 {
		t.Errorf("MinOfInt64(7) = %d, want 7", result)
	}
}

func TestMaxOfInt64(t *testing.T) {
	result := MaxOfInt64(100, 200, 50, 300)
	fmt.Println("MaxOfInt64(100,200,50,300):", result)
	if result != 300 {
		t.Errorf("MaxOfInt64(100,200,50,300) = %d, want 300", result)
	}

	result = MaxOfInt64(int64(7))
	fmt.Println("MaxOfInt64(7):", result)
	if result != 7 {
		t.Errorf("MaxOfInt64(7) = %d, want 7", result)
	}
}

func TestAbsInt(t *testing.T) {
	result := AbsInt(5)
	fmt.Println("AbsInt(5):", result)
	if result != 5 {
		t.Errorf("AbsInt(5) = %d, want 5", result)
	}

	result = AbsInt(-5)
	fmt.Println("AbsInt(-5):", result)
	if result != 5 {
		t.Errorf("AbsInt(-5) = %d, want 5", result)
	}

	result = AbsInt(0)
	fmt.Println("AbsInt(0):", result)
	if result != 0 {
		t.Errorf("AbsInt(0) = %d, want 0", result)
	}
}

func TestAbsInt32(t *testing.T) {
	result := AbsInt32(10)
	fmt.Println("AbsInt32(10):", result)
	if result != 10 {
		t.Errorf("AbsInt32(10) = %d, want 10", result)
	}

	result = AbsInt32(-10)
	fmt.Println("AbsInt32(-10):", result)
	if result != 10 {
		t.Errorf("AbsInt32(-10) = %d, want 10", result)
	}

	result = AbsInt32(0)
	fmt.Println("AbsInt32(0):", result)
	if result != 0 {
		t.Errorf("AbsInt32(0) = %d, want 0", result)
	}
}

func TestAbsInt64(t *testing.T) {
	result := AbsInt64(100)
	fmt.Println("AbsInt64(100):", result)
	if result != 100 {
		t.Errorf("AbsInt64(100) = %d, want 100", result)
	}

	result = AbsInt64(-100)
	fmt.Println("AbsInt64(-100):", result)
	if result != 100 {
		t.Errorf("AbsInt64(-100) = %d, want 100", result)
	}

	result = AbsInt64(0)
	fmt.Println("AbsInt64(0):", result)
	if result != 0 {
		t.Errorf("AbsInt64(0) = %d, want 0", result)
	}
}

func TestUniqueId(t *testing.T) {
	id1 := UniqueId()
	id2 := UniqueId()
	fmt.Println("UniqueId 1:", id1)
	fmt.Println("UniqueId 2:", id2)
	if id1 == "" {
		t.Errorf("UniqueId() returned empty string")
	}
	if id1 == id2 {
		t.Errorf("UniqueId() returned same value twice: %s", id1)
	}
}

func TestRandInt64(t *testing.T) {
	r1 := RandInt64()
	r2 := RandInt64()
	fmt.Println("RandInt64 1:", r1)
	fmt.Println("RandInt64 2:", r2)
	// Just verify it returns values (extremely unlikely to be equal)
	if r1 == r2 {
		fmt.Println("Warning: RandInt64 returned same value twice (unlikely but possible)")
	}
}

func TestArrayContainInt(t *testing.T) {
	a := []int{1, 2, 3, 4, 5}
	fmt.Println("ArrayContainInt([1,2,3,4,5], 3):", ArrayContainInt(a, 3))
	if !ArrayContainInt(a, 3) {
		t.Errorf("ArrayContainInt([1,2,3,4,5], 3) = false, want true")
	}

	fmt.Println("ArrayContainInt([1,2,3,4,5], 99):", ArrayContainInt(a, 99))
	if ArrayContainInt(a, 99) {
		t.Errorf("ArrayContainInt([1,2,3,4,5], 99) = true, want false")
	}

	fmt.Println("ArrayContainInt([], 1):", ArrayContainInt([]int{}, 1))
	if ArrayContainInt([]int{}, 1) {
		t.Errorf("ArrayContainInt([], 1) = true, want false")
	}
}

func TestArrayContainString(t *testing.T) {
	a := []string{"hello", "world", "foo"}
	fmt.Println("ArrayContainString contains 'world':", ArrayContainString(a, "world"))
	if !ArrayContainString(a, "world") {
		t.Errorf("ArrayContainString should find 'world'")
	}

	fmt.Println("ArrayContainString contains 'bar':", ArrayContainString(a, "bar"))
	if ArrayContainString(a, "bar") {
		t.Errorf("ArrayContainString should not find 'bar'")
	}

	fmt.Println("ArrayContainString empty slice:", ArrayContainString([]string{}, "x"))
	if ArrayContainString([]string{}, "x") {
		t.Errorf("ArrayContainString on empty slice should return false")
	}
}

func TestSafeDivide(t *testing.T) {
	result := SafeDivide(10, 3)
	fmt.Println("SafeDivide(10, 3):", result)
	if result != 3 {
		t.Errorf("SafeDivide(10, 3) = %d, want 3", result)
	}

	result = SafeDivide(10, 0)
	fmt.Println("SafeDivide(10, 0):", result)
	if result != 0 {
		t.Errorf("SafeDivide(10, 0) = %d, want 0", result)
	}

	result = SafeDivide(0, 5)
	fmt.Println("SafeDivide(0, 5):", result)
	if result != 0 {
		t.Errorf("SafeDivide(0, 5) = %d, want 0", result)
	}

	result = SafeDivide(-10, 2)
	fmt.Println("SafeDivide(-10, 2):", result)
	if result != -5 {
		t.Errorf("SafeDivide(-10, 2) = %d, want -5", result)
	}
}

func TestGenerateTLSConfig(t *testing.T) {
	config, err := GenerateTLSConfig("test-proto")
	fmt.Println("GenerateTLSConfig error:", err)
	if err != nil {
		t.Errorf("GenerateTLSConfig returned error: %v", err)
	}
	if config == nil {
		t.Errorf("GenerateTLSConfig returned nil config")
	}
	fmt.Println("GenerateTLSConfig config is non-nil:", config != nil)
	if config != nil && len(config.NextProtos) > 0 {
		fmt.Println("GenerateTLSConfig NextProtos:", config.NextProtos)
		if config.NextProtos[0] != "test-proto" {
			t.Errorf("GenerateTLSConfig NextProtos[0] = %s, want test-proto", config.NextProtos[0])
		}
	}
}

func TestHashString(t *testing.T) {
	h1 := HashString("hello")
	h2 := HashString("world")
	h3 := HashString("hello")
	fmt.Println("HashString('hello'):", h1)
	fmt.Println("HashString('world'):", h2)
	fmt.Println("HashString('hello') again:", h3)
	if h1 == h2 {
		t.Errorf("HashString('hello') == HashString('world'), expected different hashes")
	}
	if h1 != h3 {
		t.Errorf("HashString('hello') returned different values: %d vs %d", h1, h3)
	}
}

func TestHashInt(t *testing.T) {
	h1 := HashInt(1)
	h2 := HashInt(2)
	h3 := HashInt(1)
	fmt.Println("HashInt(1):", h1)
	fmt.Println("HashInt(2):", h2)
	fmt.Println("HashInt(1) again:", h3)
	if h1 == h2 {
		t.Errorf("HashInt(1) == HashInt(2), expected different hashes")
	}
	if h1 != h3 {
		t.Errorf("HashInt(1) returned different values: %d vs %d", h1, h3)
	}
}

func TestHashGeneric(t *testing.T) {
	hInt := HashGeneric(42)
	fmt.Println("HashGeneric(42):", hInt)
	if hInt == 0 {
		t.Errorf("HashGeneric(42) returned 0")
	}

	hStr := HashGeneric("test")
	fmt.Println("HashGeneric('test'):", hStr)
	if hStr == 0 {
		t.Errorf("HashGeneric('test') returned 0")
	}

	hFloat := HashGeneric(3.14)
	fmt.Println("HashGeneric(3.14):", hFloat)
	if hFloat == 0 {
		t.Errorf("HashGeneric(3.14) returned 0")
	}

	hBoolTrue := HashGeneric(true)
	hBoolFalse := HashGeneric(false)
	fmt.Println("HashGeneric(true):", hBoolTrue)
	fmt.Println("HashGeneric(false):", hBoolFalse)
	if hBoolTrue == hBoolFalse {
		t.Errorf("HashGeneric(true) == HashGeneric(false), expected different")
	}

	hBytes := HashGeneric([]byte("hello"))
	fmt.Println("HashGeneric([]byte('hello')):", hBytes)
	if hBytes == 0 {
		t.Errorf("HashGeneric([]byte('hello')) returned 0")
	}

	hNil := HashGeneric[any](nil)
	fmt.Println("HashGeneric(nil):", hNil)
	if hNil == 0 {
		t.Errorf("HashGeneric(nil) returned 0")
	}
}

func TestColorDistance(t *testing.T) {
	d := ColorDistance(Red, Red)
	fmt.Println("ColorDistance(Red, Red):", d)
	if d != 0 {
		t.Errorf("ColorDistance(Red, Red) = %f, want 0", d)
	}

	d = ColorDistance(Black, White)
	fmt.Println("ColorDistance(Black, White):", d)
	if d < 1 {
		t.Errorf("ColorDistance(Black, White) = %f, expected > 0", d)
	}

	d = ColorDistance(color.RGBA{0, 0, 0, 0}, color.RGBA{255, 0, 0, 0})
	fmt.Println("ColorDistance(Black, PureRed):", d)
	if d != 255 {
		t.Errorf("ColorDistance(Black, PureRed) = %f, want 255", d)
	}

	d1 := ColorDistance(Red, Blue)
	d2 := ColorDistance(Red, Lime)
	fmt.Println("ColorDistance(Red, Blue):", d1)
	fmt.Println("ColorDistance(Red, Lime):", d2)
	if d1 <= 0 || d2 <= 0 {
		t.Errorf("Expected positive distances between different colors")
	}
}

func TestIntArrayToString(t *testing.T) {
	result := IntArrayToString([]int{1, 2, 3}, ",")
	fmt.Println("IntArrayToString([1,2,3], ','):", result)
	if result != "1,2,3," {
		t.Errorf("IntArrayToString([1,2,3], ',') = %q, want %q", result, "1,2,3,")
	}

	result = IntArrayToString([]int{}, ",")
	fmt.Println("IntArrayToString([], ','):", result)
	if result != "" {
		t.Errorf("IntArrayToString([], ',') = %q, want empty", result)
	}

	result = IntArrayToString([]int{42}, "-")
	fmt.Println("IntArrayToString([42], '-'):", result)
	if result != "42-" {
		t.Errorf("IntArrayToString([42], '-') = %q, want %q", result, "42-")
	}
}

func TestInt32ArrayToString(t *testing.T) {
	result := Int32ArrayToString([]int32{10, 20, 30}, ";")
	fmt.Println("Int32ArrayToString([10,20,30], ';'):", result)
	if result != "10;20;30;" {
		t.Errorf("Int32ArrayToString([10,20,30], ';') = %q, want %q", result, "10;20;30;")
	}

	result = Int32ArrayToString([]int32{}, ",")
	fmt.Println("Int32ArrayToString([], ','):", result)
	if result != "" {
		t.Errorf("Int32ArrayToString([], ',') = %q, want empty", result)
	}
}

func TestInt64ArrayToString(t *testing.T) {
	result := Int64ArrayToString([]int64{100, 200, 300}, " ")
	fmt.Println("Int64ArrayToString([100,200,300], ' '):", result)
	if result != "100 200 300 " {
		t.Errorf("Int64ArrayToString([100,200,300], ' ') = %q, want %q", result, "100 200 300 ")
	}

	result = Int64ArrayToString([]int64{}, ",")
	fmt.Println("Int64ArrayToString([], ','):", result)
	if result != "" {
		t.Errorf("Int64ArrayToString([], ',') = %q, want empty", result)
	}
}

func TestGuid(t *testing.T) {
	g1 := Guid()
	g2 := Guid()
	fmt.Println("Guid 1:", g1)
	fmt.Println("Guid 2:", g2)
	if g1 == "" {
		t.Errorf("Guid() returned empty string")
	}
	if g2 == "" {
		t.Errorf("Guid() returned empty string")
	}
	if g1 == g2 {
		t.Errorf("Guid() returned same value twice: %s", g1)
	}
}

func TestDebugSetBigEndianAndReset(t *testing.T) {
	DebugResetBigEndian()
	original := IsBigEndian()
	fmt.Println("Original IsBigEndian:", original)

	DebugSetBigEndian(true)
	result := IsBigEndian()
	fmt.Println("After DebugSetBigEndian(true):", result)
	if !result {
		t.Errorf("After DebugSetBigEndian(true), IsBigEndian() = false, want true")
	}

	DebugSetBigEndian(false)
	result = IsBigEndian()
	fmt.Println("After DebugSetBigEndian(false):", result)
	if result {
		t.Errorf("After DebugSetBigEndian(false), IsBigEndian() = true, want false")
	}

	DebugResetBigEndian()
	result = IsBigEndian()
	fmt.Println("After DebugResetBigEndian:", result)
	if result != original {
		t.Errorf("After DebugResetBigEndian, IsBigEndian() = %v, want %v", result, original)
	}
}

func TestIsValidIP(t *testing.T) {
	fmt.Println("IsValidIP('192.168.1.1'):", IsValidIP("192.168.1.1"))
	if !IsValidIP("192.168.1.1") {
		t.Errorf("IsValidIP('192.168.1.1') = false, want true")
	}

	fmt.Println("IsValidIP('::1'):", IsValidIP("::1"))
	if !IsValidIP("::1") {
		t.Errorf("IsValidIP('::1') = false, want true")
	}

	fmt.Println("IsValidIP('10.0.0.1'):", IsValidIP("10.0.0.1"))
	if !IsValidIP("10.0.0.1") {
		t.Errorf("IsValidIP('10.0.0.1') = false, want true")
	}

	fmt.Println("IsValidIP('not-an-ip'):", IsValidIP("not-an-ip"))
	if IsValidIP("not-an-ip") {
		t.Errorf("IsValidIP('not-an-ip') = true, want false")
	}

	fmt.Println("IsValidIP(''):", IsValidIP(""))
	if IsValidIP("") {
		t.Errorf("IsValidIP('') = true, want false")
	}

	fmt.Println("IsValidIP('999.999.999.999'):", IsValidIP("999.999.999.999"))
	if IsValidIP("999.999.999.999") {
		t.Errorf("IsValidIP('999.999.999.999') = true, want false")
	}
}

func TestDumpStacks(t *testing.T) {
	result := DumpStacks()
	fmt.Println("DumpStacks length:", len(result))
	if result == "" {
		t.Errorf("DumpStacks() returned empty string")
	}
	if !strings.Contains(result, "goroutine") {
		t.Errorf("DumpStacks() does not contain 'goroutine': %s", result[:100])
	}
	fmt.Println("DumpStacks contains 'goroutine':", strings.Contains(result, "goroutine"))
}

func TestCompressDecompressData(t *testing.T) {
	src := []byte("hello world, this is a test for CompressData and DeCompressData")
	compressed := CompressData(src)
	fmt.Println("CompressData original size:", len(src), "compressed size:", len(compressed))
	if len(compressed) == 0 {
		t.Errorf("CompressData returned empty result")
	}

	decompressed, err := DeCompressData(compressed)
	fmt.Println("DeCompressData error:", err)
	if err != nil {
		t.Errorf("DeCompressData returned error: %v", err)
	}
	if string(decompressed) != string(src) {
		t.Errorf("DeCompressData result %q does not match original %q", string(decompressed), string(src))
	}
	fmt.Println("DeCompressData result matches original:", string(decompressed) == string(src))
}

func TestGetMd5String(t *testing.T) {
	h1 := GetMd5String("hello")
	h2 := GetMd5String("world")
	h3 := GetMd5String("hello")
	fmt.Println("GetMd5String('hello'):", h1)
	fmt.Println("GetMd5String('world'):", h2)
	fmt.Println("GetMd5String('hello') again:", h3)
	if h1 == "" {
		t.Errorf("GetMd5String returned empty string")
	}
	if h1 == h2 {
		t.Errorf("GetMd5String('hello') == GetMd5String('world'), expected different hashes")
	}
	if h1 != h3 {
		t.Errorf("GetMd5String('hello') returned different values: %s vs %s", h1, h3)
	}
}

func TestGetCrc32(t *testing.T) {
	h1 := GetCrc32([]byte("hello"))
	h2 := GetCrc32([]byte("world"))
	h3 := GetCrc32([]byte("hello"))
	fmt.Println("GetCrc32('hello'):", h1)
	fmt.Println("GetCrc32('world'):", h2)
	fmt.Println("GetCrc32('hello') again:", h3)
	if h1 == "" {
		t.Errorf("GetCrc32 returned empty string")
	}
	if h1 == h2 {
		t.Errorf("GetCrc32('hello') == GetCrc32('world'), expected different hashes")
	}
	if h1 != h3 {
		t.Errorf("GetCrc32('hello') returned different values: %s vs %s", h1, h3)
	}
	empty := GetCrc32([]byte(""))
	fmt.Println("GetCrc32(''):", empty)
	if empty == "" {
		t.Errorf("GetCrc32('') returned empty string")
	}
}

func TestFile_Copy(t *testing.T) {
src, err := os.CreateTemp("", "copy_src_*.txt")
if err != nil {
t.Fatal(err)
}
defer os.Remove(src.Name())
src.WriteString("hello copy")
src.Close()

dst := src.Name() + ".dst"
defer os.Remove(dst)

err = Copy(src.Name(), dst)
if err != nil {
t.Errorf("Copy returned error: %v", err)
}
if !FileExists(dst) {
t.Error("destination file does not exist after Copy")
}
}

func TestFile_FileReplace(t *testing.T) {
f, err := os.CreateTemp("", "replace_*.txt")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.WriteString("hello world foo bar foo")
f.Close()

err = FileReplace(f.Name(), "foo", "baz")
if err != nil {
t.Errorf("FileReplace returned error: %v", err)
}
count := FileFind(f.Name(), "baz")
fmt.Println("FileFind baz count:", count)
if count != 1 {
t.Errorf("expected 1 line with 'baz', got %d", count)
}
}

func TestFile_FileFind(t *testing.T) {
f, err := os.CreateTemp("", "find_*.txt")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.WriteString("apple\nbanana\napricot\norange\n")
f.Close()

n := FileFind(f.Name(), "ap")
fmt.Println("FileFind 'ap' count:", n)
if n != 2 {
t.Errorf("expected 2 lines matching 'ap', got %d", n)
}
n2 := FileFind(f.Name(), "mango")
if n2 != 0 {
t.Errorf("expected 0 lines matching 'mango', got %d", n2)
}
}

func TestFile_IsSymlink(t *testing.T) {
f, err := os.CreateTemp("", "symlink_target_*.txt")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.Close()

linkName := f.Name() + ".link"
defer os.Remove(linkName)

err = os.Symlink(f.Name(), linkName)
if err != nil {
t.Skip("cannot create symlink:", err)
}

if !IsSymlink(linkName) {
t.Error("expected IsSymlink true for symlink")
}
if IsSymlink(f.Name()) {
t.Error("expected IsSymlink false for regular file")
}
if IsSymlink("/nonexistent/path") {
t.Error("expected IsSymlink false for nonexistent path")
}
}

func TestFile_FileLineCount(t *testing.T) {
f, err := os.CreateTemp("", "linecount_*.txt")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.WriteString("line1\nline2\nline3\n")
f.Close()

n := FileLineCount(f.Name())
fmt.Println("FileLineCount:", n)
if n != 4 {
t.Errorf("expected FileLineCount 4, got %d", n)
}
n2 := FileLineCount("/nonexistent/file.txt")
if n2 != 0 {
t.Errorf("expected FileLineCount 0 for nonexistent file, got %d", n2)
}
}
