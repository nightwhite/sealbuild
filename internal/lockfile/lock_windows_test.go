//go:build windows

package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWindowsLockRejectsContentionAndAllowsReuse(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "build.lock")
	first, err := TryAcquire(lockPath)
	if err != nil {
		t.Fatalf("first TryAcquire() error = %v", err)
	}
	second, err := TryAcquire(lockPath)
	if !errors.Is(err, ErrContended) {
		t.Fatalf("second TryAcquire() error = %v, want ErrContended", err)
	}
	if second != nil {
		t.Fatal("second TryAcquire() returned a lock on contention")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	third, err := TryAcquire(lockPath)
	if err != nil {
		t.Fatalf("third TryAcquire() error = %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close() error = %v", err)
	}
}
