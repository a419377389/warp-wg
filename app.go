package main

import (
	"os/exec"
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

// getRemoteCredentials 返回远程API所需的token和deviceID
func (a *App) getRemoteCredentials() (token, deviceID string) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg.Token, a.cfg.DeviceID
}
