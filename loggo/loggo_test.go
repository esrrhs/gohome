package loggo

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func Test0001(t *testing.T) {
	Ini(Config{
		Level:  LEVEL_INFO,
		Prefix: "test",
		MaxDay: 3,
	})

	for i := 0; i < 10; i++ {
		Info("test %d", i)
		time.Sleep(time.Second)
	}

}

func TestNameToLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"debug uppercase", "DEBUG", LEVEL_DEBUG},
		{"info uppercase", "INFO", LEVEL_INFO},
		{"warn uppercase", "WARN", LEVEL_WARN},
		{"error uppercase", "ERROR", LEVEL_ERROR},
		{"debug lowercase", "debug", LEVEL_DEBUG},
		{"info lowercase", "info", LEVEL_INFO},
		{"warn lowercase", "warn", LEVEL_WARN},
		{"error lowercase", "error", LEVEL_ERROR},
		{"debug mixed case", "Debug", LEVEL_DEBUG},
		{"info mixed case", "iNfO", LEVEL_INFO},
		{"unknown level", "TRACE", -1},
		{"empty string", "", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NameToLevel(tt.input)
			if got != tt.want {
				t.Errorf("NameToLevel(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsLevel(t *testing.T) {
	origLevel := gConfig.Level
	defer func() { gConfig.Level = origLevel }()

	tests := []struct {
		name    string
		level   int
		isDebug bool
		isInfo  bool
		isWarn  bool
		isError bool
	}{
		{"debug level", LEVEL_DEBUG, true, true, true, true},
		{"info level", LEVEL_INFO, false, true, true, true},
		{"warn level", LEVEL_WARN, false, false, true, true},
		{"error level", LEVEL_ERROR, false, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gConfig.Level = tt.level
			if got := IsDebug(); got != tt.isDebug {
				t.Errorf("IsDebug() = %v, want %v (level=%d)", got, tt.isDebug, tt.level)
			}
			if got := IsInfo(); got != tt.isInfo {
				t.Errorf("IsInfo() = %v, want %v (level=%d)", got, tt.isInfo, tt.level)
			}
			if got := IsWarn(); got != tt.isWarn {
				t.Errorf("IsWarn() = %v, want %v (level=%d)", got, tt.isWarn, tt.level)
			}
			if got := IsError(); got != tt.isError {
				t.Errorf("IsError() = %v, want %v (level=%d)", got, tt.isError, tt.level)
			}
		})
	}
}

func TestSetPrinter(t *testing.T) {
	origPrinter := gConfig.printer
	origNoPrint := gConfig.NoPrint
	defer func() {
		gConfig.printer = origPrinter
		gConfig.NoPrint = origNoPrint
	}()

	var buf bytes.Buffer
	SetPrinter(&buf)
	gConfig.NoPrint = false

	origLevel := gConfig.Level
	gConfig.Level = LEVEL_DEBUG
	defer func() { gConfig.Level = origLevel }()

	Debug("printer test message %d", 42)

	output := buf.String()
	t.Logf("SetPrinter output: %q", output)
	if !strings.Contains(output, "printer test message 42") {
		t.Errorf("SetPrinter output %q does not contain expected message", output)
	}
}
