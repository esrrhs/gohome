package common

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	randv2 "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/esrrhs/gohome/loggo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ---------- net.go ----------

func TestHardenGetRootDomain(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"www.baidu.com", "baidu.com", false},
		{"sub.domain.example.co.uk", "example.co.uk", false},
		{"example.com", "example.com", false},
		{"localhost", "localhost", false},
		{"", "", false},
		{"127.0.0.1", "127.0.0.1", false},
		{"127.0.0.1:8080", "127.0.0.1", false},
		{"[::1]:8080", "::1", false},
		{"2001:db8::1", "2001:db8::1", false},
		{"www.baidu.com:443", "baidu.com", false},
		{"www.example.com.", "example.com", false},       // 根点 FQDN
		{"WWW.BAIDU.COM.", "BAIDU.COM", false},           // 根点 + 大写（eTLD+1 保留大小写）
		{"a.b.c.www.example.com.", "example.com", false}, // 多级 + 根点
		{".", "", false},                                 // 去掉根点后为空，走无点分支原样返回
		{"a..b.com", "", true},                           // 空 label 非法
		{".com", "", true},                               // 空 label 非法
	}
	for _, c := range cases {
		got, err := GetRootDomain(c.in)
		if c.err {
			if err == nil {
				t.Errorf("GetRootDomain(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("GetRootDomain(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("GetRootDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHardenIsPrivateIP(t *testing.T) {
	private := []string{
		"10.0.0.1", "10.255.255.255",
		"172.16.0.0", "172.31.255.255",
		"192.168.0.0", "192.168.255.255",
		"fc00::1", "fdff::1", "fe80::1",
	}
	public := []string{
		"8.8.8.8", "11.0.0.1", "9.255.255.255",
		"172.15.255.255", "172.32.0.1",
		"127.0.0.1", "169.254.1.1", "0.0.0.0",
		"240e::1", "2001:db8::1",
	}
	for _, ip := range private {
		if !IsPrivateIP(ip) {
			t.Errorf("IsPrivateIP(%q) = false, want true", ip)
		}
	}
	for _, ip := range public {
		if IsPrivateIP(ip) {
			t.Errorf("IsPrivateIP(%q) = true, want false", ip)
		}
	}
	if IsPrivateIP("not-an-ip") || IsPrivateIP("") {
		t.Error("invalid IP strings must be false")
	}
}

func TestHardenResolveDomainToIPFastPath(t *testing.T) {
	// 合法 IP（含空白）必须直接回显，不产生任何网络请求
	got, err := ResolveDomainToIP("  1.1.1.1  ")
	if err != nil || got != "1.1.1.1" {
		t.Fatalf("IP fast path = %q, %v", got, err)
	}
	got, err = ResolveDomainToIP("2001:db8::1")
	if err != nil || got != "2001:db8::1" {
		t.Fatalf("IPv6 fast path = %q, %v", got, err)
	}
}

// ---------- error.go ----------

var (
	crashLogOnce sync.Once
	crashLogSink muBuffer
)

type muBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (m *muBuffer) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *muBuffer) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.String()
}

func ensureCrashLogInit() {
	crashLogOnce.Do(func() {
		// 必须 NoLogFile，否则 loggo.Error 会在测试目录落 default_ERROR_*.log
		loggo.Ini(loggo.Config{
			Prefix:     "harden",
			NoLogFile:  true,
			NoPrint:    false,
			NoLogColor: true,
			Level:      loggo.LEVEL_ERROR,
		})
		loggo.SetPrinter(&crashLogSink)
	})
}

func invokeCrashLog(v any) any {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		func() {
			defer CrashLog()
			panic(v)
		}()
	}()
	return recovered
}

func TestHardenCrashLog(t *testing.T) {
	ensureCrashLogInit()

	// 无 panic 时 CrashLog 静默通过
	func() {
		defer CrashLog()
	}()

	rec := invokeCrashLog("boom-string")
	err, ok := rec.(error)
	if !ok || err.Error() != "boom-string" {
		t.Fatalf("string panic recovered = %v (%T)", rec, rec)
	}
	out := crashLogSink.String()
	if !strings.Contains(out, "crash boom-string") || !strings.Contains(out, "goroutine stack dump") {
		t.Fatalf("crash log missing body/stack: %q", out)
	}

	rec = invokeCrashLog(io.EOF)
	if err, ok := rec.(error); !ok || !errors.Is(err, io.EOF) {
		t.Fatalf("error panic must be rethrown as-is, got %v", rec)
	}

	rec = invokeCrashLog(42)
	if err, ok := rec.(error); !ok || err.Error() != "Unknown panic" {
		t.Fatalf("other panic recovered = %v, want Unknown panic error", rec)
	}
	if !strings.Contains(crashLogSink.String(), "Unknown panic") {
		t.Fatal("Unknown panic must appear in crash log")
	}
}

// ---------- file.go ----------

func TestHardenLoadJsonFallback(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "conf.json")
	back := main + ".back"

	// 主文件损坏 + back 合法 -> 从 back 恢复
	if err := os.WriteFile(main, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveJson(back, &TestStruct{A: 7, B: 8, C: "fromback"}); err != nil {
		t.Fatal(err)
	}
	var v TestStruct
	if err := LoadJson(main, &v); err != nil {
		t.Fatalf("LoadJson must fall back to .back: %v", err)
	}
	if v.C != "fromback" || v.A != 7 {
		t.Fatalf("loaded %+v, want values from .back", v)
	}

	// 主文件合法 -> 不需要 back
	v = TestStruct{}
	if err := SaveJson(main, &TestStruct{A: 9, C: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := LoadJson(main, &v); err != nil {
		t.Fatalf("LoadJson main: %v", err)
	}
	if v.C != "main" {
		t.Fatalf("loaded %+v, want main file values", v)
	}

	// 主文件与 back 都不存在 -> 返回错误
	var v2 TestStruct
	if err := LoadJson(filepath.Join(dir, "missing.json"), &v2); err == nil {
		t.Fatal("LoadJson on missing file and missing back must error")
	}
}

func TestHardenSaveJsonWritesBoth(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "save.json")
	if err := SaveJson(p, &TestStruct{A: 1, B: 2, C: "3"}); err != nil {
		t.Fatal(err)
	}
	if !FileExists(p) || !FileExists(p+".back") {
		t.Fatal("SaveJson must write both main and .back")
	}
}

func TestHardenFileMd5(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "md5.bin")
	content := []byte("abc")
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := FileMd5(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(content)
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("FileMd5 = %q, want %q", got, want)
	}
	if _, err := FileMd5(filepath.Join(dir, "no-such")); err == nil {
		t.Fatal("FileMd5 on missing file must error")
	}
}

func TestHardenFileErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if err := Copy(missing, filepath.Join(dir, "dst")); err == nil {
		t.Error("Copy missing src must error")
	}
	if err := FileReplace(missing, "a", "b"); err == nil {
		t.Error("FileReplace missing file must error")
	}
}

func TestHardenWalkSymlinks(t *testing.T) {
	dir := t.TempDir()
	// 普通目录树
	root := filepath.Join(dir, "root")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	// 符号链接 -> 目录：遍历必须跟随并访问链接路径下的文件
	realDir := filepath.Join(dir, "realdir")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "linked.txt"), []byte("l"), 0644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skip("symlink unsupported:", err)
	}
	// 符号链接 -> 普通文件
	linkFile := filepath.Join(root, "linkfile")
	if err := os.Symlink(filepath.Join(sub, "a.txt"), linkFile); err != nil {
		t.Skip("symlink unsupported:", err)
	}

	var visited []string
	if err := Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	joined := strings.Join(visited, "\n")
	if !strings.Contains(joined, filepath.Join("linkdir", "linked.txt")) {
		t.Fatalf("symlink-to-dir must be followed, visited:\n%s", joined)
	}
	if !FileExists(filepath.Join(root, "linkfile")) {
		t.Fatal("sanity")
	}
	if !strings.Contains(joined, "linkfile") {
		t.Fatalf("symlink-to-file must be visited, visited:\n%s", joined)
	}

	// 悬挂符号链接：EvalSymlinks 失败必须向上返回错误
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(dir, "gone"), dangling); err != nil {
		t.Skip("symlink unsupported:", err)
	}
	err := Walk(root, func(path string, info os.FileInfo, err error) error { return nil })
	if err == nil {
		t.Fatal("Walk with dangling symlink must return the EvalSymlinks error")
	}

	// walkFn 返回的普通错误必须中断并原样返回
	sentinel := errors.New("stop walk")
	err = Walk(root, func(path string, info os.FileInfo, err error) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("Walk must propagate walkFn error, got %v", err)
	}
}

