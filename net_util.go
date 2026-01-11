package main

import (
	"fmt"
	"net"
	"time"
)

func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func findAvailablePort(start, attempts int) int {
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		port := start + i
		if isPortAvailable(port) {
			return port
		}
	}
	return 0
}

func waitForPort(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
