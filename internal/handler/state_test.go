package handler

import (
	"sync"
	"testing"
	"time"
)

// Regression coverage for H-10. The existing handler tests don't deliberately
// race reads against the reindex worker goroutine or hammer the latest-tags
// cache from many goroutines at once. These two tests do, so `go test -race`
// will catch any future refactor that drops the lock.

func TestReindexStateConcurrent(t *testing.T) {
	var s reindexState

	if !s.tryStart() {
		t.Fatal("first tryStart should succeed on a fresh state")
	}
	if s.tryStart() {
		t.Fatal("second tryStart must fail while running")
	}

	const writers = 4
	const readers = 8
	const ops = 200

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				s.setProgress("progress msg")
			}
		}(i)
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_, _ = s.snapshot()
			}
		}()
	}
	wg.Wait()

	s.finish()
	if running, _ := s.snapshot(); running {
		t.Error("finish should clear running")
	}
}

func TestLatestTagsCacheConcurrent(t *testing.T) {
	var c latestTagsCache
	now := time.Now()
	c.set(now, map[string]string{"proj": "v1"})

	const goroutines = 16
	const ops = 200

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				switch j % 4 {
				case 0:
					c.get(time.Now())
				case 1:
					c.set(time.Now(), map[string]string{"proj": "v1", "x": "y"})
				case 2:
					c.invalidate()
				case 3:
					c.get(time.Now())
				}
			}
		}(i)
	}
	wg.Wait()
	// No assertion: the value is whatever the last writer set. The point is
	// that -race must report no data races.
}
