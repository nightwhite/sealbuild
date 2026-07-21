//go:build !windows

package vm

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labring/sealbuild/internal/lockfile"
	"github.com/labring/sealbuild/internal/tlsmaterial"
)

func TestStartRunsOneIndependentVMAndCloseReleasesResources(t *testing.T) {
	config := lifecycleConfig(t, true)
	process := newFakeProcess(true)
	launcher := &fakeLauncher{process: process}
	ports := &fakePorts{port: 49153}
	stateLock := &fakeCloser{}
	probe := &fakeProbe{results: []error{nil}}

	instance, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), Options{
		Launcher: launcher, Probe: probe, Ports: ports,
		Locks:        func(string) (io.Closer, error) { return stateLock, nil },
		ReadyTimeout: time.Second, ProbeInterval: time.Millisecond, ShutdownTimeout: time.Second,
		Shutdown: func(string, time.Duration) error { process.exit(nil); return nil },
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if instance.Address() != "tcp://127.0.0.1:49153" {
		t.Fatalf("Address() = %q", instance.Address())
	}
	if launcher.starts != 1 || ports.reservations != 1 || ports.releases != 1 {
		t.Fatalf("starts = %d reservations = %d releases = %d, want 1 each", launcher.starts, ports.reservations, ports.releases)
	}
	if launcher.path != expectedTestLaunchPath(config) {
		t.Fatalf("launcher path = %q, want %q", launcher.path, expectedTestLaunchPath(config))
	}
	joinedArgs := strings.Join(launcher.args, " ")
	if !strings.Contains(joinedArgs, "hostfwd=tcp:127.0.0.1:49153-:1234") {
		t.Fatalf("launcher args do not contain allocated port: %s", joinedArgs)
	}
	if probe.addresses()[0] != instance.Address() {
		t.Fatalf("probe address = %q, want %q", probe.addresses()[0], instance.Address())
	}
	if stateLock.closeCount != 0 {
		t.Fatalf("state lock closed before VM Close: %d", stateLock.closeCount)
	}

	if err := instance.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if process.terminateCount != 0 || process.killCount != 0 || process.waitCount != 1 {
		t.Fatalf("Terminate = %d Kill = %d Wait = %d", process.terminateCount, process.killCount, process.waitCount)
	}
	if stateLock.closeCount != 1 {
		t.Fatalf("state lock close count = %d, want 1", stateLock.closeCount)
	}
	if _, err := os.Stat(config.ProxyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("proxy Stat() error = %v, want removed", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	if process.terminateCount != 0 || stateLock.closeCount != 1 {
		t.Fatalf("repeated Close changed cleanup counts: Terminate = %d lock Close = %d", process.terminateCount, stateLock.closeCount)
	}
}

func TestStartRejectsAssignedHostPort(t *testing.T) {
	config := lifecycleConfig(t, false)
	config.HostPort = 49152
	launcher := &fakeLauncher{process: newFakeProcess(true)}
	ports := &fakePorts{port: 49153}
	locks := 0

	_, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(launcher, &fakeProbe{}, ports, func(string) (io.Closer, error) {
		locks++
		return &fakeCloser{}, nil
	}))
	if err == nil || !strings.Contains(err.Error(), "VM ports must be zero before allocation") {
		t.Fatalf("Start() error = %v, want unassigned host port", err)
	}
	if locks != 0 || ports.reservations != 0 || launcher.starts != 0 {
		t.Fatalf("invalid config used resources: locks = %d ports = %d starts = %d", locks, ports.reservations, launcher.starts)
	}
}

func TestStartStopsBeforePortAllocationWhenStateIsLocked(t *testing.T) {
	config := lifecycleConfig(t, false)
	launcher := &fakeLauncher{process: newFakeProcess(true)}
	ports := &fakePorts{port: 49153}

	_, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(launcher, &fakeProbe{}, ports, func(string) (io.Closer, error) {
		return nil, lockfile.ErrContended
	}))
	if !errors.Is(err, lockfile.ErrContended) {
		t.Fatalf("Start() error = %v, want ErrContended", err)
	}
	if ports.reservations != 0 || launcher.starts != 0 {
		t.Fatalf("contended Start reserved %d ports and launched %d processes", ports.reservations, launcher.starts)
	}
}

