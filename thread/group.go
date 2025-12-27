package thread

import (
	"errors"
	"sync"

	"github.com/esrrhs/gohome/common"
)

/*
Group 实现了一个 Goroutine 管理工具，用于组织和控制 Goroutine 的执行生命周期。
该工具允许将 Goroutine 组织成层级结构，便于管理它们的启动、停止和错误处理。

主要功能包括：

- 创建和管理 Goroutine 的分组（Group），支持父子关系以便实现层级管理。
- 提供用于启动、停止和等待 Goroutine 任务完成的接口。
- 支持错误传播机制，确保子 Goroutine 的错误可以传递影响到父 Goroutine。
- 提供每个分组中 Goroutine 状态和执行情况的记录和日志输出。
- 支持优雅地退出，避免 Goroutine 泄漏和资源争用问题。
- 实现 Goroutine 的动态增减，以及并发安全的 Goroutine 状态访问。
*/

type Group struct {
	father   *Group
	son      sync.Map
	wg       sync.WaitGroup
	errOnce  sync.Once
	err      error
	isexit   bool
	exitfunc func()
	donech   chan int
	name     string
}

func NewGroup(name string, father *Group, exitfunc func()) *Group {
	g := &Group{
		father:   father,
		exitfunc: exitfunc,
	}
	g.donech = make(chan int)
	g.name = name

	if father != nil {
		father.addson(g)
	}

	return g
}

func (g *Group) addson(son *Group) {
	g.son.Store(son, 1)
}

func (g *Group) removeson(son *Group) {
	g.son.Delete(son)
}

func (g *Group) add() {
	g.wg.Add(1)
	if g.father != nil {
		g.father.add()
	}
}

func (g *Group) done() {
	g.wg.Done()
	if g.father != nil {
		g.father.done()
	}
}

func (g *Group) IsExit() bool {
	return g.isexit
}

func (g *Group) Error() error {
	return g.err
}

func (g *Group) exit(err error) {
	g.errOnce.Do(func() {
		g.err = err
		g.isexit = true
		close(g.donech)
		if g.exitfunc != nil {
			g.exitfunc()
		}

		g.son.Range(func(soni, _ interface{}) bool {
			son := soni.(*Group)
			son.exit(err)
			return true
		})
	})
}

func (g *Group) Done() <-chan int {
	return g.donech
}

func (g *Group) Go(name string, f func() error) {
	if g.isexit {
		return
	}
	g.add()

	go func() {
		defer common.CrashLog()
		defer g.done()

		if err := f(); err != nil {
			g.exit(err)
		}
	}()
}

func (g *Group) Stop() {
	g.exit(errors.New("stop"))
}

func (g *Group) Wait() error {
	// 创建一个无缓冲通道，用于接收“任务全部完成”的信号
	c := make(chan struct{})

	// 启动一个临时的轻量级 goroutine 来守候 wg
	go func() {
		g.wg.Wait()
		close(c) // 任务完成后关闭通道
	}()

	select {
	case <-c:
		// 分支1: 所有任务正常完成 (wg 归零)
		// 此时不需要做额外操作，直接往下走
	case <-g.donech:
		// 分支2: 接收到退出信号
		// 这里包含两种情况：
		// 1. 自己调用了 Stop() -> g.donech 被关闭
		// 2. Father 退出了 -> Father 调用 g.exit() -> g.donech 被关闭
		// 此时不需要死等子任务结束，直接返回
	}

	// 清理父子关系
	if g.father != nil {
		g.father.removeson(g)
	}
	return g.err
}
