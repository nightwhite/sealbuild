//go:build linux

package lockfile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// TryAcquire obtains an exclusive lock without waiting.
func TryAcquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	syscall.CloseOnExec(int(file.Fd()))
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(fmt.Errorf("set lock file permissions: %w", err), file.Close())
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.Join(fmt.Errorf("%w: %s", ErrContended, path), closeErr)
		}
		return nil, errors.Join(fmt.Errorf("acquire lock file %s: %w", path, err), closeErr)
	}
	return &Lock{file: file}, nil
}

func release(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("release lock file: %w", err)
	}
	return nil
}
