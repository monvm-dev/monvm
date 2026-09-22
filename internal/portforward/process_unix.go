// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package portforward

import (
	"os/exec"
	"syscall"
)

// prepareProcess isolates a session and its plugin in a process group.
func prepareProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stopProcess interrupts an entire session process group.
func stopProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGINT)
	}
}

// killProcess kills an entire session process group.
func killProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
