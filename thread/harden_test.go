package thread

import (
	"errors"
	"sync/atomic"
	"testing"
)

var errHardenSentinel = errors.New("harden sentinel")

// TestHardenGroupJoinSuccess Join 等待所有任务正常结束并返回 nil。
func TestHardenGroupJoinSuccess(t *testing.T) {
	g := NewGroup("join-ok", nil, nil)
	var ran atomic.Bool
	g.Go("t", func() error {
		ran.Store(true)
		return nil
	})
	if err := g.Join(); err != nil {
		t.Fatalf("Join after clean completion = %v, want nil", err)
	}
	if !ran.Load() {
		t.Fatal("task must have run")
	}
	if g.IsExit() {
		t.Fatal("clean completion must not mark group as exited")
	}
	// 重复 Join 安全：wg 已归零，无父子关系可清理
	if err := g.Join(); err != nil {
		t.Fatalf("second Join = %v, want nil", err)
	}
}

// TestHardenGroupJoinError 任务报错时 Join 也要等 goroutine 全部返回并透出错误。
func TestHardenGroupJoinError(t *testing.T) {
	g := NewGroup("join-err", nil, nil)
	g.Go("boom", func() error {
		return errHardenSentinel
	})
	if err := g.Join(); !errors.Is(err, errHardenSentinel) {
		t.Fatalf("Join = %v, want sentinel", err)
	}
	if !g.IsExit() || g.Error() == nil {
		t.Fatal("error must mark exit and persist via Error()")
	}
	// exit 后 Done 通道必须已关闭
	select {
	case <-g.Done():
	default:
		t.Fatal("Done channel must be closed after error")
	}
}

// TestHardenGroupGoAfterExit exit 之后再 Go：任务必须被直接丢弃，不能加 wg。
func TestHardenGroupGoAfterExit(t *testing.T) {
	var calls atomic.Int32
	g := NewGroup("late", nil, nil)
	g.Stop()

	g.Go("late-task", func() error {
		calls.Add(1)
		return nil
	})
	if err := g.Wait(); err == nil {
		t.Fatal("Wait after Stop must surface the stop error")
	}
	if calls.Load() != 0 {
		t.Fatalf("task scheduled after exit must not run, calls=%d", calls.Load())
	}
}

// TestHardenGroupExitOnce exitfunc 与状态翻转只能发生一次：重复 Stop 幂等。
func TestHardenGroupExitOnce(t *testing.T) {
	var exits atomic.Int32
	g := NewGroup("once", nil, func() {
		exits.Add(1)
	})
	g.Stop()
	g.Stop()
	g.Stop()
	if exits.Load() != 1 {
		t.Fatalf("exitfunc fired %d times, want 1", exits.Load())
	}
	_ = g.Wait()
}

// TestHardenGroupFatherStopPropagates 父组 Stop 必须级联退出子组，
// 子组 Wait 返回 stop 错误并从父组注销。
func TestHardenGroupFatherStopPropagates(t *testing.T) {
	father := NewGroup("father", nil, nil)
	son := NewGroup("son", father, nil)
	if _, ok := father.son.Load(son); !ok {
		t.Fatal("son must be registered with father")
	}

	ready := make(chan struct{})
	son.Go("w", func() error {
		close(ready)
		<-son.Done()
		return nil
	})
	<-ready

	father.Stop()
	if err := son.Wait(); err == nil {
		t.Fatal("son Wait must return the propagated stop error")
	}
	if _, ok := father.son.Load(son); ok {
		t.Fatal("son must unregister from father after Wait")
	}
	// father 本身无任务，Join 立即收敛，返回退出错误
	if err := father.Join(); err == nil {
		t.Fatal("father Join must retain the stop error")
	}
}

// TestHardenGroupJoinUnregistersSon 子组干净 Join 结束后也要从父组注销。
func TestHardenGroupJoinUnregistersSon(t *testing.T) {
	father := NewGroup("f2", nil, nil)
	son := NewGroup("s2", father, nil)
	son.Go("t", func() error {
		return nil
	})
	if err := son.Join(); err != nil {
		t.Fatalf("son Join = %v, want nil", err)
	}
	if _, ok := father.son.Load(son); ok {
		t.Fatal("son must unregister from father after Join")
	}
}

// TestHardenTaskPoolQueueFull 零 worker 池：无接收者，AddTask 非阻塞发送
// 必须立刻返回队列满错误，不能挂死。
func TestHardenTaskPoolQueueFull(t *testing.T) {
	tp := NewTaskPool(0, 0)
	var ran atomic.Bool
	err := tp.AddTask(func() {
		ran.Store(true)
	})
	if err == nil {
		t.Fatal("AddTask with no worker must reject, got nil")
	}
	if ran.Load() {
		t.Fatal("rejected task must not execute")
	}
	// 计数接口在空池上保持零值可用
	if tp.DoneNum() != 0 || tp.SleepNum() != 0 || tp.TaskNum() != 0 {
		t.Fatal("idle zero-worker pool stats must be zero")
	}
}
