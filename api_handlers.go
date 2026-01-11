package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleActivationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}

	cfg := a.getConfig()
	if cfg.Token == "" || cfg.DeviceID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "activated": false})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	status, err := a.remote.Status(ctx, cfg.Token, cfg.DeviceID)
	if err != nil {
		if errors.Is(err, errRemoteUnauthorized) {
			_ = a.updateConfig(func(c *LocalConfig) {
				c.Token = ""
				c.ExpiresAt = 0
				c.AccountCount = 0
			})
			// 未授权时停止网关并清理数据
			a.handleActivationExpired()
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "activated": false, "error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	_ = a.updateConfig(func(c *LocalConfig) {
		c.ExpiresAt = status.ExpiresAt
		c.AccountCount = status.AccountCount
	})

	// 卡密到期时停止网关并清理账号数据
	if !status.Active {
		a.handleActivationExpired()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"activated":    true,
		"active":       status.Active,
		"expiresAt":    status.ExpiresAt,
		"accountCount": status.AccountCount,
		"stats":        status.Stats,
		"serverTime":   status.ServerTime,
		"deviceId":     status.DeviceID,
	})
}

// handleActivationExpired 处理卡密到期：停止网关、Warp并清理账号数据
func (a *App) handleActivationExpired() {
	// 停止网关
	_ = a.stopGateway()

	// 停止 Warp
	_ = a.stopWarp()

	// 清理账号数据
	emptySnapshot := AccountsSnapshot{
		LocalAccounts:    []Account{},
		TotalVirtualUsed: 0,
		CurrentAccount:   nil,
		Source:           "local",
	}
	_ = saveAccountsSnapshot(a.paths.AccountsFile, emptySnapshot)
	a.setMemorySnapshot(emptySnapshot)

	if a.log != nil {
		a.log.Warn("卡密已到期，已停止网关并清理账号数据")
	}
}

func (a *App) handleActivationLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	code := strings.TrimSpace(payload.Code)
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "code required"})
		return
	}

	cfg := a.getConfig()
	deviceID := cfg.DeviceID
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := a.remote.Activate(ctx, code, deviceID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	_ = a.updateConfig(func(c *LocalConfig) {
		c.Token = resp.Token
		c.ExpiresAt = resp.ExpiresAt
		c.AccountCount = resp.AccountCount
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"expiresAt":    resp.ExpiresAt,
		"accountCount": resp.AccountCount,
		"stats":        resp.Stats,
	})
}

func (a *App) handleActivationUnbind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	cfg := a.getConfig()
	if cfg.Token == "" || cfg.DeviceID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "not activated"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := a.remote.Unbind(ctx, cfg.Token, cfg.DeviceID); err != nil {
		if errors.Is(err, errRemoteUnauthorized) {
			_ = a.updateConfig(func(c *LocalConfig) {
				c.Token = ""
				c.ExpiresAt = 0
				c.AccountCount = 0
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	_ = a.updateConfig(func(c *LocalConfig) {
		c.Token = ""
		c.ExpiresAt = 0
		c.AccountCount = 0
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *App) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}

	cfg := a.getConfig()
	snapshot, _ := loadAccountsSnapshot(a.paths.AccountsFile)
	source := "local"
	stats := map[string]any{}
	accountCount := cfg.AccountCount
	preferLocal := false

	running, _, _ := a.gatewayStatus()
	if running {
		preferLocal = true
	}

	if cfg.Token != "" && cfg.DeviceID != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		status, err := a.remote.Status(ctx, cfg.Token, cfg.DeviceID)
		if err == nil && status != nil {
			stats = status.Stats
			accountCount = status.AccountCount
			_ = a.updateConfig(func(c *LocalConfig) {
				c.AccountCount = status.AccountCount
				c.ExpiresAt = status.ExpiresAt
			})
		}

		remoteAccounts, err := a.remote.Accounts(ctx, cfg.Token, cfg.DeviceID)
		if err != nil {
			if errors.Is(err, errRemoteUnauthorized) {
				_ = a.updateConfig(func(c *LocalConfig) {
					c.Token = ""
					c.ExpiresAt = 0
					c.AccountCount = 0
				})
			}
		} else {
			merged := mergeRemoteAccounts(remoteAccounts, snapshot, preferLocal)
			a.setMemorySnapshot(merged)
			_ = saveAccountsSnapshot(a.paths.AccountsFile, merged)
			snapshot = merged
			source = "remote"
		}
	}

	responseSnapshot := sanitizeSnapshotForDisk(snapshot)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"source":        source,
		"localAccounts": responseSnapshot.LocalAccounts,
		"currentAccount": responseSnapshot.CurrentAccount,
		"totalVirtualUsed": responseSnapshot.TotalVirtualUsed,
		"lastUpdated":   responseSnapshot.LastUpdated,
		"stats":         stats,
		"accountCount":  accountCount,
		"switchCount":   cfg.SwitchCount,
	})
}

