package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

// Probe checks whether BuildKit inside one VM is ready.
type Probe interface {
	Ready(ctx context.Context, address string, tls tlsmaterial.Paths) error
}

// PortAllocator reserves one host loopback port until its release function runs.
type PortAllocator interface {
	ReserveLoopback() (port uint16, release func() error, err error)
}

// Options provides the explicit process and lifecycle dependencies.
type Options struct {
	Launcher        Launcher
	Probe           Probe
	Ports           PortAllocator
	Locks           func(path string) (io.Closer, error)
	ReadyTimeout    time.Duration
	ProbeInterval   time.Duration
	ShutdownTimeout time.Duration
	Shutdown        func(path string, timeout time.Duration) error
}

// Instance owns one running QEMU process and its exclusive state lock.
type Instance struct {
	process         Process
	waitResult      <-chan error
	stateLock       io.Closer
	proxyFile       string
	shutdownPath    string
	shutdownAddress string
	shutdown        func(path string, timeout time.Duration) error
	address         string
	shutdownTimeout time.Duration
	closeOnce       sync.Once
	closeErr        error
}

// Start acquires the persistent state exclusively and starts one independent VM.
func Start(ctx context.Context, config Config, stateLockPath string, options Options) (*Instance, error) {
	if config.HostPort != 0 || config.ShutdownPort != 0 {
		return nil, fmt.Errorf("VM ports must be zero before allocation")
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	stateLock, err := options.Locks(stateLockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire VM state lock: %w", err)
	}
	cleanup := func() error {
		return errors.Join(removeProxyFile(config.ProxyFile), cleanupShutdownPath(config.ShutdownPath), stateLock.Close())
	}
	if err := config.validateInputs(); err != nil {
		return nil, errors.Join(fmt.Errorf("validate VM configuration: %w", err), cleanup())
	}

	port, releasePort, err := options.Ports.ReserveLoopback()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("reserve VM loopback port: %w", err), cleanup())
	}
	config.HostPort = port
	shutdownPort, shutdownAddress, releaseShutdown, err := reserveShutdownEndpoint(options.Ports, config.ShutdownPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("reserve Guest shutdown endpoint: %w", err), releasePort(), cleanup())
	}
	config.ShutdownPort = shutdownPort
	args, err := config.Args()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("construct QEMU arguments: %w", err), releaseShutdown(), releasePort(), cleanup())
	}
	launchPath, launchArgs, err := qemuCommand(config, args)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("construct QEMU launch command: %w", err), releaseShutdown(), releasePort(), cleanup())
	}
	if err := errors.Join(releaseShutdown(), releasePort()); err != nil {
		return nil, errors.Join(fmt.Errorf("release VM loopback reservation: %w", err), cleanup())
	}
	process, err := options.Launcher.Start(launchPath, launchArgs)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("launch QEMU: %w", err), cleanup())
	}
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- process.Wait()
	}()
	instance := &Instance{
		process: process, waitResult: waitResult, stateLock: stateLock,
		proxyFile:       config.ProxyFile,
		shutdownPath:    config.ShutdownPath,
		shutdownAddress: shutdownAddress,
		shutdown:        options.Shutdown,
		address:         "tcp://127.0.0.1:" + strconv.FormatUint(uint64(port), 10),
		shutdownTimeout: options.ShutdownTimeout,
	}
	if err := waitUntilReady(ctx, config, options, instance.waitResult, instance.address); err != nil {
		var processExit *processExitError
		if errors.As(err, &processExit) {
			return nil, errors.Join(err, removeProxyFile(instance.proxyFile), cleanupShutdownPath(instance.shutdownPath), instance.stateLock.Close())
		}
		return nil, errors.Join(err, instance.forceClose())
	}
	return instance, nil
}

// Address returns the loopback BuildKit endpoint for this VM.
func (instance *Instance) Address() string {
	if instance == nil {
		return ""
	}
	return instance.address
}