func TestStartCleansUpWhenPortOrLaunchFails(t *testing.T) {
	portError := errors.New("port unavailable")
	launchError := errors.New("QEMU start failed")
	tests := []struct {
		name        string
		ports       *fakePorts
		launcher    *fakeLauncher
		wantError   error
		wantRelease int
	}{
		{name: "port allocation", ports: &fakePorts{reserveErr: portError}, launcher: &fakeLauncher{}, wantError: portError},
		{name: "launcher", ports: &fakePorts{port: 49153}, launcher: &fakeLauncher{startErr: launchError}, wantError: launchError, wantRelease: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := lifecycleConfig(t, true)
			stateLock := &fakeCloser{}
			_, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(test.launcher, &fakeProbe{}, test.ports, func(string) (io.Closer, error) {
				return stateLock, nil
			}))
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Start() error = %v, want %v", err, test.wantError)
			}
			if stateLock.closeCount != 1 {
				t.Fatalf("state lock close count = %d, want 1", stateLock.closeCount)
			}
			if test.ports.releases != test.wantRelease {
				t.Fatalf("port release count = %d, want %d", test.ports.releases, test.wantRelease)
			}
			if _, statErr := os.Stat(config.ProxyFile); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("proxy Stat() error = %v, want removed", statErr)
			}
		})
	}
}

func TestStartRetriesProbeWithoutRestartingVM(t *testing.T) {
	config := lifecycleConfig(t, false)
	process := newFakeProcess(true)
	launcher := &fakeLauncher{process: process}
	ports := &fakePorts{port: 49153}
	probe := &fakeProbe{results: []error{errors.New("not ready"), nil}}

	instance, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(launcher, probe, ports, func(string) (io.Closer, error) {
		return &fakeCloser{}, nil
	}))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer instance.Close()
	if len(probe.addresses()) != 2 {
		t.Fatalf("Probe calls = %d, want 2", len(probe.addresses()))
	}
	if launcher.starts != 1 || ports.reservations != 1 || ports.releases != 1 {
		t.Fatalf("retry changed process resources: starts = %d reservations = %d releases = %d", launcher.starts, ports.reservations, ports.releases)
	}
}

func TestStartTimeoutTerminatesThenKillsOneVM(t *testing.T) {
	config := lifecycleConfig(t, false)
	process := newFakeProcess(false)
	launcher := &fakeLauncher{process: process}
	ports := &fakePorts{port: 49153}
	probeResults := make([]error, 100)
	for index := range probeResults {
		probeResults[index] = errors.New("not ready")
	}
	probe := &fakeProbe{results: probeResults}
	options := lifecycleOptions(launcher, probe, ports, func(string) (io.Closer, error) { return &fakeCloser{}, nil })
	options.ReadyTimeout = 10 * time.Millisecond
	options.ShutdownTimeout = time.Millisecond

	_, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), options)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v, want DeadlineExceeded", err)
	}
	if process.terminateCount != 1 || process.killCount != 1 || process.waitCount != 1 {
		t.Fatalf("Terminate = %d Kill = %d Wait = %d, want 1 each", process.terminateCount, process.killCount, process.waitCount)
	}
	if launcher.starts != 1 || ports.reservations != 1 {
		t.Fatalf("timeout restarted resources: starts = %d reservations = %d", launcher.starts, ports.reservations)
	}
}

func TestStartCancellationUsesSameShutdownPath(t *testing.T) {
	config := lifecycleConfig(t, false)
	process := newFakeProcess(true)
	probe := &fakeProbe{called: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := Start(ctx, config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(
			&fakeLauncher{process: process}, probe, &fakePorts{port: 49153}, func(string) (io.Closer, error) { return &fakeCloser{}, nil },
		))
		result <- err
	}()
	waitSignal(t, probe.called, "Probe call")
	cancel()
	err := waitError(t, result, "Start cancellation")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want Canceled", err)
	}
	if process.terminateCount != 1 || process.killCount != 0 {
		t.Fatalf("Terminate = %d Kill = %d", process.terminateCount, process.killCount)
	}
}

func TestStartReturnsWhenQEMUExitsBeforeReady(t *testing.T) {
	processError := errors.New("QEMU exited")
	process := newFakeProcess(false)
	process.exit(processError)
	probe := &fakeProbe{}
	config := lifecycleConfig(t, false)
	stateLockPath := filepath.Join(t.TempDir(), "state.lock")
	result := make(chan error, 1)
	go func() {
		_, err := Start(t.Context(), config, stateLockPath, lifecycleOptions(
			&fakeLauncher{process: process}, probe, &fakePorts{port: 49153}, func(string) (io.Closer, error) { return &fakeCloser{}, nil },
		))
		result <- err
	}()
	err := waitError(t, result, "early QEMU exit")
	if !errors.Is(err, processError) {
		t.Fatalf("Start() error = %v, want process error", err)
	}
	if process.terminateCount != 0 || process.killCount != 0 || process.waitCount != 1 {
		t.Fatalf("early exit cleanup: Terminate = %d Kill = %d Wait = %d", process.terminateCount, process.killCount, process.waitCount)
	}
}

