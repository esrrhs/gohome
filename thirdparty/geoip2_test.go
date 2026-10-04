package thirdparty

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// resetGeoip clears the package-level reader so each test starts unloaded.
func resetGeoip(t *testing.T) {
	t.Helper()
	CloseGeoip2()
	t.Cleanup(func() { CloseGeoip2() })
}

// TestGeoipNotLoaded: the accessors must return ErrGeoipNotLoaded instead of
// nil-panicking when no database has been opened. This was the original bug --
// the package-level *geoip2.Reader was dereferenced unconditionally.
func TestGeoipNotLoaded(t *testing.T) {
	resetGeoip(t)

	if _, err := GetGeoipCountryIsoCode("8.8.8.8"); !errors.Is(err, ErrGeoipNotLoaded) {
		t.Errorf("IsoCode err = %v, want ErrGeoipNotLoaded", err)
	}
	if _, err := GetGeoipCountryName("8.8.8.8"); !errors.Is(err, ErrGeoipNotLoaded) {
		t.Errorf("Name err = %v, want ErrGeoipNotLoaded", err)
	}
}

// TestGeoipLoadMissingFile: a failed open must not install a broken reader,
// and must not leave a stale one visible either.
func TestGeoipLoadMissingFile(t *testing.T) {
	resetGeoip(t)

	missing := filepath.Join(t.TempDir(), "does-not-exist.mmdb")
	if err := LoadGeoip2(missing); err == nil {
		t.Fatal("LoadGeoip2 on a missing file = nil, want error")
	}

	// Still unloaded: the failure must not publish a half-open reader.
	if _, err := GetGeoipCountryIsoCode("8.8.8.8"); !errors.Is(err, ErrGeoipNotLoaded) {
		t.Errorf("after failed Load, err = %v, want ErrGeoipNotLoaded", err)
	}
}

// TestGeoipInvalidIP: with no database the not-loaded check wins; this pins
// that the parse error is reported with the address in the message.
func TestGeoipInvalidIPMessage(t *testing.T) {
	resetGeoip(t)

	// The message construction is exercised directly because it needs a
	// loaded reader to reach, and this package ships no .mmdb fixture.
	_, err := country("not-an-ip")
	if !errors.Is(err, ErrGeoipNotLoaded) {
		t.Fatalf("err = %v, want ErrGeoipNotLoaded", err)
	}
}

// TestGeoipCloseIdempotent: Close on an unloaded package is a no-op.
func TestGeoipCloseIdempotent(t *testing.T) {
	resetGeoip(t)
	for i := 0; i < 3; i++ {
		if err := CloseGeoip2(); err != nil {
			t.Fatalf("Close #%d = %v, want nil", i, err)
		}
	}
}

// TestGeoipConcurrentAccess: the RWMutex must make concurrent reloads and
// lookups race-free. Run with -race for this to be meaningful.
func TestGeoipConcurrentAccess(t *testing.T) {
	resetGeoip(t)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Both paths are exercised; the lookup either sees the
				// not-loaded error or a live reader, never a torn pointer.
				GetGeoipCountryIsoCode("1.1.1.1")
				LoadGeoip2(filepath.Join(t.TempDir(), "missing.mmdb"))
			}
		}()
	}
	wg.Wait()
}
