// Package lockfile provides non-blocking Sealbuild process locks.
package lockfile

import (
	"errors"
	"os"
	"sync"
)

// ErrContended indicates that another process owns the requested lock.
var ErrContended = errors.New("sealbuild lock is already held")

// Lock owns one operating-system file lock.
type Lock struct {
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

// Close releases the lock and closes its file descriptor.
func (lock *Lock) Close() error {
	if lock == nil {
		return nil
	}
	lock.closeOnce.Do(func() {
		lock.closeErr = errors.Join(release(lock.file), lock.file.Close())
	})
	return lock.closeErr
}
