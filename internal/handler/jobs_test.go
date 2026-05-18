package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Regression for M-13: runJob must register the goroutine with the
// WaitGroup so StopBackgroundJobs blocks until the job exits.
func TestRunJobAwaitsCompletion(t *testing.T) {
	h := &Handler{jobs: newJobs()}

	var done atomic.Bool
	h.runJob(func(ctx context.Context) {
		// Simulate a slow job that doesn't watch ctx — Stop should still
		// wait for it to return on its own.
		time.Sleep(50 * time.Millisecond)
		done.Store(true)
	})

	h.StopBackgroundJobs()
	if !done.Load() {
		t.Error("StopBackgroundJobs returned before the job completed")
	}
}

// Regression for M-13: jobs that DO watch the shared context exit
// promptly once StopBackgroundJobs cancels it.
func TestRunJobCancelsContextOnStop(t *testing.T) {
	h := &Handler{jobs: newJobs()}

	gotCancel := make(chan struct{})
	h.runJob(func(ctx context.Context) {
		<-ctx.Done()
		close(gotCancel)
	})

	// Give the goroutine a moment to enter the select.
	time.Sleep(10 * time.Millisecond)

	go h.StopBackgroundJobs()

	select {
	case <-gotCancel:
		// good
	case <-time.After(time.Second):
		t.Error("job context was not cancelled within 1s of StopBackgroundJobs")
	}
}

// StopBackgroundJobs called with no in-flight jobs must return immediately.
func TestStopBackgroundJobsIsSafeWhenIdle(t *testing.T) {
	h := &Handler{jobs: newJobs()}
	start := time.Now()
	h.StopBackgroundJobs()
	if time.Since(start) > 100*time.Millisecond {
		t.Error("idle StopBackgroundJobs should return immediately")
	}
}
