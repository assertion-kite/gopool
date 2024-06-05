package gopool

import (
	"context"
	"sort"
	"sync"
	"time"
)

type GoPool interface {
	// AddTask 创建
	AddTask(t task)
	// Wait 等待
	Wait()
	// Release 方法释放池和所有工作者
	Release()
	// Running 正在运行的工作者数量
	Running() int
	// GetWorkerCount 数量
	GetWorkerCount() int
	// GetTaskQueueSize 任务队列的大小
	GetTaskQueueSize() int
}

type task func() (interface{}, error)

// workers []*worker: 一个指向 worker 结构体的切片，表示池中的工作线程。
// workerStack []int: 一个整数切片，用于保存空闲的工作线程的索引，以便在需要时重新利用它们。
// maxWorkers int: 池中允许的最大工作线程数。
// minWorkers int: 池中要保持的最小工作线程数。
// taskQueue chan task: 一个 task 类型的通道，用于接收要执行的任务。
// taskQueueSize int: 任务队列的大小，即能够同时存储的任务数量。
// retryCount int: 任务执行失败时的重试次数。
// lock sync.Locker: 一个互斥锁接口，用于对池中的数据进行同步操作。
// cond *sync.Cond: 一个条件变量，用于在任务队列中有新任务到达时通知工作线程。
// timeout time.Duration: 任务超时时间，即每个任务的最长执行时间。
// resultCallback func(interface{}): 当任务执行完成时的回调函数，接收任务执行结果作为参数。
// errorCallback func(error): 当任务执行出错时的回调函数，接收错误信息作为参数。
// adjustInterval time.Duration: 调整工作线程数量的间隔时间。
// ctx context.Context: 上下文对象，用于控制池的生命周期。
// cancel context.CancelFunc: 用于取消池的执行。
type goPool struct {
	workers        []*worker
	workerStack    []int
	maxWorkers     int
	minWorkers     int
	taskQueue      chan task
	taskQueueSize  int
	retryCount     int
	lock           sync.Locker
	cond           *sync.Cond
	timeout        time.Duration
	resultCallback func(interface{})
	errorCallback  func(error)
	adjustInterval time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewGoPool(maxWorkers int, opts ...Option) GoPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &goPool{
		maxWorkers:     maxWorkers,
		minWorkers:     maxWorkers,
		workers:        nil,
		workerStack:    nil,
		taskQueue:      nil,
		taskQueueSize:  1e6,
		retryCount:     0,
		lock:           new(sync.Mutex),
		timeout:        0,
		adjustInterval: 1 * time.Second,
		ctx:            ctx,
		cancel:         cancel,
	}
	// Apply options
	for _, opt := range opts {
		opt(pool)
	}

	pool.taskQueue = make(chan task, pool.taskQueueSize)
	pool.workers = make([]*worker, pool.minWorkers)
	pool.workerStack = make([]int, pool.minWorkers)

	if pool.cond == nil {
		pool.cond = sync.NewCond(pool.lock)
	}
	for i := 0; i < pool.minWorkers; i++ {
		worker := newWorker()
		pool.workers[i] = worker
		pool.workerStack[i] = i
		worker.start(pool, i)
	}
	go pool.adjustWorkers()
	go pool.dispatch()
	return pool
}

func (p *goPool) AddTask(t task) {
	p.taskQueue <- t
}

func (p *goPool) Wait() {
	for {
		p.lock.Lock()
		workerStackLen := len(p.workerStack)
		p.lock.Unlock()

		if len(p.taskQueue) == 0 && workerStackLen == len(p.workers) {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func (p *goPool) Release() {
	close(p.taskQueue)
	p.cancel()
	p.cond.L.Lock()
	for len(p.workerStack) != p.minWorkers {
		p.cond.Wait()
	}
	p.cond.L.Unlock()
	for _, worker := range p.workers {
		close(worker.taskQueue)
	}
	p.workers = nil
	p.workerStack = nil
}

func (p *goPool) popWorker() int {
	p.lock.Lock()
	workerIndex := p.workerStack[len(p.workerStack)-1]
	p.workerStack = p.workerStack[:len(p.workerStack)-1]
	p.lock.Unlock()
	return workerIndex
}

func (p *goPool) pushWorker(workerIndex int) {
	p.lock.Lock()
	p.workerStack = append(p.workerStack, workerIndex)
	p.lock.Unlock()
	p.cond.Signal()
}

func (p *goPool) adjustWorkers() {
	ticker := time.NewTicker(p.adjustInterval)
	defer ticker.Stop()

	var adjustFlag bool

	for {
		adjustFlag = false
		select {
		case <-ticker.C:
			p.cond.L.Lock()
			if len(p.taskQueue) > len(p.workers)*3/4 && len(p.workers) < p.maxWorkers {
				adjustFlag = true
				newWorkers := min(len(p.workers)*2, p.maxWorkers) - len(p.workers)
				for i := 0; i < newWorkers; i++ {
					worker := newWorker()
					p.workers = append(p.workers, worker)
					p.workerStack = append(p.workerStack, len(p.workers)-1)
					worker.start(p, len(p.workers)-1)
				}
			} else if len(p.taskQueue) == 0 && len(p.workerStack) == len(p.workers) && len(p.workers) > p.minWorkers {
				adjustFlag = true
				removeWorkers := (len(p.workers) - p.minWorkers + 1) / 2
				// [1,2,3,4,5] -working-> [1,2,3] -expansive-> [1,2,3,6,7] -idle-> [1,2,3,6,7,4,5]
				sort.Ints(p.workerStack)
				p.workers = p.workers[:len(p.workers)-removeWorkers]
				p.workerStack = p.workerStack[:len(p.workerStack)-removeWorkers]
			}
			p.cond.L.Unlock()
			if adjustFlag {
				p.cond.Broadcast()
			}
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *goPool) dispatch() {
	for t := range p.taskQueue {
		p.cond.L.Lock()
		for len(p.workerStack) == 0 {
			p.cond.Wait()
		}
		p.cond.L.Unlock()
		workerIndex := p.popWorker()
		p.workers[workerIndex].taskQueue <- t
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *goPool) Running() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return len(p.workers) - len(p.workerStack)
}

func (p *goPool) GetWorkerCount() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return len(p.workers)
}

func (p *goPool) GetTaskQueueSize() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.taskQueueSize
}
