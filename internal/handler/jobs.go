package handler

import (
	"context"
	"sync"
	"time"
)

// jobs holds the shared state for background goroutines spawned by the
// handler (search indexing, retention sweeps, cleanup tickers). All such
// goroutines run under a single cancellable context so they exit promptly
// when StopBackgroundJobs is called, and are tracked by a WaitGroup so
// main can drain them before the process exits.
type jobs struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newJobs creates a fresh jobs registry with a context derived from
// context.Background. Each Handler gets its own — there is no global
// shared registry.
func newJobs() *jobs {
	ctx, cancel := context.WithCancel(context.Background())
	return &jobs{ctx: ctx, cancel: cancel}
}

// runJob registers fn as an in-flight background goroutine. fn receives a
// context that is cancelled when StopBackgroundJobs is called, so
// long-running scans should check ctx.Err() periodically.
//
// Per-request callers use this instead of bare `go func()` so the work
// can drain on shutdown rather than be abandoned mid-flight.
func (h *Handler) runJob(fn func(ctx context.Context)) {
	h.jobs.wg.Add(1)
	go func() {
		defer h.jobs.wg.Done()
		fn(h.jobs.ctx)
	}()
}

// StopBackgroundJobs cancels the shared jobs context (signalling all
// in-flight goroutines to wind down) and blocks until they return. Called
// by main during graceful shutdown, after the HTTP server has stopped
// accepting new requests. Safe to call multiple times; second and later
// calls are no-ops since cancel is idempotent and Wait returns immediately
// once the counter is zero.
func (h *Handler) StopBackgroundJobs() {
	h.jobs.cancel()
	h.jobs.wg.Wait()
}

// StartBackgroundWorkers spawns the long-running periodic workers
// (retention sweep, session cleanup, rate-limit cleanup). Each runs under
// runJob so StopBackgroundJobs drains it cleanly.
func (h *Handler) StartBackgroundWorkers() {
	h.runJob(func(ctx context.Context) {
		h.StartRetentionWorker(ctx)
	})

	h.runJob(func(ctx context.Context) {
		h.sessionCleanupLoop(ctx)
	})

	h.runJob(func(ctx context.Context) {
		h.rateLimitCleanupLoop(ctx)
	})
}

// sessionCleanupLoop deletes expired session rows once an hour. Wired
// (audit M-12) so the sessions table doesn't grow without bound.
func (h *Handler) sessionCleanupLoop(ctx context.Context) {
	const interval = 1 * time.Hour
	if err := h.sessions.DeleteExpired(ctx); err != nil {
		h.logger.Error("session cleanup: initial sweep", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.sessions.DeleteExpired(ctx); err != nil {
				h.logger.Error("session cleanup: periodic sweep", "error", err)
			}
		}
	}
}

// rateLimitCleanupLoop prunes stale in-memory rate-limit entries every
// 10 minutes (audit M-12). Without this the map grows linearly with the
// number of distinct client IPs observed since boot.
func (h *Handler) rateLimitCleanupLoop(ctx context.Context) {
	const interval = 10 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.loginLimiter.Cleanup()
		}
	}
}
