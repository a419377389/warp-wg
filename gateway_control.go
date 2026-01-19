package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) startGateway(port int) (int, error) {
	a.gatewayMu.Lock()
	defer a.gatewayMu.Unlock()

	if a.gateway != nil {
		if a.gateway.Running() {
			return 0, errors.New("gateway already running")
		}
		_ = a.gateway.Stop()
		a.gateway = nil
	}

	cfg := a.getConfig()
	if port <= 0 {
		port = cfg.GatewayPort
	}
	if !isPortAvailable(port) {
		alt := findAvailablePort(port+1, 20)
		if alt == 0 {
			return 0, errors.New("gateway port unavailable")
		}
		port = alt
	}

	if cfg.Token == "" || cfg.DeviceID == "" {
		return 0, errors.New("activation required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := a.remote.Status(ctx, cfg.Token, cfg.DeviceID)
	if err != nil {
		if errors.Is(err, errRemoteUnauthorized) {
			_ = a.updateConfig(func(c *LocalConfig) {
				c.Token = ""
				c.ExpiresAt = 0
				c.AccountCount = 0
			})
		}
		return 0, err
	}
	if status == nil || !status.Active {
		return 0, errors.New("activation expired")
	}

	if _, err := a.refreshRemoteAccounts(ctx, false); err != nil {
		if mem, ok := a.getMemorySnapshot(); !ok || !snapshotHasSecrets(mem) {
			localSnapshot, _ := loadAccountsSnapshot(a.paths.AccountsFile)
			if len(localSnapshot.LocalAccounts) == 0 {
				return 0, errors.New("no accounts available")
			}
			return 0, errors.New("no account secrets available")
		}
	}

	if err := ensureCertificateReady(a.log); err != nil {
		return 0, err
	}

	service, err := newGatewayService(a, port)
	if err != nil {
		return 0, err
	}
	if err := service.Start(); err != nil {
		return 0, err
	}

	a.gateway = service
	a.gatewayPort = port
	a.gatewayStarted = service.StartedAt()

	_ = setSystemProxy(fmt.Sprintf("127.0.0.1:%d", port), true)

	if cfg.AutoRestartWarp {
		if _, err := a.prepareWarpAccount(); err != nil && a.log != nil {
			a.log.Error("warp account prepare failed: " + err.Error())
		}
		_, _ = a.startWarp(fmt.Sprintf("http://127.0.0.1:%d", port))
		// Schedule MCP restore after Warp starts
		scheduleGlobalMCPRestore(5*time.Second, a.log)
	}

	return port, nil
}

func (a *App) prepareWarpAccount() (*Account, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	snapshot, err := a.loadSnapshotWithSecrets(ctx, false)
	if err != nil && len(snapshot.LocalAccounts) == 0 {
		return nil, err
	}

	current := snapshot.CurrentAccount
	if current != nil && !accountSelectable(*current) {
		current = nil
	}
	if current == nil {
		next := selectNextAvailableAccount(snapshot)
		if next != nil {
			snapshot.CurrentAccount = next
			current = next
			if err := saveAccountsSnapshot(a.paths.AccountsFile, snapshot); err != nil && a.log != nil {
				a.log.Error("accounts save failed: " + err.Error())
			}
		} else if snapshot.CurrentAccount != nil && strings.TrimSpace(snapshot.CurrentAccount.APIKey) != "" {
			current = snapshot.CurrentAccount
		}
	}

	if current == nil {
		return nil, errors.New("no available accounts")
	}
	if strings.TrimSpace(current.APIKey) == "" {
		refreshed, err := a.refreshRemoteAccounts(ctx, false)
		if err == nil {
			snapshot = mergeSnapshotWithSecrets(snapshot, refreshed)
			if current.Email != "" {
				if updated := findAccountByEmail(snapshot, current.Email); updated != nil {
					current = updated
				}
			}
		}
	}
	if strings.TrimSpace(current.APIKey) == "" {
		return nil, errors.New("missing api key")
	}
	if err := updateWarpCredentialsWithLog(*current, a.log, "prepare"); err != nil {
		return nil, err
	}
	return current, nil
}

func (a *App) stopGateway() error {
	a.gatewayMu.Lock()
	defer a.gatewayMu.Unlock()

	if a.gateway == nil {
		return errors.New("gateway not running")
	}

	_ = setSystemProxy("", false)
	_ = a.stopWarp()

	if !a.gateway.Running() {
		a.gateway = nil
		a.gatewayPort = 0
		a.gatewayStarted = time.Time{}
		return errors.New("gateway not running")
	}

	_ = a.gateway.Stop()
	a.gateway = nil
	a.gatewayPort = 0
	a.gatewayStarted = time.Time{}
	return nil
}

func (a *App) gatewayStatus() (bool, int, time.Time) {
	a.gatewayMu.Lock()
	defer a.gatewayMu.Unlock()
	if a.gateway == nil || !a.gateway.Running() {
		return false, 0, time.Time{}
	}
	return true, a.gatewayPort, a.gatewayStarted
}

func resolveProjectRoot(paths Paths) string {
	if strings.EqualFold(filepath.Base(paths.DataDir), "data") {
		parent := filepath.Dir(paths.DataDir)
		if pathExists(filepath.Join(parent, "backend")) {
			return parent
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		if pathExists(filepath.Join(exeDir, "backend")) {
			return exeDir
		}
		parent := filepath.Dir(exeDir)
		if pathExists(filepath.Join(parent, "backend")) {
			return parent
		}
	}
	return filepath.Dir(paths.DataDir)
}
