package sync

import (
	"sync/atomic"

	"github.com/gammazero/workerpool"
	"github.com/panjf2000/ants/v2"
	conc "github.com/sourcegraph/conc/pool"

	"github.com/Tangerg/pkg/safe"
)

// Pool runs functions concurrently. Implementations may impose limits
// on parallelism, queueing, or per-task timeouts.
type Pool interface {
	Submit(f func()) error
}

// defaultPool holds the package-level default Pool. The holder struct
// keeps the atomic.Value's concrete type stable so any [Pool]
// implementation can be stored.
var defaultPool atomic.Value

func init() {
	defaultPool.Store(defaultPoolHolder{pool: PoolOfNoPool()})
}

// defaultPoolHolder wraps a [Pool] so the atomic.Value always stores the
// same concrete type regardless of the underlying implementation.
type defaultPoolHolder struct {
	pool Pool
}

// DefaultPool returns the current default Pool. Until [SetDefaultPool]
// is called, this is [PoolOfNoPool]. It never returns nil.
func DefaultPool() Pool {
	return defaultPool.Load().(defaultPoolHolder).pool
}

// SetDefaultPool replaces the default Pool. A nil pool is ignored.
func SetDefaultPool(p Pool) {
	if p == nil {
		return
	}
	defaultPool.Store(defaultPoolHolder{pool: p})
}

// poolAdapter adapts a func(func()) error into the [Pool] interface.
type poolAdapter func(f func()) error

// Submit implements [Pool].
func (p poolAdapter) Submit(f func()) error { return p(f) }

// PoolOfNoPool returns a Pool that launches a fresh recoverable
// goroutine for every task via [safe.Go]; a task panic is recovered and
// discarded. It applies no concurrency limit and Submit never blocks.
func PoolOfNoPool() Pool {
	return poolAdapter(func(f func()) error {
		safe.Go(f)
		return nil
	})
}

// PoolOfConc adapts a sourcegraph/conc *Pool. Panics if pool is nil.
// Submit blocks once all goroutines are busy. A task panic is not
// recovered and crashes the process.
func PoolOfConc(pool *conc.Pool) Pool {
	if pool == nil {
		panic("sync: pool must not be nil")
	}
	return poolAdapter(func(f func()) error {
		pool.Go(f)
		return nil
	})
}

// PoolOfAnts adapts a panjf2000/ants *Pool. Panics if pool is nil.
// In its default mode Submit blocks once the pool is exhausted; ants
// recovers and logs task panics. Submit returns an error when the pool
// is full in non-blocking mode or already released.
func PoolOfAnts(pool *ants.Pool) Pool {
	if pool == nil {
		panic("sync: pool must not be nil")
	}
	return poolAdapter(func(f func()) error {
		return pool.Submit(f)
	})
}

// PoolOfWorkerpool adapts a gammazero/workerpool *WorkerPool. Panics
// if pool is nil. Submit never blocks: workerpool queues tasks without
// bound. Once the pool is stopped, Submit returns [workerpool.ErrStopped]
// instead of panicking. A task panic is not recovered and crashes the
// process.
func PoolOfWorkerpool(pool *workerpool.WorkerPool) Pool {
	if pool == nil {
		panic("sync: pool must not be nil")
	}
	return poolAdapter(func(f func()) (err error) {
		if pool.Stopped() {
			return workerpool.ErrStopped
		}
		defer func() {
			if r := recover(); r != nil {
				perr, ok := r.(error)
				if !ok {
					panic(r)
				}
				err = perr
			}
		}()
		pool.Submit(f)
		return nil
	})
}
