package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getlantern/systray"
)

func runTray(app *App) {
	systray.Run(func() {
		if iconData := loadTrayIcon(app.paths); len(iconData) > 0 {
			systray.SetIcon(iconData)
		}
		systray.SetTitle("Warp \u7f51\u5173")
		systray.SetTooltip("Warp \u65e0\u611f\u6362\u53f7\u7f51\u5173")

		openItem := systray.AddMenuItem("\u6253\u5f00\u63a7\u5236\u53f0", "Open UI")
		startItem := systray.AddMenuItem("\u542f\u52a8\u7f51\u5173", "Start gateway")
		stopItem := systray.AddMenuItem("\u505c\u6b62\u7f51\u5173", "Stop gateway")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("\u9000\u51fa", "Quit")

		go func() {
			for {
				select {
				case <-openItem.ClickedCh:
					openBrowser(app.uiURL)
				case <-startItem.ClickedCh:
					_, _ = app.startGateway(0)
				case <-stopItem.ClickedCh:
					_ = app.stopGateway()
				case <-quitItem.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		time.AfterFunc(800*time.Millisecond, func() {
			openBrowser(app.uiURL)
		})
	}, func() {})
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}

func loadTrayIcon(paths Paths) []byte {
	name := "icon.png"
	if runtime.GOOS == "windows" {
		name = "icon.ico"
	}
	if data, err := embeddedAsset(name); err == nil && len(data) > 0 {
		return data
	}
	assetsDir := resolveAssetsDir(paths)
	if assetsDir == "" {
		return nil
	}
	iconPath := filepath.Join(assetsDir, name)
	data, err := os.ReadFile(iconPath)
	if err != nil {
		return nil
	}
	return data
}

func resolveAssetsDir(paths Paths) string {
	if override := os.Getenv("ASSETS_DIR"); override != "" && pathExists(override) {
		return override
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "assets")
		if pathExists(candidate) {
			return candidate
		}
		candidate = filepath.Join(cwd, "go-gateway", "assets")
		if pathExists(candidate) {
			return candidate
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		candidate := filepath.Join(exeDir, "assets")
		if pathExists(candidate) {
			return candidate
		}
	}
	root := resolveProjectRoot(paths)
	candidate := filepath.Join(root, "go-gateway", "assets")
	if pathExists(candidate) {
		return candidate
	}
	return ""
}