func TestHardenJsonMarshalError(t *testing.T) {
	// chan 无法 JSON 序列化：saveJson 的 MarshalIndent 错误必须透出
	dir := t.TempDir()
	if err := SaveJson(filepath.Join(dir, "x.json"), make(chan int)); err == nil {
		t.Fatal("SaveJson must propagate marshal error")
	}
	var c chan int
	if err := saveJson(filepath.Join(dir, "y.json"), c); err == nil {
		t.Fatal("saveJson on unmarshalable value must error")
	}

	// 主文件可写、.back 路径是目录：主文件成功而 back OpenFile 失败，必须返回 err2
	base := filepath.Join(dir, "dup.json")
	if err := os.Mkdir(base+".back", 0755); err != nil {
		t.Fatal(err)
	}
	if err := SaveJson(base, &TestStruct{A: 1}); err == nil {
		t.Fatal("SaveJson must fail when .back cannot be opened as a file")
	}
}

func TestHardenCopyReplaceDstErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	// 目标父目录不存在：os.Create 失败
	badDst := filepath.Join(dir, "no", "dst")
	if err := Copy(src, badDst); err == nil {
		t.Error("Copy to missing directory must error")
	}
	if err := FileReplace(src, "a", "b"); err != nil {
		t.Fatal(err)
	}
	// FileReplace 读取成功后重建文件失败：文件只读，O_WRONLY|O_TRUNC 被拒
	roFile := filepath.Join(dir, "ro.txt")
	if err := os.WriteFile(roFile, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(roFile, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roFile, 0644) })
	if os.Geteuid() != 0 {
		if err := FileReplace(roFile, "a", "b"); err == nil {
			t.Error("FileReplace must fail when it cannot reopen the file for writing")
		}
	}
}

