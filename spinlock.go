package gopool

import (
	"runtime"
	"sync/atomic"
)

type SpinLock struct {
	flag uint32
}

func (sl *SpinLock) Lock() {
	backoff := 1
	for !sl.TryLock() {
		for i := 0; i < backoff; i++ {
			runtime.Gosched()
		}
		if backoff < 128 { // Limit the maximum backoff time
			backoff *= 2
		}
	}
}

func (sl *SpinLock) Unlock() {
	atomic.StoreUint32(&sl.flag, 0)
}

func (sl *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapUint32(&sl.flag, 0, 1)
}