func TestStartStopsOnSerialRuntimeFailure(t *testing.T) {
	config := lifecycleConfig(t, false)
	process := newFakeProcess(true)
	probe := &fakeProbe{called: make(chan struct{}, 1)}
	result := make(chan error, 1)
	go func() {
		_, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(
			&fakeLauncher{process: process}, probe, &fakePorts{port: 49153}, func(string) (io.Closer, error) { return &fakeCloser{}, nil },
		))
		result <- err
	}()
	waitSignal(t, probe.called, "Probe call")
	if err := os.WriteFile(config.SerialPath, []byte("boot\nSEALBUILD_RUNTIME_FAILED: buildkitd\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(serial) error = %v", err)
	}
	err := waitError(t, result, "serial failure")
	if err == nil || !strings.Contains(err.Error(), "SEALBUILD_RUNTIME_FAILED") {
		t.Fatalf("Start() error = %v, want serial failure", err)
	}
	if process.terminateCount != 1 {
		t.Fatalf("Terminate count = %d, want 1", process.terminateCount)
	}
}

func TestInstanceCloseJoinsShutdownAndCleanupErrors(t *testing.T) {
	terminateError := errors.New("terminate failed")
	killError := errors.New("kill failed")
	waitProcessError := errors.New("wait failed")
	lockError := errors.New("lock release failed")
	config := lifecycleConfig(t, true)
	process := newFakeProcess(false)
	process.terminateErr = terminateError
	process.killErr = killError
	process.exitErr = waitProcessError
	stateLock := &fakeCloser{closeErr: lockError}
	options := lifecycleOptions(&fakeLauncher{process: process}, &fakeProbe{results: []error{nil}}, &fakePorts{port: 49153}, func(string) (io.Closer, error) {
		return stateLock, nil
	})
	options.ShutdownTimeout = time.Millisecond
	shutdownError := errors.New("shutdown failed")
	options.Shutdown = func(string, time.Duration) error { return shutdownError }

	instance, err := Start(t.Context(), config, filepath.Join(t.TempDir(), "state.lock"), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := os.Remove(config.ProxyFile); err != nil {
		t.Fatalf("Remove(proxy) error = %v", err)
	}
	if err := os.Mkdir(config.ProxyFile, 0o700); err != nil {
		t.Fatalf("Mkdir(proxy path) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(config.ProxyFile, "child"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(proxy child) error = %v", err)
	}

	closeErr := instance.Close()
	for _, want := range []error{shutdownError, terminateError, killError, waitProcessError, lockError} {
		if !errors.Is(closeErr, want) {
			t.Errorf("Close() error = %v, want joined %v", closeErr, want)
		}
	}
	if closeErr == nil || !strings.Contains(closeErr.Error(), "remove Guest proxy file") {
		t.Errorf("Close() error = %v, want proxy cleanup error", closeErr)
	}
	if repeated := instance.Close(); repeated != closeErr {
		t.Fatalf("repeated Close() error = %v, want same error %v", repeated, closeErr)
	}
}

func TestInstanceCloseAcceptsProcessExitDuringTerminate(t *testing.T) {
	process := newFakeProcess(true)
	process.terminateErr = os.ErrProcessDone
	instance, err := Start(t.Context(), lifecycleConfig(t, false), filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(
		&fakeLauncher{process: process}, &fakeProbe{results: []error{nil}}, &fakePorts{port: 49153}, func(string) (io.Closer, error) { return &fakeCloser{}, nil },
	))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("Close() error = %v, want process-already-exited success", err)
	}
}