func (a *App) handleAccountSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var payload struct {
		Email       string `json:"email"`
		RestartWarp *bool  `json:"restartWarp"`
	}
	if err := readJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	email := strings.TrimSpace(payload.Email)
	restartWarp := true
	if payload.RestartWarp != nil {
		restartWarp = *payload.RestartWarp
	}

	running, _, _ := a.gatewayStatus()
	preferLocal := running
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	snapshot, err := a.loadSnapshotWithSecrets(ctx, preferLocal)
	if err != nil && len(snapshot.LocalAccounts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	var acc *Account
	if email == "" {
		acc = selectNextAvailableAccount(snapshot)
	} else {
		acc = findAccountByEmail(snapshot, email)
	}
	if acc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "\u6ca1\u6709\u53ef\u7528\u8d26\u53f7"})
		return
	}
	if !accountSelectable(*acc) {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "\u8d26\u53f7\u989d\u5ea6\u4e0d\u53ef\u7528"})
		return
	}
	if strings.TrimSpace(acc.APIKey) == "" {
		refreshed, err := a.refreshRemoteAccounts(ctx, preferLocal)
		if err == nil {
			snapshot = mergeSnapshotWithSecrets(snapshot, refreshed)
			if email == "" {
				acc = selectNextAvailableAccount(snapshot)
			} else {
				acc = findAccountByEmail(snapshot, email)
			}
		}
	}
	if acc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "\u6ca1\u6709\u53ef\u7528\u8d26\u53f7"})
		return
	}
	if !accountSelectable(*acc) {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "\u8d26\u53f7\u989d\u5ea6\u4e0d\u53ef\u7528"})
		return
	}
	if strings.TrimSpace(acc.APIKey) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "\u7f3a\u5c11\u51ed\u8bc1"})
		return
	}

	_ = resetMachineID()

	if restartWarp {
		_ = a.stopWarp()
		time.Sleep(1500 * time.Millisecond)
	}

	if err := updateWarpCredentialsWithLog(*acc, a.log, "manual"); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	if restartWarp {
		proxy := ""
		if running, port, _ := a.gatewayStatus(); running && port > 0 {
			proxy = "http://127.0.0.1:" + strconv.Itoa(port)
		}
		_, _ = a.startWarp(proxy)
	}

	acc.LastUsed = float64(time.Now().Unix())
	updateAccountSnapshot(&snapshot, *acc)
	snapshot.CurrentAccount = acc
	a.setMemorySnapshot(snapshot)
	_ = saveAccountsSnapshot(a.paths.AccountsFile, snapshot)

	_ = a.updateConfig(func(cfg *LocalConfig) {
		cfg.SwitchCount += 1
	})

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "currentAccount": acc})
}

