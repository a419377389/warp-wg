package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) startHTTP(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveEmbeddedUI(w)
	})
	if assetsFS, err := embeddedAssetsFS(); err == nil {
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))
	}

	mux.HandleFunc("/api/activation/status", a.handleActivationStatus)
	mux.HandleFunc("/api/activation/login", a.handleActivationLogin)
	mux.HandleFunc("/api/activation/unbind", a.handleActivationUnbind)
	mux.HandleFunc("/api/accounts", a.handleAccounts)
	mux.HandleFunc("/api/accounts/switch", a.handleAccountSwitch)
	mux.HandleFunc("/api/accounts/refresh", a.handleAccountRefresh)
	mux.HandleFunc("/api/notice", a.handleNotice)
	mux.HandleFunc("/api/gateway/start", a.handleGatewayStart)
	mux.HandleFunc("/api/gateway/stop", a.handleGatewayStop)
	mux.HandleFunc("/api/gateway/status", a.handleGatewayStatus)
	mux.HandleFunc("/api/gateway/notify", a.handleGatewayNotify)
	mux.HandleFunc("/api/warp/start", a.handleWarpStart)
	mux.HandleFunc("/api/warp/stop", a.handleWarpStop)
	mux.HandleFunc("/api/warp/status", a.handleWarpStatus)
	mux.HandleFunc("/api/warp/path", a.handleWarpPath)
	mux.HandleFunc("/api/warp/path/auto", a.handleWarpPathAuto)
	mux.HandleFunc("/api/mcp/servers", a.handleMCPServers)
	mux.HandleFunc("/api/mcp/backup", a.handleMCPBackup)
	mux.HandleFunc("/api/mcp/restore", a.handleMCPRestore)
	mux.HandleFunc("/api/mcp/backups", a.handleMCPBackups)
	mux.HandleFunc("/api/mcp/backup/delete", a.handleMCPBackupDelete)
	mux.HandleFunc("/api/logs/stream", a.handleLogStream)
	mux.HandleFunc("/api/logs/tail", a.handleLogTail)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	a.log.Info("UI server listening on " + addr)
	return http.ListenAndServe(addr, mux)
}

func (a *App) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := a.logStream.Subscribe()
	defer a.logStream.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-ch:
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", escapeSSE(line))
			flusher.Flush()
		}
	}
}

func (a *App) handleLogTail(w http.ResponseWriter, r *http.Request) {
	lines := 80
	if v := r.URL.Query().Get("lines"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed < 500 {
			lines = parsed
		}
	}
	path := filepath.Join(a.paths.LogDir, "go-gateway.log")
	result := tailFile(path, lines)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "lines": result})
}

func tailFile(path string, lines int) []string {
	if lines <= 0 {
		return []string{}
	}
	file, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]string, 0, lines)
	for scanner.Scan() {
		buffer = append(buffer, scanner.Text())
		if len(buffer) > lines {
			buffer = buffer[1:]
		}
	}
	return buffer
}

func escapeSSE(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
