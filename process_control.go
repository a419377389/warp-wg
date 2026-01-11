package main

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
)

func killProcessByName(name string) error {
	if name == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(strings.ToLower(name), ".exe") {
			name += ".exe"
		}
		cmd := exec.Command("taskkill", "/F", "/IM", name)
		hideWindowCommand(cmd)
		_ = cmd.Run()
		return nil
	}
	cmd := exec.Command("pkill", "-f", name)
	_ = cmd.Run()
	return nil
}

func isProcessRunning(name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(strings.ToLower(name), ".exe") {
			name += ".exe"
		}
		cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name)
		hideWindowCommand(cmd)
		out, err := cmd.Output()
		if err != nil {
			return false, err
		}
		return bytes.Contains(bytes.ToLower(out), bytes.ToLower([]byte(name))), nil
	}
	cmd := exec.Command("pgrep", "-f", name)
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}
