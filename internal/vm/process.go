package vm

import (
	"fmt"
)

// Process is the lifecycle boundary for one QEMU process.
type Process interface {
	Wait() error
	Terminate() error
	Kill() error
}

// Launcher starts one QEMU process.
type Launcher interface {
	Start(path string, args []string) (Process, error)
}

// ExecLauncher starts QEMU through os/exec.
type ExecLauncher struct{}

// Start launches one process without binding its lifetime to a Context.
func (ExecLauncher) Start(path string, args []string) (Process, error) {
	process, err := startExecProcess(path, args)
	if err != nil {
		return nil, fmt.Errorf("start QEMU process: %w", err)
	}
	return process, nil
}
