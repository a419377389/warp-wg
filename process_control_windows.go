//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func hideWindowCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
