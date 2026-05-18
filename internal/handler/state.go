package handler

import (
	"sync"
	"time"
)

// reindexState tracks the background reindex worker's status. The worker
// goroutine started in handleAdminReindex writes here while the admin
// projects page renders concurrent reads, so every access must go through
// the mutex. Previously these were two bare fields on Handler and the
// race detector caught it.
type reindexState struct {
	mu       sync.Mutex
	running  bool
	progress string
}

// tryStart claims the slot. Returns false if a reindex is already running,
// in which case the caller should refuse rather than spawn a second worker.
func (s *reindexState) tryStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.progress = "Starting..."
	return true
}

// setProgress updates the human-readable status string from the worker
// goroutine.
func (s *reindexState) setProgress(msg string) {
	s.mu.Lock()
	s.progress = msg
	s.mu.Unlock()
}

// finish marks the slot empty; called from the worker goroutine's defer.
func (s *reindexState) finish() {
	s.mu.Lock()
	s.running = false
	s.progress = ""
	s.mu.Unlock()
}

// snapshot returns the current state for rendering. Both values are
// captured under the same lock so a renderer never sees an inconsistent
// (running=false, progress="3/10 ...") pairing.
func (s *reindexState) snapshot() (running bool, progress string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.progress
}

// latestTagsCacheTTL is how long the latest version tags cache is valid.
const latestTagsCacheTTL = 30 * time.Second

// latestTagsCache caches the project-slug → latest-version-tag mapping
// used by the search-time filtering logic. Reads are frequent (every
// search query) and writes are rare (post-upload/delete or TTL refresh),
// but both happen from concurrent HTTP goroutines so all access goes
// through the mutex.
//
// The cached map is returned by reference. Callers must not mutate it.
type latestTagsCache struct {
	mu      sync.Mutex
	entries map[string]string
	stored  time.Time
}

// get returns the cached map and true if a non-expired entry exists.
// Otherwise returns (nil, false) and the caller is expected to recompute
// and call set.
func (c *latestTagsCache) get(now time.Time) (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	if now.Sub(c.stored) >= latestTagsCacheTTL {
		return nil, false
	}
	return c.entries, true
}

// set replaces the cached map and resets the TTL clock.
func (c *latestTagsCache) set(now time.Time, entries map[string]string) {
	c.mu.Lock()
	c.entries = entries
	c.stored = now
	c.mu.Unlock()
}

// invalidate drops the cached map so the next get returns a miss.
// Called from upload/delete paths so the next search sees fresh data.
func (c *latestTagsCache) invalidate() {
	c.mu.Lock()
	c.entries = nil
	c.mu.Unlock()
}