func TestHardenStrTableEmpty(t *testing.T) {
	if got := (&StrTable{}).String(""); got != "" {
		t.Errorf("table without header must render empty, got %q", got)
	}
}

// ---------- hash.go ----------

func TestHardenHashGenericAllKinds(t *testing.T) {
	// 各整型与对应手工构造保持一致
	if HashGeneric(int8(-1)) != HashInt(-1) {
		t.Error("int8 mismatch")
	}
	if HashGeneric(int16(-2)) != HashInt(-2) {
		t.Error("int16 mismatch")
	}
	if HashGeneric(int32(-3)) != HashInt(-3) {
		t.Error("int32 mismatch")
	}
	if HashGeneric(uint8(255)) != HashInt(255) {
		t.Error("uint8 mismatch")
	}
	if HashGeneric(uint16(65535)) != HashInt(65535) {
		t.Error("uint16 mismatch")
	}
	if HashGeneric(uint(123)) != HashString("123") {
		t.Error("uint mismatch")
	}
	if HashGeneric(uint32(456)) != HashString("456") {
		t.Error("uint32 mismatch")
	}
	if HashGeneric(int64(-789)) != HashString("-789") {
		t.Error("int64 mismatch")
	}
	if HashGeneric(uint64(789)) != HashString("789") {
		t.Error("uint64 mismatch")
	}
	if HashGeneric(float32(1.5)) != HashString(fmt.Sprintf("%f", float32(1.5))) {
		t.Error("float32 mismatch")
	}
	if HashGeneric(1.5) != HashString(fmt.Sprintf("%f", 1.5)) {
		t.Error("float64 mismatch")
	}
	if HashGeneric(true) != HashString("true") || HashGeneric(false) != HashString("false") {
		t.Error("bool mismatch")
	}
	if HashGeneric([]byte("x")) != HashString("x") {
		t.Error("[]byte mismatch")
	}
	if HashGeneric("x") != HashString("x") {
		t.Error("string mismatch")
	}
	if HashGeneric[any](nil) != HashString("nil") {
		t.Error("nil mismatch")
	}

	// 不支持的类型必须 panic
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("HashGeneric(struct{}{}) must panic")
			}
		}()
		_ = HashGeneric(struct{}{})
	}()
}

// ---------- alg.go ----------