func TestInstanceConcurrentCloseRunsCleanupOnce(t *testing.T) {
	process := newFakeProcess(true)
	stateLock := &fakeCloser{}
	instance, err := Start(t.Context(), lifecycleConfig(t, false), filepath.Join(t.TempDir(), "state.lock"), lifecycleOptions(
		&fakeLauncher{process: process}, &fakeProbe{results: []error{nil}}, &fakePorts{port: 49153}, func(string) (io.Closer, error) { return stateLock, nil },
	))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	results := make(chan error, 8)
	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			results <- instance.Close()
		}()
	}
	callers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}
	if process.terminateCount != 0 || process.waitCount != 1 || stateLock.closeCount != 1 {
		t.Fatalf("cleanup counts: Terminate = %d Wait = %d lock Close = %d", process.terminateCount, process.waitCount, stateLock.closeCount)
	}
}

func lifecycleConfig(t *testing.T, withProxy bool) Config {
	t.Helper()
	config := validConfig(t)
	config.HostPort = 0
	if withProxy {
		config.ProxyFile = writeConfigFile(t, filepath.Join(t.TempDir(), "proxy"), "http://10.0.2.2:7890", 0o600)
	}
	return config
}

func lifecycleOptions(launcher Launcher, probe Probe, ports PortAllocator, locks func(string) (io.Closer, error)) Options {
	shutdown := func(string, time.Duration) error { return nil }
	if fake, ok := launcher.(*fakeLauncher); ok {
		if process, ok := fake.process.(*fakeProcess); ok {
			shutdown = func(string, time.Duration) error { process.exit(nil); return nil }
		}
	}
	return Options{
		Launcher: launcher, Probe: probe, Ports: ports, Locks: locks,
		ReadyTimeout: 100 * time.Millisecond, ProbeInterval: time.Millisecond, ShutdownTimeout: 20 * time.Millisecond,
		Shutdown: shutdown,
	}
}

type fakeCloser struct {
	closeCount int
	closeErr   error
}

func (closer *fakeCloser) Close() error {
	closer.closeCount++
	return closer.closeErr
}

type fakePorts struct {
	port         uint16
	reserveErr   error
	releaseErr   error
	reservations int
	releases     int
}

func (ports *fakePorts) ReserveLoopback() (uint16, func() error, error) {
	ports.reservations++
	if ports.reserveErr != nil {
		return 0, nil, ports.reserveErr
	}
	return ports.port, func() error {
		ports.releases++
		return ports.releaseErr
	}, nil
}

type fakeLauncher struct {
	process  Process
	startErr error
	starts   int
	path     string
	args     []string
}

func (launcher *fakeLauncher) Start(path string, args []string) (Process, error) {
	launcher.starts++
	launcher.path = path
	launcher.args = append([]string(nil), args...)
	if launcher.startErr != nil {
		return nil, launcher.startErr
	}
	return launcher.process, nil
}

type fakeProcess struct {
	mutex           sync.Mutex
	waitResult      chan error
	exitOnTerminate bool
	exitOnce        sync.Once
	terminateErr    error
	killErr         error
	exitErr         error
	terminateCount  int
	killCount       int
	waitCount       int
}

func newFakeProcess(exitOnTerminate bool) *fakeProcess {
	return &fakeProcess{waitResult: make(chan error, 1), exitOnTerminate: exitOnTerminate}
}

func (process *fakeProcess) Wait() error {
	process.mutex.Lock()
	process.waitCount++
	process.mutex.Unlock()
	return <-process.waitResult
}

func (process *fakeProcess) Terminate() error {
	process.mutex.Lock()
	process.terminateCount++
	process.mutex.Unlock()
	if process.exitOnTerminate {
		process.exit(process.exitErr)
	}
	return process.terminateErr
}

func (process *fakeProcess) Kill() error {
	process.mutex.Lock()
	process.killCount++
	process.mutex.Unlock()
	process.exit(process.exitErr)
	return process.killErr
}

func (process *fakeProcess) exit(err error) {
	process.exitOnce.Do(func() { process.waitResult <- err })
}

type fakeProbe struct {
	mutex   sync.Mutex
	results []error
	calls   []string
	called  chan struct{}
}

func (probe *fakeProbe) Ready(ctx context.Context, address string, _ tlsmaterial.Paths) error {
	probe.mutex.Lock()
	probe.calls = append(probe.calls, address)
	if probe.called != nil {
		select {
		case probe.called <- struct{}{}:
		default:
		}
	}
	if len(probe.results) > 0 {
		result := probe.results[0]
		probe.results = probe.results[1:]
		probe.mutex.Unlock()
		return result
	}
	probe.mutex.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (probe *fakeProbe) addresses() []string {
	probe.mutex.Lock()
	defer probe.mutex.Unlock()
	return append([]string(nil), probe.calls...)
}

func waitSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitError(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return nil
	}
}
