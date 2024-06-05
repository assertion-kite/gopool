package gopool

import (
	"context"
	"fmt"
)

type worker struct {
	taskQueue chan task
}

func newWorker() *worker {
	return &worker{
		taskQueue: make(chan task, 1),
	}
}

func (w *worker) start(pool *goPool, workerIndex int) {
	go func() {
		for t := range w.taskQueue {
			if t != nil {
				result, err := w.executeTask(t, pool)
				w.handleResult(result, err, pool)
			}
			pool.pushWorker(workerIndex)
		}
	}()
}

func (w *worker) executeTask(t task, pool *goPool) (result interface{}, err error) {
	for i := 0; i <= pool.retryCount; i++ {
		if pool.timeout > 0 {
			result, err = w.executeTaskWithTimeout(t, pool)
		} else {
			result, err = w.executeTaskWithoutTimeout(t, pool)
		}
		if err == nil || i == pool.retryCount {
			return result, err
		}
	}
	return
}

func (w *worker) executeTaskWithTimeout(t task, pool *goPool) (result interface{}, err error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), pool.timeout)
	defer cancel()

	// Create a channel to receive the result of the task
	resultChan := make(chan interface{})
	errChan := make(chan error)

	go func() {
		res, err := t()
		select {
		case resultChan <- res:
		case errChan <- err:
		case <-ctx.Done():
			// The context was cancelled, stop the task
			return
		}
	}()

	select {
	case result = <-resultChan:
		err = <-errChan
		return result, err
	case <-ctx.Done():
		return nil, fmt.Errorf("task timed out")
	}
}

func (w *worker) executeTaskWithoutTimeout(t task, pool *goPool) (result interface{}, err error) {
	// If timeout is not set or is zero, just run the task
	return t()
}

func (w *worker) handleResult(result interface{}, err error, pool *goPool) {
	if err != nil && pool.errorCallback != nil {
		pool.errorCallback(err)
	} else if pool.resultCallback != nil {
		pool.resultCallback(result)
	}
}
