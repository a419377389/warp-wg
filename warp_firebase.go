package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const defaultWarpFirebaseAPIKey = "AIzaSyBdy3O3S9hrdayLJxJ7mriBR4qgUaUygAs"

var (
	warpFirebaseKeyMu sync.RWMutex
	warpFirebaseKey   = defaultWarpFirebaseAPIKey
	firebaseKeyRe     = regexp.MustCompile(`firebase_api_key:\s*"([^"]+)"`)
)

func getWarpFirebaseAPIKey() string {
	warpFirebaseKeyMu.RLock()
	key := warpFirebaseKey
	warpFirebaseKeyMu.RUnlock()
	if strings.TrimSpace(key) == "" {
		return defaultWarpFirebaseAPIKey
	}
	return key
}

func setWarpFirebaseAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	warpFirebaseKeyMu.Lock()
	if key == warpFirebaseKey {
		warpFirebaseKeyMu.Unlock()
		return false
	}
	warpFirebaseKey = key
	warpFirebaseKeyMu.Unlock()
	return true
}

func (a *App) bootstrapWarpFirebaseKey() {
	if a == nil {
		return
	}
	cfg := a.getConfig()
	if strings.TrimSpace(cfg.WarpFirebaseAPIKey) != "" {
		setWarpFirebaseAPIKey(cfg.WarpFirebaseAPIKey)
	}
	if key := readWarpFirebaseKeyFromLog(); key != "" {
		_ = a.updateWarpFirebaseAPIKey(key, "warp.log")
	}
}

func (a *App) updateWarpFirebaseAPIKey(key, source string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if !setWarpFirebaseAPIKey(key) {
		return false
	}
	if a != nil {
		_ = a.updateConfig(func(cfg *LocalConfig) {
			if cfg.WarpFirebaseAPIKey != key {
				cfg.WarpFirebaseAPIKey = key
			}
		})
		if a.log != nil {
			msg := "warp firebase api key updated"
			if strings.TrimSpace(source) != "" {
				msg += ": " + source
			}
			a.log.Info(msg)
		}
	}
	return true
}

func readWarpFirebaseKeyFromLog() string {
	logPath := warpLogPath()
	if logPath == "" || !pathExists(logPath) {
		return ""
	}
	data, err := readFileTail(logPath, 512*1024)
	if err != nil || len(data) == 0 {
		return ""
	}
	return findFirebaseAPIKeyInLog(data)
}

func findFirebaseAPIKeyInLog(data []byte) string {
	matches := firebaseKeyRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	key := string(matches[len(matches)-1][1])
	return strings.TrimSpace(key)
}

func warpLogPath() string {
	dir := warpDataDir()
	if dir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(dir, "logs", "warp.log"),
		filepath.Join(dir, "warp.log"),
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate
		}
	}
	return filepath.Join(dir, "logs", "warp.log")
}

func readFileTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if maxBytes > 0 && size > maxBytes {
		if _, err := f.Seek(size-maxBytes, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}
