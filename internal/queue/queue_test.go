package queue_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"openchat/internal/queue"
)

func startRun(t *testing.T, q *queue.Queue) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx, nil)
	}()
	t.Cleanup(func() {
		cancel()
		q.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("queue worker did not stop")
		}
	})
	return cancel, done
}

func TestFIFOOrderAndSerialization(t *testing.T) {
	q := queue.New(4)
	var mu sync.Mutex
	var seq []string
	var active, maxActive int

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("op%d", i)
		q.Enqueue(queue.Operation{ID: id, Run: func(ctx context.Context) error {
			mu.Lock()
			seq = append(seq, "start:"+id)
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond)

			mu.Lock()
			active--
			seq = append(seq, "end:"+id)
			mu.Unlock()
			return nil
		}})
	}
	_, _ = startRun(t, q)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := len(seq) == 10
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("operations did not finish: %v", seq)
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 5; i++ {
		want := "start:op" + fmt.Sprint(i) + "end:op" + fmt.Sprint(i)
		if got := seq[i*2] + seq[i*2+1]; got != want {
			t.Fatalf("FIFO order broken at %d: got %q, want %q (seq %v)", i, got, want, seq)
		}
	}
	if maxActive != 1 {
		t.Fatalf("operations ran concurrently: maxActive=%d", maxActive)
	}
}

func TestCapacityBound(t *testing.T) {
	q := queue.New(2)
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve 1: %v", err)
	}
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	if err := q.ReserveAsk(); !errors.Is(err, queue.ErrFull) {
		t.Fatalf("reserve 3 must be ErrFull, got %v", err)
	}
	q.ReleaseAsk()
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
	q.ReleaseAsk()
	q.ReleaseAsk()
}

func TestRemovePendingReleasesSlot(t *testing.T) {
	q := queue.New(1)
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	ran := false
	q.Enqueue(queue.Operation{ID: "x", Ask: true, Run: func(ctx context.Context) error {
		ran = true
		return nil
	}})

	if !q.RemovePending("x") {
		t.Fatal("RemovePending must find the queued op")
	}
	if q.RemovePending("x") {
		t.Fatal("second RemovePending must be a no-op")
	}
	// the slot was released by the removal
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve after removal must succeed, got %v", err)
	}
	// and the worker never runs the removed op
	_, _ = startRun(t, q)
	time.Sleep(20 * time.Millisecond)
	if ran {
		t.Fatal("removed operation must not run")
	}
}

func TestWorkerReleasesSlotAfterRun(t *testing.T) {
	q := queue.New(1)
	if err := q.ReserveAsk(); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	q.Enqueue(queue.Operation{ID: "fail", Ask: true, Run: func(ctx context.Context) error {
		return errors.New("boom")
	}})
	if err := q.ReserveAsk(); !errors.Is(err, queue.ErrFull) {
		t.Fatalf("slot must be held while queued, got %v", err)
	}
	_, _ = startRun(t, q)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := q.ReserveAsk(); err == nil {
			break // worker released the slot after finishing
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not release the ask slot")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestWorkerSurvivesPanic(t *testing.T) {
	q := queue.New(2)
	var mu sync.Mutex
	var ran []string
	q.Enqueue(queue.Operation{ID: "panic", Run: func(ctx context.Context) error {
		panic("boom")
	}})
	q.Enqueue(queue.Operation{ID: "after", Run: func(ctx context.Context) error {
		mu.Lock()
		ran = append(ran, "after")
		mu.Unlock()
		return nil
	}})
	_, _ = startRun(t, q)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := len(ran) == 1
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not continue after the panicking op")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
