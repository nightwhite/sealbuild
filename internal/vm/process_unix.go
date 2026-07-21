//go:build !windows

package vm

import (
	"fmt"
	"os/exec"
	"syscall"
)

type execProcess struct {
	command *exec.Cmd
}

func startExecProcess(path string, args []string) (Process, error) {
	command := exec.Command(path, args...)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &execProcess{command: command}, nil
}

func (process *execProcess) Wait() error {
	if err := process.command.Wait(); err != nil {
		return fmt.Errorf("wait for QEMU process: %w", err)
	}
	return nil
}

func (process *execProcess) Terminate() error {
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("terminate QEMU process: %w", err)
	}
	return nil
}

func (process *execProcess) Kill() error {
	if err := process.command.Process.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill QEMU process: %w", err)
	}
	return nil
}
