package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryAcquireRejectsContentionAndAllowsReuseAfterClose(t *testing.T) {
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

func TestTryAcquireCreatesPrivateLockFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "runtime.lock")
	lock, err := TryAcquire(lockPath)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	defer lock.Close()

	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %#o, want 0600", info.Mode().Perm())
	}
}

func TestTryAcquireDoesNotCreateParentDirectory(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "missing", "build.lock")
	lock, err := TryAcquire(lockPath)
	if err == nil {
		lock.Close()
		t.Fatal("TryAcquire() error = nil, want missing parent error")
	}
	if !strings.Contains(err.Error(), "open lock file") {
		t.Fatalf("TryAcquire() error = %q, want open context", err)
	}
}
