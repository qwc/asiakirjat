package handler

import "sync"

// keyedMutex hands out one mutex per int64 key so callers can serialize work
// that touches a single project's storage without blocking other projects.
// It is used to make archive extraction (upload) and directory moves (rename)
// mutually exclusive per project: both mutate the project's on-disk directory,
// and a rename that runs os.Rename while an extract is mid-write would corrupt
// or strand the deployment.
//
// Keys are project IDs, which are immutable across a rename — locking on the
// slug would break the moment the slug changed.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[int64]*sync.Mutex)}
}

// Lock blocks until the mutex for key is held and returns the unlock func.
// Per-key mutexes are created on demand and retained; the count is bounded by
// the number of projects, so no eviction is needed for this workload.
func (k *keyedMutex) Lock(key int64) func() {
	k.mu.Lock()
	m, ok := k.locks[key]
	if !ok {
		m = &sync.Mutex{}
		k.locks[key] = m
	}
	k.mu.Unlock()

	m.Lock()
	return m.Unlock
}