func (a *App) handleAccountRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var payload struct {
		Email string `json:"email"`
	}
	_ = readJSON(r, &payload)

	running, _, _ := a.gatewayStatus()
	preferLocal := running
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	snapshot, err := a.loadSnapshotWithSecrets(ctx, preferLocal)
	if err != nil && len(snapshot.LocalAccounts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	targetEmail := strings.TrimSpace(payload.Email)
	if targetEmail == "" && snapshot.CurrentAccount != nil {
		targetEmail = snapshot.CurrentAccount.Email
	}
	if targetEmail == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "email required"})
		return
	}
	acc := findAccountByEmail(snapshot, targetEmail)
	if acc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "account not found"})
		return
	}
	if strings.TrimSpace(acc.APIKey) == "" {
		refreshed, err := a.refreshRemoteAccounts(ctx, preferLocal)
		if err == nil {
			snapshot = mergeSnapshotWithSecrets(snapshot, refreshed)
			acc = findAccountByEmail(snapshot, targetEmail)
		}
	}
	if acc == nil || strings.TrimSpace(acc.APIKey) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "missing api key"})
		return
	}

	usage := fetchWarpUsage(r.Context(), acc.APIKey)
	if usage.Status == "error" && usage.Error != "" {
		acc.Status = "error"
		acc.ErrorCount += 1
		updateAccountSnapshot(&snapshot, *acc)
		_ = saveAccountsSnapshot(a.paths.AccountsFile, snapshot)
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": usage.Error, "account": acc})
		return
	}
	if usage.Status == "banned" {
		acc.Status = "banned"
		acc.ErrorCount += 1
	} else {
		acc.Status = usage.Status
	}
	if usage.Type != "" {
		acc.Type = usage.Type
	}
	if usage.Quota > 0 {
		acc.Quota = usage.Quota
	}
	if usage.Used >= 0 {
		acc.Used = usage.Used
	}
	if usage.NextRefresh != "" {
		acc.NextRefresh = usage.NextRefresh
	}

	updateAccountSnapshot(&snapshot, *acc)
	_ = saveAccountsSnapshot(a.paths.AccountsFile, snapshot)

	cfg := a.getConfig()
	if cfg.Token != "" && cfg.DeviceID != "" && acc.ID > 0 {
		payload := map[string]any{
			"quota":          acc.Quota,
			"used":           acc.Used,
			"status":         acc.Status,
			"type":           acc.Type,
			"nextRefreshTime": acc.NextRefresh,
			"errorCount":     acc.ErrorCount,
		}
		_ = a.remote.UpdateAccount(context.Background(), cfg.Token, cfg.DeviceID, acc.ID, payload)
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "account": acc})
}

func (a *App) handleNotice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	notice, err := a.remote.Notice(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "notice": notice})
}

func (a *App) handleGatewayStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var payload struct {
		Port int `json:"port"`
	}
	_ = readJSON(r, &payload)
	port, err := a.startGateway(payload.Port)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// startGateway 内部已经处理了 Warp 启动（如果 AutoRestartWarp=true）
	// 所以这里不需要再次启动
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "port": port})
}

func (a *App) handleGatewayStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	if err := a.stopGateway(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *App) handleGatewayStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	running, port, started := a.gatewayStatus()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "running": running, "port": port, "startedAt": started.Unix()})
}

func (a *App) handleWarpStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	runningGateway, port, _ := a.gatewayStatus()
	startedGateway := false
	if !runningGateway {
		startPort, err := a.startGateway(0)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		startedGateway = true
		port = startPort
	}
	if port <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "gateway port unavailable"})
		return
	}
	proxy := "http://127.0.0.1:" + strconv.Itoa(port)
	if a.warpRunning() {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	if _, err := a.prepareWarpAccount(); err != nil {
		if startedGateway {
			_ = a.stopGateway()
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	path, err := a.startWarp(proxy)
	if err != nil {
		if startedGateway {
			_ = a.stopGateway()
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": path})
}

func (a *App) handleWarpStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	if running, _, _ := a.gatewayStatus(); running {
		if err := a.stopGateway(); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	if err := a.stopWarp(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *App) handleWarpStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	cfg := a.getConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"running": a.warpRunning(),
		"path":    cfg.WarpPath,
	})
}

func (a *App) handleWarpPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.getConfig()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": cfg.WarpPath})
		return
	case http.MethodPost:
		var payload struct {
			Path string `json:"path"`
		}
		if err := readJSON(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
			return
		}
		if err := a.setWarpPath(payload.Path); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
	}
}

func (a *App) handleWarpPathAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	path, err := a.autoDetectWarpPath()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": path})
}
