//go:build windows

package mcp

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {
	// Windows does not support Unix process groups.
}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
