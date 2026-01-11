package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type legacyRemoteConfig struct {
	BaseURL      string `json:"baseUrl"`
	Token        string `json:"token"`
	ExpiresAt    int64  `json:"expiresAt"`
	AccountCount int    `json:"accountCount"`
	DeviceID     string `json:"deviceId"`
}

type legacyWarpConfig struct {
	WarpPath string `json:"warpPath"`
}

func applyLegacyConfig(paths Paths, cfg LocalConfig) LocalConfig {
	legacyRemote := filepath.Join(paths.DataDir, "remote_backend.json")
	if cfg.DeviceID == "" || cfg.Token == "" || cfg.ExpiresAt == 0 || cfg.AccountCount == 0 {
		if raw, err := os.ReadFile(legacyRemote); err == nil && len(raw) > 0 {
			var legacy legacyRemoteConfig
			if json.Unmarshal(raw, &legacy) == nil {
				if cfg.DeviceID == "" && legacy.DeviceID != "" {
					cfg.DeviceID = legacy.DeviceID
				}
				if cfg.Token == "" && legacy.Token != "" {
					cfg.Token = legacy.Token
				}
				if cfg.ExpiresAt == 0 && legacy.ExpiresAt > 0 {
					cfg.ExpiresAt = legacy.ExpiresAt
				}
				if cfg.AccountCount == 0 && legacy.AccountCount > 0 {
					cfg.AccountCount = legacy.AccountCount
				}
			}
		}
	}

	legacyWarp := filepath.Join(paths.DataDir, "warp_config.json")
	if cfg.WarpPath == "" {
		if raw, err := os.ReadFile(legacyWarp); err == nil && len(raw) > 0 {
			var legacy legacyWarpConfig
			if json.Unmarshal(raw, &legacy) == nil {
				if strings.TrimSpace(legacy.WarpPath) != "" {
					cfg.WarpPath = legacy.WarpPath
				}
			}
		}
	}

	return cfg
}
