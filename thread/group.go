package thread

import (
	"errors"
	"fmt"
	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	"sync"
	"sync/atomic"
	"time"
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
	son      map[*Group]int
	wg       int32
	errOnce  sync.Once
	err      error
	isexit   bool
	exitfunc func()
	donech   chan int
	sonname  map[string]int
	lock     sync.Mutex
	name     string
}

func NewGroup(name string, father *Group, exitfunc func()) *Group {
	g := &Group{
		father:   father,
		exitfunc: exitfunc,
	}
	g.lock.Lock()
	defer g.lock.Unlock()
	g.donech = make(chan int)
	g.sonname = make(map[string]int)
	g.son = make(map[*Group]int)
	g.name = name

	if father != nil {
		father.addson(g)
	}

	return g
}

func (g *Group) addson(son *Group) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.son[son]++
}

func (g *Group) removeson(son *Group) {
	g.lock.Lock()
	defer g.lock.Unlock()
	if g.son[son] == 0 {
		//loggo.Debug("removeson fail no son %s %s", g.name, son.name)
	}
	delete(g.son, son)
}

func (g *Group) add() {
	atomic.AddInt32(&g.wg, 1)
	if g.father != nil {
		g.father.add()
	}
}

func (g *Group) done() {
	atomic.AddInt32(&g.wg, -1)
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

		for son, _ := range g.son {
			son.exit(err)
		}
	})
}

func (g *Group) runningmap() string {
	g.lock.Lock()
	defer g.lock.Unlock()
	ret := ""
	tmp := make(map[string]int)
	for k, v := range g.sonname {
		if v > 0 {
			tmp[k] = v
		}
	}
	ret += fmt.Sprintf("%v", tmp) + "\n"
	for son, _ := range g.son {
		ret += son.runningmap()
	}
	return ret
}

func (g *Group) Done() <-chan int {
	return g.donech
}

func (g *Group) Go(name string, f func() error) {
	g.lock.Lock()
	defer g.lock.Unlock()
	if g.isexit {
		return
	}
	g.add()
	g.sonname[name]++

	go func() {
		defer common.CrashLog()
		defer g.done()

		if err := f(); err != nil {
			g.exit(err)
		}

		g.lock.Lock()
		defer g.lock.Unlock()
		g.sonname[name]--
	}()
}

func (g *Group) Stop() {
	g.exit(errors.New("stop"))
}

func (g *Group) Wait() error {
	last := int64(0)
	begin := int64(0)
	for g.wg != 0 {
		if g.isexit {
			cur := time.Now().Unix()
			if last == 0 {
				last = cur
				begin = cur
			} else {
				if cur-last > 30 {
					last = cur
					loggo.Error("Group Wait too long %s %d %s %v", g.name, g.wg,
						time.Duration((cur-begin)*int64(time.Second)).String(), g.runningmap())
				}
			}
		} else if g.father != nil {
			if g.father.IsExit() {
				g.exit(errors.New("father exit"))
			}
		}
		time.Sleep(time.Millisecond * 100)
	}
	if g.father != nil {
		g.father.removeson(g)
	}
	return g.err
}
