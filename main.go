package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const appPort = 9530

func main() {
	paths, err := resolvePaths()
	if err != nil {
		fmt.Println("failed to resolve paths:", err)
		os.Exit(1)
	}
	if err := ensureDirs(paths); err != nil {
		fmt.Println("failed to prepare data dirs:", err)
		os.Exit(1)
	}
	if err := ensureDataFiles(paths); err != nil {
		fmt.Println("failed to prepare data files:", err)
		os.Exit(1)
	}

	cfg, err := loadConfig(paths.ConfigFile)
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}
	cfg = ensureDeviceID(cfg)
	cfg = applyLegacyConfig(paths, cfg)
	if err := saveConfig(paths.ConfigFile, cfg); err != nil {
		fmt.Println("failed to save config:", err)
		os.Exit(1)
	}

	stream := NewLogStream()
	logPath := filepath.Join(paths.LogDir, "go-gateway.log")
	logger, err := NewLogger(logPath, stream)
	if err != nil {
		fmt.Println("failed to init logger:", err)
		os.Exit(1)
	}
	fmt.Println("log file:", logPath)
	logger.Info("log file: " + logPath)

	app := NewApp(paths, cfg, logger, stream)
	app.uiURL = fmt.Sprintf("http://127.0.0.1:%d", appPort)
	app.bootstrapWarpFirebaseKey()

	go func() {
		if err := app.startHTTP(appPort); err != nil {
			logger.Error("http server stopped: " + err.Error())
		}
	}()

	runTray(app)
}
