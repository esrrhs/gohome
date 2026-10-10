package loggo

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetLogHandles 关闭并清空缓存的日志句柄，供需要独立日志目录的用例做前后清理。
func resetLogHandles() {
	logFileMu.Lock()
	for l, f := range logFiles {
		_ = f.Close()
		delete(logFiles, l)
	}
	logFilePrefix = ""
	logFileDate = ""
	logFileMu.Unlock()
}

func TestGrayscaleAllScales(t *testing.T) {
	cases := []struct {
		scale uint8
		index int
	}{
		{0x08, 232}, {0x12, 233}, {0x1c, 234}, {0x26, 235},
		{0x30, 236}, {0x3a, 237}, {0x44, 238}, {0x4e, 239},
		{0x58, 240}, {0x62, 241}, {0x6c, 242}, {0x76, 243},
		{0x80, 244}, {0x8a, 245}, {0x94, 246}, {0x9e, 247},
		{0xa8, 248}, {0xb2, 249}, {0xbc, 250}, {0xc6, 251},
		{0xd0, 252}, {0xda, 253}, {0xe4, 254}, {0xee, 255},
	}
	for _, tc := range cases {
		fg, ok := grayscale(tc.scale, true)
		if !ok || !bytes.Equal(fg, fgTermRGB[tc.index]) {
			t.Errorf("grayscale(%#x, fg) = %q, ok=%v, want %q", tc.scale, fg, ok, fgTermRGB[tc.index])
		}
		bg, ok := grayscale(tc.scale, false)
		if !ok || !bytes.Equal(bg, bgTermRGB[tc.index]) {
			t.Errorf("grayscale(%#x, bg) = %q, ok=%v, want %q", tc.scale, bg, ok, bgTermRGB[tc.index])
		}
		// color() 必须真的采用灰度结果而不是走通用 RGB 近似
		if got := color(tc.scale, tc.scale, tc.scale, true); !bytes.Equal(got, fgTermRGB[tc.index]) {
			t.Errorf("color grayscale fg %#x = %q, want %q", tc.scale, got, fgTermRGB[tc.index])
		}
	}

	// 不在灰度梯度上的等 RGB 值退回通用近似
	if got, ok := grayscale(1, true); ok || got != nil {
		t.Errorf("grayscale(1) miss = %q,%v, want nil,false", got, ok)
	}
	if got := color(1, 2, 3, true); !bytes.Equal(got, fgTermRGB[16]) {
		t.Errorf("color(1,2,3) general case = %q, want fg term 16 %q", got, fgTermRGB[16])
	}
	// 255,0,0 -> r6=5,g6=0,b6=0 -> i=180 -> bgcolors[180] == bgTermRGB[196]
	if got := color(255, 0, 0, false); !bytes.Equal(got, bgTermRGB[196]) {
		t.Errorf("color(255,0,0,bg) = %q, want %q", got, bgTermRGB[196])
	}

	// Bytes 同时带前景与背景：包含完整转义包装
	out := Bytes([]byte("xy"), 10, 20, 30, 40, 50, 60)
	if !bytes.HasPrefix(out, []byte("\033[")) ||
		!bytes.HasSuffix(out, reset) ||
		!bytes.Contains(out, []byte("xy")) {
		t.Errorf("Bytes escape wrapper mismatch: %q", out)
	}
}

func TestDumpStacks(t *testing.T) {
	s := dumpStacks()
	if !strings.Contains(s, "=== BEGIN goroutine stack dump ===") ||
		!strings.Contains(s, "=== END goroutine stack dump ===") {
		t.Fatalf("dump markers missing: %q", s)
	}
	if !strings.Contains(s, "TestDumpStacks") {
		t.Fatalf("dump must contain current test frame, got: %q", s)
	}
}

// invokeCrashLog 在两层函数里触发 panic 并让 crashLog 恢复后重新抛出，
// 返回最外层 recover 到的值和 Error 写出的崩溃日志。
func invokeCrashLog(t *testing.T, panicValue any) (any, string) {
	t.Helper()
	orig := gConfig
	t.Cleanup(func() { gConfig = orig })

	var buf bytes.Buffer
	SetPrinter(&buf)
	gConfig.Level = LEVEL_ERROR
	gConfig.NoPrint = false
	gConfig.NoLogFile = true
	gConfig.NoLogColor = true

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		func() {
			defer crashLog()
			panic(panicValue)
		}()
	}()
	return recovered, buf.String()
}

