package build

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealbuild/internal/cache"
	proxyconfig "github.com/labring/sealbuild/internal/proxy"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/internal/tlsmaterial"
	"github.com/labring/sealbuild/internal/vm"
)

func TestRunnerInstallsStartsSolvesAndClosesOneVM(t *testing.T) {
	workspace := t.TempDir()
	contextDirectory := filepath.Join(workspace, "context")
	if err := os.Mkdir(contextDirectory, 0o755); err != nil {
		t.Fatalf("Mkdir(context) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDirectory, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(Dockerfile) error = %v", err)
	}
	layout := cache.Layout{Root: filepath.Join(workspace, "cache")}
	if err := os.MkdirAll(layout.LogDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll(logs) error = %v", err)
	}
	installation := runnerInstallation(workspace)
	installer := &fakeRuntimeInstaller{installation: installation}
	runningVM := &fakeRunningVM{address: "tcp://127.0.0.1:49152"}
	starter := &fakeVMStarter{instance: runningVM}
	solver := &fakeSolveExecutor{}
	proxy, err := proxyconfig.Parse("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Parse(proxy) error = %v", err)
	}
	runner := Runner{
		Bundle: runtimepkg.Bundle{}, Layout: layout, Probe: Probe{}, Solver: solver,
		installer: installer, start: starter.Start,
	}
	request := Request{
		ContextDir: contextDirectory, OutputPath: filepath.Join(workspace, "image.tar"), Proxy: &proxy,
	}

	if err := runner.Build(t.Context(), request, io.Discard); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if installer.calls != 1 || starter.calls != 1 || solver.calls != 1 || runningVM.closeCalls != 1 {
		t.Fatalf("calls: install=%d start=%d solve=%d close=%d", installer.calls, starter.calls, solver.calls, runningVM.closeCalls)
	}
	if starter.config.QEMUPath != filepath.Join(installation.Host, "bin", "qemu-system-x86_64") ||
		starter.config.KernelPath != filepath.Join(installation.Guest, "bzImage") ||
		starter.config.RootFSPath != filepath.Join(installation.Guest, "rootfs.ext4") ||
		starter.config.StatePath != installation.StateDisk || starter.config.HostPort != 0 {
		t.Fatalf("VM config = %#v", starter.config)
	}
	if starter.config.ProxyFile == "" {
		t.Fatal("VM config proxy file is empty")
	}
	if _, err := os.Stat(starter.config.ProxyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("proxy Stat(after Build) error = %v, want removed", err)
	}
	wantLock, err := layout.StateLockPath(installation.CompatibilityID)
	if err != nil {
		t.Fatalf("StateLockPath() error = %v", err)
	}
	if starter.stateLockPath != wantLock {
		t.Fatalf("state lock path = %q, want %q", starter.stateLockPath, wantLock)
	}
	if starter.options.ShutdownTimeout != guestShutdownTimeout || starter.options.Shutdown == nil {
		t.Fatalf("VM shutdown options = %#v", starter.options)
	}
	if solver.address != runningVM.address || solver.request.OutputPath != request.OutputPath {
		t.Fatalf("solver address = %q request = %#v", solver.address, solver.request)
	}
}

func TestRunnerJoinsSolveAndVMCloseErrors(t *testing.T) {
	solveError := errors.New("solve failed")
	closeError := errors.New("VM close failed")
	workspace := t.TempDir()
	contextDirectory := filepath.Join(workspace, "context")
	if err := os.Mkdir(contextDirectory, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDirectory, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	layout := cache.Layout{Root: filepath.Join(workspace, "cache")}
	if err := os.MkdirAll(layout.LogDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	instance := &fakeRunningVM{address: "tcp://127.0.0.1:49152", closeErr: closeError}
	runner := Runner{
		Layout: layout, Probe: Probe{}, Solver: &fakeSolveExecutor{err: solveError}, installer: &fakeRuntimeInstaller{installation: runnerInstallation(workspace)},
		start: (&fakeVMStarter{instance: instance}).Start,
	}
	err := runner.Build(t.Context(), Request{ContextDir: contextDirectory, OutputPath: filepath.Join(workspace, "image.tar")}, io.Discard)
	if !errors.Is(err, solveError) || !errors.Is(err, closeError) {
		t.Fatalf("Build() error = %v, want solve and close errors", err)
	}
}

func runnerInstallation(workspace string) runtimepkg.Installation {
	return runtimepkg.Installation{
		CompatibilityID: strings.Repeat("a", 64),
		Host:            filepath.Join(workspace, "runtime", "host"), Guest: filepath.Join(workspace, "runtime", "guest"),
		StateDisk: filepath.Join(workspace, "state", "buildkit-state.qcow2"),
		TLS:       tlsmaterial.Paths{CA: "/tls/ca.crt", ClientCert: "/tls/client.crt", ClientKey: "/tls/client.key"},
	}
}

type fakeRuntimeInstaller struct {
	installation runtimepkg.Installation
	err          error
	calls        int
}

func (installer *fakeRuntimeInstaller) Install(context.Context, runtimepkg.Bundle) (runtimepkg.Installation, error) {
	installer.calls++
	return installer.installation, installer.err
}

type fakeVMStarter struct {
	instance      runningVM
	err           error
	calls         int
	config        vm.Config
	stateLockPath string
	options       vm.Options
}

func (starter *fakeVMStarter) Start(_ context.Context, config vm.Config, stateLockPath string, options vm.Options) (runningVM, error) {
	starter.calls++
	starter.config = config
	starter.stateLockPath = stateLockPath
	starter.options = options
	return starter.instance, starter.err
}

type fakeRunningVM struct {
	address    string
	closeErr   error
	closeCalls int
}

func (instance *fakeRunningVM) Address() string { return instance.address }
func (instance *fakeRunningVM) Close() error {
	instance.closeCalls++
	return instance.closeErr
}

type fakeSolveExecutor struct {
	err     error
	calls   int
	address string
	request PreparedRequest
}

func (solver *fakeSolveExecutor) Solve(_ context.Context, address string, _ tlsmaterial.Paths, request PreparedRequest, _ io.Writer) error {
	solver.calls++
	solver.address = address
	solver.request = request
	return solver.err
}
