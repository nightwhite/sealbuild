//go:build windows

package lockfile

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TryAcquire obtains an exclusive lock without waiting.
func TryAcquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if err != nil {
		closeErr := file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, errors.Join(fmt.Errorf("%w: %s", ErrContended, path), closeErr)
		}
		return nil, errors.Join(fmt.Errorf("acquire lock file %s: %w", path, err), closeErr)
	}
	return &Lock{file: file}, nil
}

func release(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return fmt.Errorf("release lock file: %w", err)
	}
	return nil
}
