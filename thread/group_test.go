package thread

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func Test0001(t *testing.T) {
	g := NewGroup("", nil, nil)
	g.Go("", func() error {
		fmt.Println("a")
		return nil
	})
	g.Wait()
}

func Test0002(t *testing.T) {
	g := NewGroup("", nil, nil)
	g.Go("", func() error {
		for !g.IsExit() {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick")
			}
		}
		return nil
	})
	g.Go("", func() error {
		time.Sleep(time.Second * 5)
		return errors.New("done")
	})
	fmt.Println(g.Wait())
}

func Test0003(t *testing.T) {
	g := NewGroup("", nil, nil)
	gg := NewGroup("", g, nil)
	gg.Go("", func() error {
		for {
			select {
			case <-gg.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick")
			}
		}
	})
	g.Go("", func() error {
		time.Sleep(time.Second * 5)
		return errors.New("done")
	})
	fmt.Println(g.Wait())
}

func Test0004(t *testing.T) {
	g := NewGroup("", nil, nil)
	gg := NewGroup("", g, nil)
	g.Go("", func() error {
		for {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick father")
			}
		}
	})
	g.Go("", func() error {
		time.Sleep(time.Second * 10)
		return errors.New("done father")
	})
	gg.Go("", func() error {
		for {
			select {
			case <-gg.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick")
			}
		}
	})
	gg.Go("", func() error {
		time.Sleep(time.Second * 5)
		return errors.New("done")
	})
	fmt.Println(gg.Wait())
	fmt.Println(g.Wait())
}

func Test0005(t *testing.T) {
	g := NewGroup("", nil, func() {
		fmt.Println("stop")
	})

	g.Go("", func() error {
		for {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick 1")
			}
		}
	})

	g.Go("", func() error {
		for {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick 2")
			}
		}
	})

	time.Sleep(time.Second * 5)
	g.Stop()
	g.Wait()

}

func Test0006(t *testing.T) {
	// done 会被 exit 回调写、被两个 worker 读，必须用原子变量
	var done atomic.Bool
	g := NewGroup("", nil, func() {
		done.Store(true)
		fmt.Println("stop")
	})

	g.Go("", func() error {
		for !done.Load() {
			fmt.Println("tick 1")
			time.Sleep(time.Second)
		}
		return nil
	})

	g.Go("", func() error {
		for !done.Load() {
			fmt.Println("tick 2")
			time.Sleep(time.Second)
		}
		return nil
	})

	time.Sleep(time.Second * 5)
	g.Stop()
	g.Wait()

}

func Test0007(t *testing.T) {
	g := NewGroup("", nil, func() {
		fmt.Println("stop")
	})

	g.Go("", func() error {
		time.Sleep(time.Second)
		fmt.Println("tick 1")
		return nil
	})

	g.Go("", func() error {
		time.Sleep(time.Second)
		time.Sleep(time.Second)
		fmt.Println("tick 2")
		return nil
	})

	g.Go("", func() error {
		for {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick 3")
			}
		}
	})

	time.Sleep(time.Second * 5)
	g.Stop()
	g.Wait()
}

func Test008(t *testing.T) {
	g := NewGroup("", nil, func() {
		fmt.Println("stop")
	})

	// exit 由另一个 goroutine 写、被 worker 读，必须用原子变量
	var exit atomic.Bool
	g.Go("test", func() error {
		for !exit.Load() {
			fmt.Println("tick")
			time.Sleep(time.Second)
		}
		return nil
	})

	go func() {
		time.Sleep(time.Second * 5)
		g.Stop()
	}()

	go func() {
		time.Sleep(time.Second * 7)
		exit.Store(true)
	}()

	g.Wait()

}

func Test0009(t *testing.T) {
	g := NewGroup("", nil, nil)
	gg1 := NewGroup("gg1", g, nil)
	gg2 := NewGroup("gg2", g, nil)
	g.Go("", func() error {
		for {
			select {
			case <-g.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick father")
			}
		}
	})
	g.Go("", func() error {
		time.Sleep(time.Second * 30)
		return errors.New("done father")
	})
	gg1.Go("", func() error {
		for {
			select {
			case <-gg1.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick1")
			}
		}
	})
	gg1.Go("", func() error {
		time.Sleep(time.Second * 5)
		return errors.New("done1")
	})
	gg2.Go("", func() error {
		for {
			select {
			case <-gg2.Done():
				return nil
			case <-time.After(time.Second):
				fmt.Println("tick2")
			}
		}
	})
	gg2.Go("", func() error {
		time.Sleep(time.Second * 20)
		return errors.New("done2")
	})
	fmt.Println(gg1.Wait())
	fmt.Println(gg2.Wait())
	fmt.Println(g.Wait())
}

func TestGroupError(t *testing.T) {
	g := NewGroup("", nil, nil)
	// Initially no error
	if g.Error() != nil {
		t.Errorf("expected nil error on new group, got %v", g.Error())
	}
	// After stop, error should be set
	g.Stop()
	g.Wait()
	if g.Error() == nil {
		t.Error("expected non-nil error after Stop")
	}
	fmt.Println("Group error after Stop:", g.Error())
}
