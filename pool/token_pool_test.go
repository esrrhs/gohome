package pool

import (
	"sync"
	"testing"
)

func TestTokenPool(t *testing.T) {
	p := NewTokenPool(10)
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			index := p.Acquire()
			t.Logf("Acquire %d", index)
			p.Release(index)
			t.Logf("Release %d", index)
		}()
	}

	wg.Wait()
}
