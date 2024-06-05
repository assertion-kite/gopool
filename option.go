package gopool

import (
	"sync"
	"time"
)

type Option func(*goPool)

func WithLock(lock sync.Locker) Option {
	return func(p *goPool) {
		p.lock = lock
		p.cond = sync.NewCond(p.lock)
	}
}

func WithMinWorkers(minWorkers int) Option {
	return func(p *goPool) {
		p.minWorkers = minWorkers
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(p *goPool) {
		p.timeout = timeout
	}
}

func WithResultCallback(callback func(interface{})) Option {
	return func(p *goPool) {
		p.resultCallback = callback
	}
}

func WithErrorCallback(callback func(error)) Option {
	return func(p *goPool) {
		p.errorCallback = callback
	}
}

func WithRetryCount(retryCount int) Option {
	return func(p *goPool) {
		p.retryCount = retryCount
	}
}

func WithTaskQueueSize(size int) Option {
	return func(p *goPool) {
		p.taskQueueSize = size
	}
}