func TestHardenRc4KeySize(t *testing.T) {
	if _, err := Rc4("", nil); err == nil {
		t.Error("empty key must be rejected")
	}
	if _, err := Rc4(strings.Repeat("k", 257), nil); err == nil {
		t.Error("257-byte key must be rejected")
	}
	// 同一 key 首次与缓存命中两条路径输出一致
	a, err := Rc4("cache-key", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Rc4("cache-key", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("cached rc4 state must produce identical output")
	}
}

func TestHardenDecompressErrors(t *testing.T) {
	if _, err := DeCompressData([]byte("not zlib at all")); err == nil {
		t.Error("garbage must fail zlib NewReader")
	}
	if _, err := DeCompressDataZstd([]byte("not zstd at all")); err == nil {
		t.Error("garbage must fail zstd DecodeAll")
	}
	// 截断的合法帧走无 FCS 快速通道，DecodeAll 报错并归还 pool buffer
	frame := CompressDataZstd([]byte("a-frame-that-gets-cut"))
	if _, err := DeCompressDataZstd(frame[:2]); err == nil {
		t.Error("truncated zstd frame must error")
	}
	// 连续多次失败后再成功，pool 缓冲仍可正常工作
	if _, err := DeCompressDataZstd(frame); err != nil {
		t.Fatalf("pool reuse after error: %v", err)
	}
}

func TestHardenZstdLargeBufferReset(t *testing.T) {
	// 不可压缩的 >1MB 载荷使编码结果超过 zstdBufMax，
	// 触发 pool 缓冲丢弃重建分支；功能与往返必须正确
	big := make([]byte, 2<<20)
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i*7 + 3)
	}
	randv2.NewChaCha8(seed).Read(big) // ChaCha8 随机字节，不可压缩
	enc := CompressDataZstd(big)
	if len(enc) <= zstdBufMax {
		t.Skipf("payload compressed to %d <= %d, reset branch not exercised", len(enc), zstdBufMax)
	}
	dec, err := DeCompressDataZstd(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, big) {
		t.Fatal("large payload roundtrip mismatch")
	}
	// 大缓冲被丢弃后，后续小编码仍正常（pool 已补充默认容量缓冲）
	small := CompressDataZstd([]byte("small-after-large"))
	if got, err := DeCompressDataZstd(small); err != nil || string(got) != "small-after-large" {
		t.Fatalf("pool after reset: %q %v", got, err)
	}
}

func TestHardenGzipEdgeCases(t *testing.T) {
	if got := GunzipString("definitely not gzip"); got != "" {
		t.Errorf("invalid gzip must return empty, got %q", got)
	}
	for _, src := range []string{"", "roundtrip-内容", strings.Repeat("abc", 1000)} {
		if got := GunzipString(GzipString(src)); got != src {
			t.Errorf("gzip roundtrip mismatch, want %q got %q", src, got)
		}
	}
}

func TestHardenIsBigEndianDetect(t *testing.T) {
	DebugResetBigEndian()
	first := IsBigEndian()
	// 再次调用命中缓存，结果必须一致
	if IsBigEndian() != first {
		t.Error("cached endianness differs from detected value")
	}
	DebugResetBigEndian()
}

// ---------- channel.go ----------

func TestHardenWriteTimeoutNonBlocking(t *testing.T) {
	c := NewChannel(1)
	if !c.WriteTimeout(1, 0) {
		t.Fatal("empty channel non-blocking write must succeed")
	}
	// buffer 已满：<=0 立即失败，不得阻塞
	if c.WriteTimeout(2, 0) {
		t.Error("full channel non-blocking write must fail")
	}
	if c.WriteTimeout(2, -5) {
		t.Error("negative timeout must behave non-blocking")
	}
	// 消费后立刻可写
	<-c.Ch()
	if !c.WriteTimeout(3, 0) {
		t.Error("freed slot must accept write")
	}
	c.Close()
	if c.WriteTimeout(4, 0) {
		t.Error("closed channel must reject write")
	}
}

// ---------- string.go ----------

func TestHardenNumBaseNegative(t *testing.T) {
	if got := NumToHex(-255, 16); got != "-ff" {
		t.Errorf("NumToHex(-255,16) = %q", got)
	}
	if got := Hex2Num("-ff", 16); got != -255 {
		t.Errorf("Hex2Num(-ff,16) = %d", got)
	}
	if got := NumToHex(math.MinInt64, 16); got != "-8000000000000000" {
		t.Errorf("NumToHex(MinInt64) = %q", got)
	}
	if got := Hex2Num("-8000000000000000", 16); got != math.MinInt64 {
		t.Errorf("Hex2Num MinInt64 = %d", got)
	}
	if got := Hex2Num("-", 16); got != 0 {
		t.Errorf("lone minus must return 0, got %d", got)
	}
	// 非法进制
	if got := Hex2Num("1", 1); got != 0 {
		t.Errorf("base 1 must return 0, got %d", got)
	}
}

// ---------- time.go ----------

func TestHardenGetNowUpdateInSecond(t *testing.T) {
	a := GetNowUpdateInSecond()
	if a.IsZero() {
		t.Fatal("cached time must not be zero")
	}
	// 后台每秒刷新；这里只要求返回值落在合理窗口内
	now := time.Now()
	if a.After(now.Add(2*time.Second)) || a.Before(now.Add(-5*time.Second)) {
		t.Fatalf("cached time %v out of window around %v", a, now)
	}
}

// ---------- math.go ----------

