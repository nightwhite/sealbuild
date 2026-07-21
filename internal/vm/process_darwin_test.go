//go:build darwin

package vm

import (
	"strings"
	"testing"
	"time"
)

func TestExecLauncherTerminatesAndReapsProcess(t *testing.T) {
	testExecProcessSignal(t, "terminate", func(process Process) error {
		return process.Terminate()
	})
}

func TestExecLauncherKillsAndReapsProcess(t *testing.T) {
	testExecProcessSignal(t, "kill", func(process Process) error {
		return process.Kill()
	})
}

func TestExecLauncherReturnsStartError(t *testing.T) {
	_, err := (ExecLauncher{}).Start("/sealbuild/missing/qemu", nil)
	if err == nil || !strings.Contains(err.Error(), "start QEMU process") {
		t.Fatalf("Start() error = %v, want start context", err)
	}
}

func testExecProcessSignal(t *testing.T, label string, signal func(Process) error) {
	t.Helper()
	process, err := (ExecLauncher{}).Start("/bin/sleep", []string{"60"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := signal(process); err != nil {
		if killErr := process.Kill(); killErr != nil {
			t.Errorf("Kill() after signal failure = %v", killErr)
		}
		t.Fatalf("%s process error = %v", label, err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- process.Wait() }()
	select {
	case err := <-waitResult:
		if err == nil || !strings.Contains(err.Error(), "wait for QEMU process") {
			t.Fatalf("Wait() error = %v, want signaled process error", err)
		}
	case <-time.After(time.Second):
		if killErr := process.Kill(); killErr != nil {
			t.Errorf("Kill() after Wait timeout = %v", killErr)
		}
		t.Fatalf("timed out waiting for %s process", label)
	}
}