// Close terminates one VM and releases its owned resources exactly once.
func (instance *Instance) Close() error {
	if instance == nil {
		return nil
	}
	instance.closeOnce.Do(func() {
		select {
		case waitErr := <-instance.waitResult:
			instance.closeErr = errors.Join(waitErr, removeProxyFile(instance.proxyFile), cleanupShutdownPath(instance.shutdownPath), instance.stateLock.Close())
			return
		default:
		}
		shutdownErr := instance.shutdown(instance.shutdownAddress, instance.shutdownTimeout)
		if shutdownErr == nil {
			timer := time.NewTimer(instance.shutdownTimeout)
			defer timer.Stop()
			select {
			case waitErr := <-instance.waitResult:
				instance.closeErr = errors.Join(waitErr, removeProxyFile(instance.proxyFile), cleanupShutdownPath(instance.shutdownPath), instance.stateLock.Close())
				return
			case <-timer.C:
				shutdownErr = fmt.Errorf("wait for Guest shutdown: %w", context.DeadlineExceeded)
			}
		}
		instance.closeErr = errors.Join(shutdownErr, instance.stopProcess(), removeProxyFile(instance.proxyFile), cleanupShutdownPath(instance.shutdownPath), instance.stateLock.Close())
	})
	return instance.closeErr
}

func (instance *Instance) forceClose() error {
	instance.closeOnce.Do(func() {
		instance.closeErr = errors.Join(instance.stopProcess(), removeProxyFile(instance.proxyFile), cleanupShutdownPath(instance.shutdownPath), instance.stateLock.Close())
	})
	return instance.closeErr
}

func (instance *Instance) stopProcess() error {
	terminateErr := normalizeProcessSignalError(instance.process.Terminate())
	timer := time.NewTimer(instance.shutdownTimeout)
	defer timer.Stop()
	var killErr error
	var waitErr error
	select {
	case waitErr = <-instance.waitResult:
	case <-timer.C:
		killErr = normalizeProcessSignalError(instance.process.Kill())
		waitErr = <-instance.waitResult
	}
	return errors.Join(
		terminateErr,
		killErr,
		waitErr,
	)
}

type processExitError struct {
	err error
}

func (processError *processExitError) Error() string {
	return fmt.Sprintf("QEMU exited before Runtime was ready: %v", processError.err)
}

func (processError *processExitError) Unwrap() error {
	return processError.err
}

func waitUntilReady(ctx context.Context, config Config, options Options, waitResult <-chan error, address string) error {
	readyContext, cancel := context.WithTimeout(ctx, options.ReadyTimeout)
	defer cancel()
	probeTicker := time.NewTicker(options.ProbeInterval)
	defer probeTicker.Stop()
	serialFailures := monitorSerial(readyContext, config.SerialPath, options.ProbeInterval)
	probeResults := make(chan error, 1)
	probeInFlight := false
	var lastProbeErr error

	startProbe := func() {
		probeInFlight = true
		go func() {
			probeResults <- options.Probe.Ready(readyContext, address, config.TLS)
		}()
	}
	startProbe()

	for {
		select {
		case probeErr := <-probeResults:
			probeInFlight = false
			if probeErr == nil {
				return nil
			}
			lastProbeErr = probeErr
		case <-probeTicker.C:
			if !probeInFlight {
				startProbe()
			}
		case processErr := <-waitResult:
			if processErr == nil {
				processErr = fmt.Errorf("QEMU exited without an error")
			}
			return &processExitError{err: processErr}
		case serialErr := <-serialFailures:
			return serialErr
		case <-readyContext.Done():
			return errors.Join(fmt.Errorf("wait for VM readiness: %w", readyContext.Err()), lastProbeErr)
		}
	}
}

func monitorSerial(ctx context.Context, path string, interval time.Duration) <-chan error {
	failures := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				contents, err := os.ReadFile(path)
				if err != nil {
					failures <- fmt.Errorf("read VM serial log: %w", err)
					return
				}
				if bytes.Contains(contents, []byte("SEALBUILD_RUNTIME_FAILED")) {
					failures <- fmt.Errorf("VM serial log reported SEALBUILD_RUNTIME_FAILED")
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return failures
}

func validateOptions(options Options) error {
	if options.Launcher == nil || options.Probe == nil || options.Ports == nil || options.Locks == nil || options.Shutdown == nil {
		return fmt.Errorf("VM lifecycle dependencies are required")
	}
	if options.ReadyTimeout <= 0 || options.ProbeInterval <= 0 || options.ShutdownTimeout <= 0 {
		return fmt.Errorf("VM lifecycle durations must be positive")
	}
	return nil
}

func removeProxyFile(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove Guest proxy file: %w", err)
	}
	return nil
}

func normalizeProcessSignalError(err error) error {
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
