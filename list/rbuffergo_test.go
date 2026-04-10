package list

import (
	"testing"
)

func TestNew(t *testing.T) {
	rb := NewRBuffergo(10, true)
	if rb.Capacity() != 10 {
		t.Error()
	}
}

func TestRBuffer_Write(t *testing.T) {
	rb := NewRBuffergo(10, true)
	rb.Write([]byte{1, 2, 3})
	if rb.Size() != 3 {
		t.Error(rb.Size())
	}
	var tmp [3]byte
	rb.Read(tmp[0:])
	if tmp[0] != 1 || tmp[1] != 2 || tmp[2] != 3 {
		t.Error(tmp)
	}
	t.Log(rb.GetBuffer())
	rb.Write([]byte{1, 2, 3})
	rb.Write([]byte{1, 2, 3})
	rb.Write([]byte{1, 2, 3})
	t.Log(rb.GetBuffer())
	if rb.Size() != 9 {
		t.Error(rb.Size())
	}
	if rb.Write([]byte{1, 2, 3}) == true {
		t.Error()
	}
	t.Log(rb.GetBuffer())
	rb.Read(tmp[0:])
	rb.Read(tmp[0:])
	rb.Write([]byte{1, 2, 3})
	if rb.Size() != 6 {
		t.Error(rb.Size())
	}
	t.Log(rb.GetBuffer())

	var tmp1 [6]byte
	rb.Read(tmp1[0:])
	t.Log(tmp1)
}

func TestRBuffer_CanWrite(t *testing.T) {
rb := NewRBuffergo(10, true)
if !rb.CanWrite(5) {
t.Error("expected CanWrite(5) true on empty buffer of size 10")
}
if !rb.CanWrite(10) {
t.Error("expected CanWrite(10) true on empty buffer of size 10")
}
if rb.CanWrite(11) {
t.Error("expected CanWrite(11) false on empty buffer of size 10")
}
rb.Write([]byte{1, 2, 3, 4, 5})
if !rb.CanWrite(5) {
t.Error("expected CanWrite(5) true after writing 5 bytes")
}
if rb.CanWrite(6) {
t.Error("expected CanWrite(6) false after writing 5 bytes")
}
}

func TestRBuffer_SkipWrite(t *testing.T) {
rb := NewRBuffergo(10, false)
rb.SkipWrite(3)
if rb.Size() != 3 {
t.Errorf("expected size 3 after SkipWrite(3), got %d", rb.Size())
}
// SkipWrite beyond capacity should be a no-op
rb.SkipWrite(10)
if rb.Size() != 3 {
t.Errorf("expected size still 3 after over-capacity SkipWrite, got %d", rb.Size())
}
}

func TestRBuffer_CanRead(t *testing.T) {
rb := NewRBuffergo(10, true)
if rb.CanRead(1) {
t.Error("expected CanRead(1) false on empty buffer")
}
rb.Write([]byte{1, 2, 3})
if !rb.CanRead(3) {
t.Error("expected CanRead(3) true after writing 3 bytes")
}
if rb.CanRead(4) {
t.Error("expected CanRead(4) false after writing only 3 bytes")
}
}

func TestRBuffer_SkipRead(t *testing.T) {
rb := NewRBuffergo(10, false)
rb.Write([]byte{1, 2, 3, 4, 5})
rb.SkipRead(3)
if rb.Size() != 2 {
t.Errorf("expected size 2 after SkipRead(3), got %d", rb.Size())
}
// SkipRead beyond available should be a no-op
rb.SkipRead(10)
if rb.Size() != 2 {
t.Errorf("expected size still 2 after over-size SkipRead, got %d", rb.Size())
}
}

func TestRBuffer_StoreRestore(t *testing.T) {
rb := NewRBuffergo(10, true)
rb.Write([]byte{1, 2, 3})
rb.Store()
rb.Write([]byte{4, 5})
if rb.Size() != 5 {
t.Errorf("expected size 5 before restore, got %d", rb.Size())
}
rb.Restore()
if rb.Size() != 3 {
t.Errorf("expected size 3 after restore, got %d", rb.Size())
}
var tmp [3]byte
rb.Read(tmp[:])
if tmp[0] != 1 || tmp[1] != 2 || tmp[2] != 3 {
t.Errorf("unexpected data after restore: %v", tmp)
}
}

func TestRBuffer_Clear(t *testing.T) {
rb := NewRBuffergo(10, true)
rb.Write([]byte{1, 2, 3})
rb.Clear()
if rb.Size() != 0 {
t.Errorf("expected size 0 after Clear, got %d", rb.Size())
}
if !rb.Empty() {
t.Error("expected Empty() true after Clear")
}
}

func TestRBuffer_EmptyFull(t *testing.T) {
rb := NewRBuffergo(3, true)
if !rb.Empty() {
t.Error("expected Empty() true on new buffer")
}
if rb.Full() {
t.Error("expected Full() false on new buffer")
}
rb.Write([]byte{1, 2, 3})
if rb.Empty() {
t.Error("expected Empty() false after filling buffer")
}
if !rb.Full() {
t.Error("expected Full() true after filling buffer")
}
}

func TestRBuffer_GetLineBuffers(t *testing.T) {
rb := NewRBuffergo(10, true)
rb.Write([]byte{1, 2, 3})
readBuf := rb.GetReadLineBuffer()
if len(readBuf) != 3 {
t.Errorf("expected GetReadLineBuffer len 3, got %d", len(readBuf))
}
writeBuf := rb.GetWriteLineBuffer()
if len(writeBuf) == 0 {
t.Error("expected non-empty GetWriteLineBuffer")
}
}