func TestCrashLog(t *testing.T) {
	// 字符串 panic -> 转成 errors.New
	rec, out := invokeCrashLog(t, "boom string")
	err, ok := rec.(error)
	if !ok || err.Error() != "boom string" {
		t.Fatalf("recovered = %v (%T), want error boom string", rec, rec)
	}
	if !strings.Contains(out, "crash boom string") ||
		!strings.Contains(out, "BEGIN goroutine stack dump") {
		t.Fatalf("crash log output missing body/stack: %q", out)
	}

	// error 类型 panic -> 原样重新抛出
	rec, _ = invokeCrashLog(t, io.EOF)
	if !errors.Is(rec.(error), io.EOF) {
		t.Fatalf("error panic must be rethrown as-is, got %v", rec)
	}

	// 其他类型 -> Unknown panic
	rec, out = invokeCrashLog(t, 42)
	if err, ok := rec.(error); !ok || err.Error() != "Unknown panic" {
		t.Fatalf("non-string/non-error panic recovered = %v, want Unknown panic error", rec)
	}
	if !strings.Contains(out, "Unknown panic") {
		t.Fatalf("expected Unknown panic in output, got %q", out)
	}
}

func TestCheckDateRetention(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	orig := gConfig
	t.Cleanup(func() { gConfig = orig })
	// 本轮不期望任何 Error 输出；双 sink 关闭即可
	gConfig.NoLogFile = true
	gConfig.NoPrint = true

	const prefix = "rot"
	cfg := Config{Prefix: prefix, MaxDay: 1}
	today := time.Now().Format("2006-01-02")
	old := time.Now().AddDate(0, 0, -5).Format("2006-01-02")

	mustCreate := func(name string) {
		t.Helper()
		if err := os.WriteFile(name, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	expired := prefix + "_DEBUG_" + old + ".log"
	fresh := prefix + "_INFO_" + today + ".log"
	foreign := "other_WARN_" + old + ".log"
	noSuffix := prefix + "_DEBUG_" + old + ".txt"
	shortName := "x.log"
	badDate := prefix + "_WARN_not-a-date.log"
	for _, name := range []string{expired, fresh, foreign, noSuffix, shortName, badDate} {
		mustCreate(name)
	}

	checkDate(cfg)

	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired log %s must be removed, stat err=%v", expired, err)
	}
	for _, name := range []string{fresh, foreign, noSuffix, shortName, badDate} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("file %s must be kept, got err=%v", name, err)
		}
	}
}

// 删除失败（无写权限的目录）时，修复后的 checkDate 必须打出 Error 而不是静默吞掉。
func TestCheckDateRemoveFailureLogged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory DAC permissions, cannot force os.Remove failure")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "ro")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	oldName := "rot_DEBUG_" + time.Now().AddDate(0, 0, -5).Format("2006-01-02") + ".log"
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	t.Chdir(dir)

	orig := gConfig
	t.Cleanup(func() { gConfig = orig })
	var buf bytes.Buffer
	SetPrinter(&buf)
	gConfig.Level = LEVEL_ERROR
	gConfig.NoPrint = false
	gConfig.NoLogFile = true
	gConfig.NoLogColor = true

	checkDate(Config{Prefix: "rot", MaxDay: 1})

	if !strings.Contains(buf.String(), "delete log file fail") {
		t.Fatalf("expected remove-failure error log, got %q", buf.String())
	}
	if _, err := os.Stat(oldName); err != nil {
		t.Fatalf("file must still exist after failed remove, got err=%v", err)
	}
}

func TestWarnErrorMirrorFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	resetLogHandles()
	t.Cleanup(resetLogHandles)

	orig := gConfig
	t.Cleanup(func() { gConfig = orig })

	gConfig.Prefix = "mirrortest"
	gConfig.NoLogFile = false
	gConfig.NoPrint = true
	gConfig.Level = LEVEL_DEBUG
	date := time.Now().Format("2006-01-02")

	filePath := func(level, prefix string) string {
		return prefix + "_" + level + "_" + date + ".log"
	}
	fileContains := func(path, want string) bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), want)
	}

	// DEBUG 级别下 Debug/Info 自身也落文件，且 Info 镜像进 DEBUG
	Debug("debugbody-1")
	Info("infobody-1")
	if !fileContains(filePath("DEBUG", "mirrortest"), "debugbody-1") {
		t.Error("Debug must write the DEBUG file at DEBUG level")
	}
	if !fileContains(filePath("INFO", "mirrortest"), "infobody-1") ||
		!fileContains(filePath("DEBUG", "mirrortest"), "infobody-1") {
		t.Error("Info must land in INFO and mirror to DEBUG at DEBUG level")
	}

	Warn("warnbody-1")
	for _, level := range []string{"WARN", "INFO", "DEBUG"} {
		if !fileContains(filePath(level, "mirrortest"), "warnbody-1") {
			t.Errorf("WARN must mirror into %s file", level)
		}
	}
	if _, err := os.Stat(filePath("ERROR", "mirrortest")); !os.IsNotExist(err) {
		t.Errorf("ERROR file must not exist before any Error log, err=%v", err)
	}

	Error("errorbody-1")
	for _, level := range []string{"ERROR", "WARN", "INFO", "DEBUG"} {
		if !fileContains(filePath(level, "mirrortest"), "errorbody-1") {
			t.Errorf("ERROR must mirror into %s file", level)
		}
	}

	// Level=INFO：Warn 只落 WARN/INFO，不进 DEBUG
	resetLogHandles()
	gConfig.Prefix = "mirrorinfo"
	gConfig.Level = LEVEL_INFO
	Warn("warnbody-2")
	if !fileContains(filePath("WARN", "mirrorinfo"), "warnbody-2") ||
		!fileContains(filePath("INFO", "mirrorinfo"), "warnbody-2") {
		t.Error("at INFO level Warn must land in WARN and INFO files")
	}
	if _, err := os.Stat(filePath("DEBUG", "mirrorinfo")); !os.IsNotExist(err) {
		t.Errorf("at INFO level Warn must not create DEBUG file, err=%v", err)
	}
}

func TestIniReinitAndPanics(t *testing.T) {
	origCfg := gConfig
	origInited := gInited
	t.Cleanup(func() {
		gConfig = origCfg
		gInited = origInited
	})

	// 已初始化后再次 Ini：早退，不覆盖现有配置，不开新 goroutine（NoLogFile 与否都直接返回）
	gInited = true
	gConfig.Prefix = "already"
	Ini(Config{Prefix: "second", NoLogFile: true})
	if gConfig.Prefix != "already" {
		t.Fatalf("re-Ini must be a no-op, prefix changed to %q", gConfig.Prefix)
	}

	// 回到未初始化态才能触达参数校验分支（NoLogFile 避免重定向 stderr/起 goroutine）
	gInited = false
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("empty prefix must panic")
			}
		}()
		Ini(Config{Prefix: "", NoLogFile: true})
	}()

	gInited = false
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("underscore in prefix must panic")
			}
		}()
		Ini(Config{Prefix: "a_b", NoLogFile: true})
	}()

	// 合法配置正常通过
	gInited = false
	Ini(Config{Prefix: "validprefix", NoLogFile: true, Level: LEVEL_INFO})
	if !gInited {
		t.Fatal("gInited must be true after a valid Ini")
	}
}

// 关闭颜色时四个级别走纯文本打印分支；INFO 级别下 Debug 被门控丢弃。
func TestPlainPrintAndDebugGating(t *testing.T) {
	orig := gConfig
	t.Cleanup(func() { gConfig = orig })

	var buf bytes.Buffer
	SetPrinter(&buf)
	gConfig.NoLogFile = true
	gConfig.NoPrint = false
	gConfig.NoLogColor = true
	gConfig.Level = LEVEL_DEBUG

	Debug("plain-debug")
	Info("plain-info")
	Warn("plain-warn")
	Error("plain-error")
	out := buf.String()
	for _, want := range []string{"[DEBUG]", "[INFO]", "[WARN]", "[ERROR]",
		"plain-debug", "plain-info", "plain-warn", "plain-error"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Errorf("NoLogColor must suppress escape sequences, got %q", out)
	}

	buf.Reset()
	gConfig.Level = LEVEL_INFO
	Debug("gated-debug")
	if buf.Len() != 0 {
		t.Errorf("Debug must be gated at INFO level, got %q", buf.String())
	}
}
