// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package portforward

import "os/exec"

// prepareProcess applies platform process settings before starting a session.
func prepareProcess(_ *exec.Cmd) {}

// stopProcess terminates a session process on Windows.
func stopProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}

// killProcess forcibly terminates a session process on Windows.
func killProcess(command *exec.Cmd) { stopProcess(command) }
