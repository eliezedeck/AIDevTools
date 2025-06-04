//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup sets up the process to run in its own process group (Unix-specific)
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup kills the entire process group (Unix-specific)
func killProcessGroup(pid int, signal syscall.Signal) error {
	// Kill the entire process group by sending signal to -pid
	return syscall.Kill(-pid, signal)
}

// terminateProcessGroup sends SIGTERM to a process group
func terminateProcessGroup(pid int) error {
	return killProcessGroup(pid, syscall.SIGTERM)
}

// forceKillProcessGroup sends SIGKILL to a process group
func forceKillProcessGroup(pid int) error {
	return killProcessGroup(pid, syscall.SIGKILL)
}