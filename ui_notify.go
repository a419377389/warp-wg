package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type gatewayNotifyPayload struct {
	Event          string `json:"event"`
	Reason         string `json:"reason"`
	PrevEmail      string `json:"prevEmail"`
	CurrentEmail   string `json:"currentEmail"`
	Used           int    `json:"used"`
	Quota          int    `json:"quota"`
	Status         string `json:"status"`
	NextRefresh    string `json:"nextRefreshTime"`
	TimestampMilli int64  `json:"timestamp"`
}

func (a *App) handleGatewayNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Gateway-Token"))
	if token == "" || token != a.notifyToken {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "invalid token"})
		return
	}

	var payload gatewayNotifyPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if payload.Event == "account_switched" && payload.CurrentEmail != "" {
		snapshot, _ := loadAccountsSnapshot(a.paths.AccountsFile)
		acc := findAccountByEmail(snapshot, payload.CurrentEmail)
		if acc == nil {
			acc = &Account{Email: payload.CurrentEmail}
		}
		if payload.Quota > 0 {
			acc.Quota = payload.Quota
		}
		if payload.Used >= 0 {
			acc.Used = payload.Used
		}
		if payload.Status != "" {
			acc.Status = payload.Status
		}
		if payload.NextRefresh != "" {
			acc.NextRefresh = payload.NextRefresh
		}
		if payload.TimestampMilli > 0 {
			acc.LastUsed = float64(payload.TimestampMilli) / 1000.0
		}

		updateAccountSnapshot(&snapshot, *acc)
		snapshot.CurrentAccount = acc
		_ = saveAccountsSnapshot(a.paths.AccountsFile, snapshot)

		_ = a.updateConfig(func(cfg *LocalConfig) {
			cfg.SwitchCount += 1
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
