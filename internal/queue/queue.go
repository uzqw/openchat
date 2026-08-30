// Package queue implements the single in-process FIFO operation queue for
// Gemini. All OpenCLI operations (ask, login, provider refresh, doctor,
// ...) are serialized through one worker; ask operations additionally hold
// a bounded capacity slot so a full queue answers 429 before any database
// row exists. There is no cross-provider parallelism or distributed lock —
// v1 runs one backend instance.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrFull is returned when the ask capacity is exhausted (HTTP 429).
var ErrFull = errors.New("Gemini queue is full")

// Operation is one serialized Gemini operation. Ask operations reserve a
// capacity slot when enqueued; all operations share the FIFO position.
type Operation struct {
	ID  string
	Ask bool
	Run func(ctx context.Context) error
}

// Queue is a bounded FIFO with a single worker.
type Queue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	items    []Operation
	askSlots int
	capacity int
	closed   bool
	busy     bool // worker is currently executing an operation
}

// New creates an empty queue with the given ask capacity (<=0 disables the
// bound, used by tests).
func New(capacity int) *Queue {
	q := &Queue{capacity: capacity}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// ReserveAsk acquires one ask slot without enqueueing; the slot is held
// while the turn+task transaction runs, so a full queue never leaves
// orphaned database rows.
func (q *Queue) ReserveAsk() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return errors.New("queue closed")
	}
	if q.capacity > 0 && q.askSlots >= q.capacity {
		return ErrFull
	}
	q.askSlots++
	return nil
}

// ReleaseAsk returns one ask slot (transaction failure or task terminal).
func (q *Queue) ReleaseAsk() {
	q.mu.Lock()
	q.askSlots--
	q.mu.Unlock()
}

// Enqueue appends an operation in FIFO order and wakes the worker. The
// operation's ask slot, if any, was already reserved by the caller.
func (q *Queue) Enqueue(op Operation) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, op)
		q.cond.Signal()
	}
	q.mu.Unlock()
}

// RemovePending drops a queued operation by ID and reports whether it was
// still queued. The caller must have already terminalized the operation's
// task; the ask slot is released here (otherwise the worker releases it).
func (q *Queue) RemovePending(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range q.items {
		if it.ID == id {
			q.items = append(q.items[:i], q.items[i+1:]...)
			if it.Ask {
				q.askSlots--
			}
			return true
		}
	}
	return false
}

// Close wakes the worker and prevents further enqueues.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// Idle reports whether the worker is not executing anything and nothing
// is queued (used by the provider cache refresher before enqueueing).
func (q *Queue) Idle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) == 0 && !q.busy
}

// Run executes operations serially in FIFO order until ctx is canceled or
// Close is called. A panicking operation is contained and reported via
// onErr so the worker survives. The ask slot is always released when the
// operation finishes.
func (q *Queue) Run(ctx context.Context, onErr func(op Operation, err error)) {
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed && ctx.Err() == nil {
			q.cond.Wait()
		}
		if len(q.items) == 0 {
			q.mu.Unlock()
			return
		}
		op := q.items[0]
		q.items = q.items[1:]
		q.busy = true
		q.mu.Unlock()

		if op.Run != nil {
			func() {
				defer func() {
					if r := recover(); r != nil && onErr != nil {
						onErr(op, fmt.Errorf("operation panic: %v", r))
					}
				}()
				if err := op.Run(ctx); err != nil && onErr != nil {
					onErr(op, err)
				}
			}()
		}
		if op.Ask {
			q.ReleaseAsk()
		}
		q.mu.Lock()
		q.busy = false
		q.mu.Unlock()
	}
}
