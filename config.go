package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Paths struct {
	DataDir      string
	LogDir       string
	ConfigFile   string
	AccountsFile string
}

type LocalConfig struct {
	DeviceID           string `json:"deviceId"`
	Token              string `json:"token"`
	ExpiresAt          int64  `json:"expiresAt"`
	AccountCount       int    `json:"accountCount"`
	WarpPath           string `json:"warpPath"`
	WarpFirebaseAPIKey string `json:"warpFirebaseApiKey"`
	GatewayPort        int    `json:"gatewayPort"`
	AutoRestartWarp    bool   `json:"autoRestartWarp"`
	AutoRestartGateway bool   `json:"autoRestartGateway"`
	SwitchCount        int    `json:"switchCount"`
	LastUpdated        int64  `json:"lastUpdated"`
}

func resolvePaths() (Paths, error) {
	dataDir := os.Getenv("GATEWAY_DATA_DIR")
	if dataDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, err
		}
		exeDir := filepath.Dir(exe)
		switch runtime.GOOS {
		case "windows":
			dataDir = filepath.Join(exeDir, "data")
		case "darwin":
			if base, err := os.UserConfigDir(); err == nil && base != "" {
				dataDir = filepath.Join(base, "warp-gateway")
			} else if home, err := os.UserHomeDir(); err == nil && home != "" {
				dataDir = filepath.Join(home, "Library", "Application Support", "warp-gateway")
			} else {
				dataDir = filepath.Join(exeDir, "data")
			}
		default:
			if base, err := os.UserConfigDir(); err == nil && base != "" {
				dataDir = filepath.Join(base, "warp-gateway")
			} else if home, err := os.UserHomeDir(); err == nil && home != "" {
				dataDir = filepath.Join(home, ".config", "warp-gateway")
			} else {
				dataDir = filepath.Join(exeDir, "data")
			}
		}
	}
	logDir := filepath.Join(dataDir, "logs")
	return Paths{
		DataDir:      dataDir,
		LogDir:       logDir,
		ConfigFile:   filepath.Join(dataDir, "config.json"),
		AccountsFile: filepath.Join(dataDir, "gateway_accounts.json"),
	}, nil
}

func ensureDirs(paths Paths) error {
	if paths.DataDir == "" {
		return errors.New("data dir not resolved")
	}
	if err := os.MkdirAll(paths.DataDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.LogDir, 0o755); err != nil {
		return err
	}
	return nil
}

func ensureDataFiles(paths Paths) error {
	if paths.DataDir == "" {
		return errors.New("data dir not resolved")
	}
	type fileSpec struct {
		name    string
		content string
	}
	specs := []fileSpec{
		{
			name: "gateway_accounts.json",
			content: "{\n" +
				"  \"localAccounts\": [],\n" +
				"  \"totalVirtualUsed\": 0,\n" +
				"  \"currentAccount\": null,\n" +
				"  \"lastUpdated\": \"\",\n" +
				"  \"source\": \"local\"\n" +
				"}\n",
		},
		{
			name: "remote_backend.json",
			content: "{\n" +
				"  \"baseUrl\": \"\",\n" +
				"  \"token\": \"\",\n" +
				"  \"expiresAt\": 0,\n" +
				"  \"accountCount\": 0,\n" +
				"  \"deviceId\": \"\"\n" +
				"}\n",
		},
		{
			name:    "warp_config.json",
			content: "{\n  \"warpPath\": \"\"\n}\n",
		},
		{
			name:    "warp_rules_config.json",
			content: "{\n  \"rules\": []\n}\n",
		},
		{
			name:    "selected_rule_ids.json",
			content: "{\n  \"ruleIds\": []\n}\n",
		},
		{
			name:    "inject_rules_enabled.json",
			content: "{\n  \"enabled\": true\n}\n",
		},
		{
			name:    "warp_settings_backup.json",
			content: "{}\n",
		},
	}

	for _, spec := range specs {
		path := filepath.Join(paths.DataDir, spec.name)
		info, err := os.Stat(path)
		if err == nil {
			if info.Size() > 0 {
				continue
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, []byte(spec.content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func defaultConfig() LocalConfig {
	return LocalConfig{
		GatewayPort:        9528,
		AutoRestartWarp:    true,
		AutoRestartGateway: false,
		SwitchCount:        0,
		LastUpdated:        time.Now().Unix(),
	}
}

func loadConfig(path string) (LocalConfig, error) {
	cfg := defaultConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func saveConfig(path string, cfg LocalConfig) error {
	cfg.LastUpdated = time.Now().Unix()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func ensureDeviceID(cfg LocalConfig) LocalConfig {
	if cfg.DeviceID != "" {
		return cfg
	}
	cfg.DeviceID = newDeviceID()
	return cfg
}

func newDeviceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "device-" + hex.EncodeToString([]byte(time.Now().Format("20060102150405")))
	}
	return hex.EncodeToString(buf)
}
