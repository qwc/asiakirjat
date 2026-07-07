package handler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Concurrent Lock calls for the same key must never overlap: the max number
// of goroutines inside the critical section at once must be 1.
func TestKeyedMutexSerializesSameKey(t *testing.T) {
	km := newKeyedMutex()

	var active, maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := km.Lock(7)
			defer unlock()

			n := atomic.AddInt32(&active, 1)
			for {
				old := atomic.LoadInt32(&maxActive)
				if n <= old || atomic.CompareAndSwapInt32(&maxActive, old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()

	if maxActive != 1 {
		t.Errorf("same-key critical sections overlapped: max concurrent = %d, want 1", maxActive)
	}
}

// Locks on different keys must be independent — two goroutines holding
// different keys at the same time must both make progress. If the
// implementation shared a single mutex this would deadlock and the test's
// timeout would fire.
func TestKeyedMutexDifferentKeysAreIndependent(t *testing.T) {
	km := newKeyedMutex()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, key := range []int64{1, 2} {
		wg.Add(1)
		go func(k int64) {
			defer wg.Done()
			unlock := km.Lock(k)
			defer unlock()
			started <- struct{}{} // signal we hold the lock
			<-release             // ...and keep holding until told to let go
		}(key)
	}

	// Both goroutines must reach "holding" before either releases; if keys
	// blocked each other, only one would signal and this would hang.
	<-started
	<-started
	close(release)
	wg.Wait()
}

// The unlock func returned by Lock releases the key so a later Lock on the
// same key succeeds.
func TestKeyedMutexUnlockReleases(t *testing.T) {
	km := newKeyedMutex()

	unlock := km.Lock(3)
	unlock()

	done := make(chan struct{})
	go func() {
		unlock2 := km.Lock(3)
		unlock2()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Lock on the same key blocked after unlock")
	}
}
