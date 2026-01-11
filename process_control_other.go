//go:build !windows

package main

import "os/exec"

func hideWindowCommand(cmd *exec.Cmd) {
}