func TestHardenSafeDivideMinInt(t *testing.T) {
	// 除 0 已在既有用例覆盖；MinInt64/-1 与 Go 原生语义一致（溢出回绕），不得 panic
	v := SafeDivide(math.MinInt64, -1)
	if v != -v {
		// 回绕后 v == -v（MinInt64），以此确认没有 panic 且值符合回绕预期
		t.Fatalf("SafeDivide(MinInt64,-1)=%d, want wrap-around", v)
	}
}

// ---------- proto.go ----------

func buildTestFDSPBytes(t *testing.T) []byte {
	t.Helper()
	labelOpt := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	labelRep := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	typeStr := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeMsg := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("harden.proto"),
		Package: proto.String("hardentest"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("v"),
						Number: proto.Int32(1),
						Label:  &labelOpt,
						Type:   &typeStr,
					},
				},
			},
			{
				Name: proto.String("Outer"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("inner"),
						Number:   proto.Int32(1),
						Label:    &labelOpt,
						Type:     &typeMsg,
						TypeName: proto.String(".hardentest.Inner"),
					},
					{
						Name:   proto.String("names"),
						Number: proto.Int32(2),
						Label:  &labelRep,
						Type:   &typeStr,
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("Svc"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("M"),
						InputType:  proto.String(".hardentest.Outer"),
						OutputType: proto.String(".hardentest.Outer"),
					},
				},
			},
		},
	}
	b, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHardenProtobufRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "desc.pb")
	if err := os.WriteFile(p, buildTestFDSPBytes(t), 0644); err != nil {
		t.Fatal(err)
	}

	err, descs := LoadProtobuf(p)
	if err != nil {
		t.Fatalf("LoadProtobuf: %v", err)
	}
	if len(descs) != 1 || descs[0].Path() != "harden.proto" {
		t.Fatalf("unexpected descriptors: %v", descs)
	}
	outer := descs[0].Messages().ByName("Outer")
	if outer == nil {
		t.Fatal("Outer message descriptor not found")
	}
	inner := descs[0].Messages().ByName("Inner")
	if inner == nil {
		t.Fatal("Inner message descriptor not found")
	}

	// fullFill 递归填充单条消息字段，repeated 字段不展开
	err, js := MessageToFullJson(outer)
	if err != nil {
		t.Fatalf("MessageToFullJson: %v", err)
	}
	if !strings.Contains(js, `"inner":`) || !strings.Contains(js, `"v":`) {
		t.Fatalf("nested message must be full-filled, got:\n%s", js)
	}
	// 无嵌套消息的 Inner 也应正常序列化
	err, jsInner := MessageToFullJson(inner)
	if err != nil || !strings.Contains(jsInner, `"v":`) {
		t.Fatalf("Inner json: %v %q", err, jsInner)
	}

	err, methods := LoadProtobufMethods(p)
	if err != nil {
		t.Fatalf("LoadProtobufMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("want 1 method, got %d", len(methods))
	}
	if methods[0].FullName() != protoreflect.FullName("hardentest.Svc.M") {
		t.Fatalf("method full name = %q", methods[0].FullName())
	}
	if methods[0].Input().FullName() != "hardentest.Outer" {
		t.Fatalf("method input = %q", methods[0].Input().FullName())
	}

	// 描述符自相矛盾（方法引用不存在的类型）：protodesc.NewFiles 必须报错
	badFDS := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:    proto.String("broken.proto"),
		Package: proto.String("brokentest"),
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Svc"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("M"),
				InputType:  proto.String(".brokentest.Missing"),
				OutputType: proto.String(".brokentest.Missing"),
			}},
		}},
	}}}
	bb, err := proto.Marshal(badFDS)
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.pb")
	if err := os.WriteFile(broken, bb, 0644); err != nil {
		t.Fatal(err)
	}
	if err, _ := LoadProtobuf(broken); err == nil {
		t.Fatal("unresolved references in descriptor set must error")
	}
	if err, _ := LoadProtobufMethods(broken); err == nil {
		t.Fatal("LoadProtobufMethods must propagate descriptor errors")
	}

	// 文件不存在
	if err, _ := LoadProtobuf(filepath.Join(dir, "missing.pb")); err == nil {
		t.Fatal("missing file must error")
	}
	// 内容非法
	bad := filepath.Join(dir, "bad.pb")
	if err := os.WriteFile(bad, bytes.Repeat([]byte{0xff}, 32), 0644); err != nil {
		t.Fatal(err)
	}
	if err, _ := LoadProtobuf(bad); err == nil {
		t.Fatal("corrupt descriptor set must error")
	}
}
