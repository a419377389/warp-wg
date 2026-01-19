package main

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

type App struct {
	paths       Paths
	log         *Logger
	logStream   *LogStream
	remote      *RemoteClient
	uiURL       string
	notifyToken string

	cfgMu sync.Mutex
	cfg   LocalConfig

	gatewayMu      sync.Mutex
	gateway        *GatewayService
	gatewayPort    int
	gatewayStarted time.Time

	warpMu  sync.Mutex
	warpCmd *exec.Cmd

	accountsMu     sync.Mutex
	memorySnapshot *AccountsSnapshot

	mcpSwitchMu   sync.Mutex
	mcpSkipEmail  string
	mcpSkipUntil  time.Time
}

func NewApp(paths Paths, cfg LocalConfig, log *Logger, stream *LogStream) *App {
	return &App{
		paths:       paths,
		log:         log,
		logStream:   stream,
		remote:      NewRemoteClient(),
		notifyToken: newRandomToken(24),
		cfg:         cfg,
	}
}

func (a *App) getConfig() LocalConfig {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg
}

func (a *App) updateConfig(update func(*LocalConfig)) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	update(&a.cfg)
	return saveConfig(a.paths.ConfigFile, a.cfg)
}

func (a *App) markMCPSyncHandled(email string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return
	}
	a.mcpSwitchMu.Lock()
	a.mcpSkipEmail = email
	a.mcpSkipUntil = time.Now().Add(5 * time.Second)
	a.mcpSwitchMu.Unlock()
}

func (a *App) consumeMCPSyncSkip(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	a.mcpSwitchMu.Lock()
	defer a.mcpSwitchMu.Unlock()
	if a.mcpSkipEmail == "" {
		return false
	}
	if time.Now().After(a.mcpSkipUntil) {
		a.mcpSkipEmail = ""
		return false
	}
	if a.mcpSkipEmail == email {
		a.mcpSkipEmail = ""
		return true
	}
	return false
}

// getRemoteCredentials 返回远程API所需的token和deviceID
func (a *App) getRemoteCredentials() (token, deviceID string) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg.Token, a.cfg.DeviceID
}
