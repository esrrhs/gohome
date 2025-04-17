package list

import (
	"github.com/esrrhs/gohome/loggo"
	"sync"
)

type Req[V any] struct {
	value V
	c     chan bool
}

// TaskQueue 管理请求队列和正在执行的任务
type ReqQueue[K comparable, V any] struct {
	tasks       sync.Map
	requestFunc func(K) (V, error)
	newNum      int
	reuseNum    int
}

// NewTaskQueue 创建一个新的任务队列
func NewReqQueue[K comparable, V any](requestFunc func(K) (V, error)) *ReqQueue[K, V] {
	return &ReqQueue[K, V]{
		requestFunc: requestFunc,
	}
}

// Submit 提交任务
func (q *ReqQueue[K, V]) Submit(key K) (V, error) {
	newReq := &Req[V]{
		c: make(chan bool),
	}
	// 检查是否已有同名任务在进行
	actual, loaded := q.tasks.LoadOrStore(key, newReq)
	req := actual.(*Req[V])
	if loaded {
		// 如果已有任务在进行，等待它完成
		q.reuseNum++
		loggo.Debug("Task %v is already in progress, waiting...", key)
		<-req.c
		loggo.Debug("Task %v completed, returning result", key)
		return req.value, nil
	} else {
		// 如果没有任务在进行，开始新的任务
		q.newNum++
		loggo.Debug("Starting new task for %v", key)
		result, err := q.requestFunc(key)
		if err != nil {
			loggo.Debug("Error processing task %v: %v", key, err)
			return req.value, err
		}
		req.value = result
		q.tasks.Delete(key)
		close(req.c) // 通知等待的任务完成
		loggo.Debug("Task %v completed successfully", key)
		return result, nil
	}
}

// GetNewNum 获取新任务数量
func (q *ReqQueue[K, V]) GetNewNum() int {
	return q.newNum
}

// GetReuseNum 获取重用任务数量
func (q *ReqQueue[K, V]) GetReuseNum() int {
	return q.reuseNum
}

// ResetNewNum 重置新任务数量
func (q *ReqQueue[K, V]) ResetNewNum() {
	q.newNum = 0
}

// ResetReuseNum 重置重用任务数量
func (q *ReqQueue[K, V]) ResetReuseNum() {
	q.reuseNum = 0
}
