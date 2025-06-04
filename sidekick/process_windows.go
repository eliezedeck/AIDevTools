//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// configureProcessGroup sets up the process (Windows-specific)
func configureProcessGroup(cmd *exec.Cmd) {
	// Windows doesn't support process groups in the same way as Unix
	// We create a new process group for basic process isolation
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Use HideWindow to avoid creating console windows
		HideWindow: true,
	}
}

// terminateProcessGroup sends termination signal to a process (Windows-specific)
func terminateProcessGroup(pid int) error {
	// On Windows, we don't have SIGTERM equivalent
	// We need to try graceful termination first, but for now we'll
	// indicate that the caller should use process.Kill()
	return fmt.Errorf("windows termination requires process.Kill()")
}

// forceKillProcessGroup forcefully kills a process (Windows-specific)
func forceKillProcessGroup(pid int) error {
	// On Windows, we don't have SIGKILL equivalent
	// The caller should use process.Kill() instead
	return fmt.Errorf("windows force kill requires process.Kill()")
}
