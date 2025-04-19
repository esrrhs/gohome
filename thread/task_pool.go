package thread

import (
	"fmt"
	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	"runtime"
	"time"
)

/*
TaskPool 实现了一个任务池（Task Pool），用于并发执行 CPU 密集型任务。该任务池允许用户将任务批量添加到池中，并通过固定数量的 Worker 来控制任务的并发执行。

主要功能包括：

- 创建和管理固定数量的 Worker，用于并发执行任务。
- 提供一个任务队列，以便能够快速非阻塞地添加任务。
- 支持将任务按批量方式执行，提高任务执行的效率。
- 在任务执行完成后，通过信道通知调用方任务已完成。
- 提供监控功能以获取当前任务池中的任务数量、完成的任务数量和 Worker 的休眠状态。

该实现适用于需要并发处理大量 CPU 密集型任务的场景。
*/

// Task 表示一个任务，专门用来批量执行CPU密集型任务
type Task struct {
	id   int
	f    func()
	done chan bool
}

// TaskPool 是任务执行池的结构体
type TaskPool struct {
	tasks      chan Task
	numWorkers int
	idSeq      int
	doneNum    int
	sleepNum   int
}

// Init 初始化任务池
func NewTaskPool(numWorkers, taskQueueSize int) *TaskPool {
	tp := &TaskPool{}
	tp.numWorkers = numWorkers
	tp.tasks = make(chan Task, taskQueueSize)

	// 启动 N 个 Worker
	for i := 1; i <= tp.numWorkers; i++ {
		go tp.worker(i)
	}
	return tp
}

// worker 执行任务的 Worker
func (tp *TaskPool) worker(id int) {
	runtime.LockOSThread()

	defer common.CrashLog()

	batchCount := 16
	tasks := make([]Task, 0, batchCount)
	sleepTime := 1 * time.Microsecond

	for {
		tasks = tasks[:0] // 清空任务列表
		// 每次批量从tp.tasks获取任务，批量执行
		// 如果超时不足，则直接执行
		for {
			task := Task{}
			select {
			case task = <-tp.tasks:
				loggo.Debug("Worker %d got task %d", id, task.id)
				tasks = append(tasks, task)
			default:
				break
			}

			if task.f == nil || len(tasks) >= batchCount {
				break
			}
		}

		for _, task := range tasks {
			loggo.Debug("Worker %d executing task %d", id, task.id)
			task.f() // 执行任务
			task.done <- true
			tp.doneNum++
			loggo.Debug("Task %d done\n", task.id)
		}

		if len(tasks) == 0 {
			tp.sleepNum++
			sleepTime = min(2*sleepTime, 1*time.Millisecond)
			time.Sleep(sleepTime) // 如果没有任务，稍微休眠一下
		}
	}
}

// AddTask 添加任务到任务池，并等待任务完成
func (tp *TaskPool) AddTask(f func()) error {
	tp.idSeq++
	task := Task{
		id:   tp.idSeq,
		f:    f,
		done: make(chan bool),
	}
	select {
	case tp.tasks <- task: // 尝试非阻塞发送
		loggo.Debug("Task %d added to the pool", task.id)
		<-task.done // 等待任务完成
		return nil
	default:
		return fmt.Errorf("task queue is full, task %d was not added", task.id)
	}
}

func (tp *TaskPool) TaskNum() int {
	return len(tp.tasks)
}

func (tp *TaskPool) DoneNum() int {
	return tp.doneNum
}

func (tp *TaskPool) ResetDoneNum() {
	tp.doneNum = 0
}

func (tp *TaskPool) SleepNum() int {
	return tp.sleepNum
}

func (tp *TaskPool) ResetSleepNum() {
	tp.sleepNum = 0
}
