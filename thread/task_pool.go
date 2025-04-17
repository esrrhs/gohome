package thread

import (
	"fmt"
	"github.com/esrrhs/gohome/common"
	"runtime"
	"time"
)

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
				//logInfof("Worker %d got task %d", id, task.id)
				tasks = append(tasks, task)
			default:
				break
			}

			if task.f == nil || len(tasks) >= batchCount {
				break
			}
		}

		for _, task := range tasks {
			//logInfof("Worker %d executing task %d", id, task.id)
			task.f() // 执行任务
			task.done <- true
			tp.doneNum++
			//logInfof("Task %d done\n", task.id)
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
		//logInfof("Task %d added to the pool", task.id)
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
