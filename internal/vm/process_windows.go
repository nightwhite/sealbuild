//go:build windows

package vm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type execProcess struct {
	command   *exec.Cmd
	job       windows.Handle
	waitOnce  sync.Once
	waitErr   error
	closeOnce sync.Once
}

func startExecProcess(path string, args []string) (Process, error) {
	command := exec.Command(path, args...)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		return nil, err
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, stopUnassignedProcess(command, fmt.Errorf("create QEMU Job Object: %w", err))
	}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limit)),
		uint32(unsafe.Sizeof(limit)),
	)
	if err != nil {
		return nil, stopJobProcess(command, job, fmt.Errorf("configure QEMU Job Object: %w", err))
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		return nil, stopJobProcess(command, job, fmt.Errorf("open QEMU process for Job Object: %w", err))
	}
	assignErr := windows.AssignProcessToJobObject(job, processHandle)
	closeErr := windows.CloseHandle(processHandle)
	if assignErr != nil || closeErr != nil {
		return nil, stopJobProcess(command, job, errors.Join(
			wrapWindowsError("assign QEMU process to Job Object", assignErr),
			wrapWindowsError("close QEMU process handle", closeErr),
		))
	}
	return &execProcess{command: command, job: job}, nil
}

func (process *execProcess) Wait() error {
	process.waitOnce.Do(func() {
		waitErr := process.command.Wait()
		process.closeOnce.Do(func() {
			process.waitErr = errors.Join(wrapWindowsError("wait for QEMU process", waitErr), wrapWindowsError("close QEMU Job Object", windows.CloseHandle(process.job)))
		})
	})
	return process.waitErr
}

func (process *execProcess) Terminate() error {
	if process.command.ProcessState != nil {
		return os.ErrProcessDone
	}
	if err := windows.TerminateJobObject(process.job, 1); err != nil {
		return fmt.Errorf("terminate QEMU Job Object: %w", err)
	}
	return nil
}

func (process *execProcess) Kill() error {
	if process.command.ProcessState != nil {
		return os.ErrProcessDone
	}
	if err := windows.TerminateJobObject(process.job, 137); err != nil {
		return fmt.Errorf("kill QEMU Job Object: %w", err)
	}
	return nil
}

func stopUnassignedProcess(command *exec.Cmd, primary error) error {
	return errors.Join(primary, command.Process.Kill(), command.Wait())
}

func stopJobProcess(command *exec.Cmd, job windows.Handle, primary error) error {
	return errors.Join(primary, windows.TerminateJobObject(job, 1), command.Wait(), windows.CloseHandle(job))
}

func wrapWindowsError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", stage, err)
}
