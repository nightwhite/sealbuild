package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/labring/sealbuild/internal/cache"
	"github.com/labring/sealbuild/internal/lockfile"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/internal/tlsmaterial"
	"github.com/labring/sealbuild/internal/vm"
)

type runtimeInstaller interface {
	Install(context.Context, runtimepkg.Bundle) (runtimepkg.Installation, error)
}

type runningVM interface {
	Address() string
	Close() error
}

type vmStarter func(context.Context, vm.Config, string, vm.Options) (runningVM, error)

type solveExecutor interface {
	Solve(context.Context, string, tlsmaterial.Paths, PreparedRequest, io.Writer) error
}

const guestShutdownTimeout = 45 * time.Second

// Runner owns the full local Runtime, VM, and BuildKit workflow for one build.
type Runner struct {
	Bundle runtimepkg.Bundle
	Layout cache.Layout
	Probe  vm.Probe
	Solver solveExecutor

	installer runtimeInstaller
	start     vmStarter
}

// NewRunner constructs the production local OCI build workflow.
func NewRunner(bundle runtimepkg.Bundle, layout cache.Layout) Runner {
	probe := Probe{Open: OpenBuildKitClient}
	return Runner{
		Bundle: bundle, Layout: layout, Probe: probe, Solver: Solver{Open: OpenSolveClient},
		installer: runtimepkg.Installer{Layout: layout},
		start: func(ctx context.Context, config vm.Config, lockPath string, options vm.Options) (runningVM, error) {
			return vm.Start(ctx, config, lockPath, options)
		},
	}
}

// Build executes one independent local VM build and preserves only its cache disk and serial log.
func (runner Runner) Build(ctx context.Context, request Request, progress io.Writer) (returnErr error) {
	if runner.installer == nil || runner.start == nil || runner.Solver == nil || runner.Probe == nil {
		return fmt.Errorf("local build Runner dependencies are required")
	}
	prepared, err := Prepare(request)
	if err != nil {
		return err
	}
	installation, err := runner.installer.Install(ctx, runner.Bundle)
	if err != nil {
		return fmt.Errorf("install Runtime: %w", err)
	}
	serialFile, err := os.CreateTemp(runner.Layout.LogDir(), "vm-*.serial.log")
	if err != nil {
		return fmt.Errorf("create VM serial log: %w", err)
	}
	serialPath := serialFile.Name()
	if err := errors.Join(serialFile.Chmod(0o600), serialFile.Sync(), serialFile.Close()); err != nil {
		return fmt.Errorf("prepare VM serial log: %w", err)
	}
	shutdownPath, err := vm.PrepareShutdownPath()
	if err != nil {
		return err
	}

	proxyFile := ""
	proxyCleanup := func() error { return nil }
	if request.Proxy != nil {
		proxyFile, proxyCleanup, err = request.Proxy.WriteGuestFile(runner.Layout.Root)
		if err != nil {
			return fmt.Errorf("create Guest proxy material: %w", err)
		}
	}
	defer func() { returnErr = errors.Join(returnErr, proxyCleanup()) }()

	stateLockPath, err := runner.Layout.StateLockPath(installation.CompatibilityID)
	if err != nil {
		return err
	}
	instance, err := runner.start(ctx, vm.Config{
		QEMUPath:     filepath.Join(installation.Host, "bin", vm.QEMUExecutableName()),
		FirmwarePath: filepath.Join(installation.Host, "share", "qemu"),
		KernelPath:   filepath.Join(installation.Guest, "bzImage"),
		RootFSPath:   filepath.Join(installation.Guest, "rootfs.ext4"),
		StatePath:    installation.StateDisk, SerialPath: serialPath,
		ShutdownPath: shutdownPath,
		TLS:          installation.TLS, ProxyFile: proxyFile,
	}, stateLockPath, vm.Options{
		Launcher: vm.ExecLauncher{}, Probe: runner.Probe, Ports: vm.LoopbackPorts{},
		Locks:        func(path string) (io.Closer, error) { return lockfile.TryAcquire(path) },
		ReadyTimeout: 30 * time.Second, ProbeInterval: 250 * time.Millisecond, ShutdownTimeout: guestShutdownTimeout,
		Shutdown: vm.RequestGuestShutdown,
	})
	if err != nil {
		return fmt.Errorf("start local BuildKit VM: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, instance.Close()) }()
	if err := runner.Solver.Solve(ctx, instance.Address(), installation.TLS, prepared, progress); err != nil {
		return err
	}
	return nil
}
