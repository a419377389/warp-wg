package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	warpProcessName      = "Warp"
	warpTokenExpiryYears = 10
	warpProxyTokenURL    = "https://app.warp.dev/proxy/token"
)

type warpTokenInfo struct {
	IDToken      string
	RefreshToken string
	Expiration   time.Time
	UserID       string
}

func (a *App) setWarpPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("warp path is empty")
	}
	resolved, err := resolveWarpExecutable(path)
	if err != nil {
		return err
	}
	return a.updateConfig(func(cfg *LocalConfig) {
		cfg.WarpPath = resolved
	})
}

func (a *App) autoDetectWarpPath() (string, error) {
	path := findWarp()
	if path == "" {
		return "", errors.New("warp not found")
	}
	if err := a.setWarpPath(path); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) startWarp(proxy string) (string, error) {
	a.warpMu.Lock()
	defer a.warpMu.Unlock()

	// Restore global MCP config BEFORE Warp starts
	if hasMCPBackup(mcpGlobalBackupKey) {
		if a.log != nil {
			a.log.Info("[MCP] restoring global MCP config before Warp start")
		}
		_ = restoreMCPConfig(mcpGlobalBackupKey)
	}

	if err := ensureCertificateReady(a.log); err != nil {
		return "", err
	}

	cfg := a.getConfig()
	warpPath := strings.TrimSpace(cfg.WarpPath)
	if warpPath != "" {
		resolved, err := resolveWarpExecutable(warpPath)
		if err == nil {
			warpPath = resolved
		} else {
			warpPath = ""
		}
	}
	if warpPath == "" {
		warpPath = findWarp()
		if warpPath == "" {
			return "", errors.New("warp path not found")
		}
	}

	cmd := exec.Command(warpPath)
	cmd.Env = os.Environ()
	if proxy != "" {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("HTTP_PROXY=%s", proxy),
			fmt.Sprintf("HTTPS_PROXY=%s", proxy),
			"NO_PROXY=localhost,127.0.0.1",
		)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", err
	}
	a.warpCmd = cmd
	return warpPath, nil
}

func (a *App) stopWarp() error {
	if err := killProcessByName(warpProcessName); err != nil {
		return err
	}
	a.warpMu.Lock()
	if a.warpCmd != nil && a.warpCmd.Process != nil {
		_ = a.warpCmd.Process.Kill()
	}
	a.warpCmd = nil
	a.warpMu.Unlock()
	return nil
}

func (a *App) warpRunning() bool {
	running, _ := isProcessRunning(warpProcessName)
	return running
}

func resetMachineID() bool {
	if runtime.GOOS == "windows" {
		newID := newUUID()
		psScript := fmt.Sprintf(`$regPath = "HKCU:\SOFTWARE\Warp.dev\Warp"
if (-not (Test-Path $regPath)) { New-Item -Path $regPath -Force | Out-Null }
Set-ItemProperty -Path $regPath -Name "ExperimentId" -Value "%s" -Type String
Write-Output "OK"`, newID)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", psScript)
		out, err := cmd.CombinedOutput()
		return err == nil && bytes.Contains(out, []byte("OK"))
	}

	newID := newUUID()
	updated := false
	for _, filePath := range machineIDCandidateFiles() {
		if !pathExists(filePath) {
			continue
		}
		raw, err := os.ReadFile(filePath)
		if err != nil || len(raw) == 0 {
			continue
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		if setKeyRecursive(data, []string{"ExperimentId", "experimentId", "experiment_id", "machineId", "deviceId", "device_id"}, newID) {
			encoded, _ := json.MarshalIndent(data, "", "  ")
			_ = os.WriteFile(filePath, encoded, 0o644)
			updated = true
		}
	}
	return updated
}

func updateWarpCredentials(account Account) error {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = updateWarpCredentialsMac(account)
	case "linux":
		err = updateWarpCredentialsLinux(account)
	default:
		err = updateWarpCredentialsWindows(account)
	}
	if err == nil && strings.TrimSpace(account.Email) != "" {
		_ = updateWarpDatabaseCurrentUser(account)
	}
	return err
}

func updateWarpCredentialsWithLog(account Account, log *Logger, reason string) error {
	if log != nil {
		msg := "warp credentials update"
		if strings.TrimSpace(reason) != "" {
			msg += " reason=" + reason
		}
		if strings.TrimSpace(account.Email) != "" {
			msg += " account=" + account.Email
		}
		log.Info("[DEBUG] " + msg)
	}


	if err := updateWarpCredentials(account); err != nil {
		if log != nil {
			log.Error("warp credentials update failed: " + err.Error())
		}
		return err
	}
	if log != nil {
		if strings.TrimSpace(account.Email) != "" {
			log.Info("[DEBUG] warp credentials updated: " + account.Email)
		} else {
			log.Info("[DEBUG] warp credentials updated")
		}
	}
	return nil
}

func updateWarpCredentialsLinux(account Account) error {
	tokenInfo, err := buildWarpTokenInfo(account)
	if err != nil {
		return err
	}
	uid := tokenInfo.UserID
	uidSource := "jwt"
	if uid == "" {
		uid = account.UID
		uidSource = "account"
	}
	if uid == "" {
		uid = strings.Split(account.Email, "@")[0]
		uidSource = "email"
	}
	// 调试日志
	fmt.Printf("[DEBUG] Linux credentials: email=%s uid=%s source=%s\n", account.Email, uid, uidSource)
	userJSON := map[string]any{
		"id_token": map[string]any{
			"id_token":        tokenInfo.IDToken,
			"refresh_token":   tokenInfo.RefreshToken,
			"expiration_time": formatWarpExpiration(tokenInfo.Expiration),
		},
		"refresh_token":          tokenInfo.RefreshToken,
		"local_id":               uid,
		"email":                  account.Email,
		"display_name":           nil,
		"photo_url":              nil,
		"is_onboarded":           true,
		"needs_sso_link":         false,
		"anonymous_user_type":    nil,
		"linked_at":              nil,
		"personal_object_limits": nil,
		"is_on_work_domain":      false,
	}
	apiKeysJSON := map[string]any{
		"google":      nil,
		"anthropic":   nil,
		"openai":      nil,
		"open_router": nil,
	}

	// 1. 尝试写入系统密钥环 (secret-tool)
	if err := updateWarpCredentialsLinuxKeyring(userJSON); err != nil {
		fmt.Printf("[DEBUG] Linux keyring write failed: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] Linux keyring write success\n")
	}

	// 2. 同时写入 JSON 文件作为备用
	userFile := warpUserFile()
	apiFile := warpAPIKeysFile()
	fmt.Printf("[DEBUG] Linux data dir: %s\n", warpDataDir())
	fmt.Printf("[DEBUG] Linux user file: %s\n", userFile)
	fmt.Printf("[DEBUG] Linux api file: %s\n", apiFile)
	_ = os.MkdirAll(filepath.Dir(userFile), 0o755)
	raw, _ := json.Marshal(userJSON)
	if err := os.WriteFile(userFile, raw, 0o644); err != nil {
		fmt.Printf("[DEBUG] Linux user file write failed: %v\n", err)
		return err
	}
	fmt.Printf("[DEBUG] Linux user file written: %d bytes\n", len(raw))

	raw, _ = json.Marshal(apiKeysJSON)
	if err := os.WriteFile(apiFile, raw, 0o644); err != nil {
		fmt.Printf("[DEBUG] Linux api file write failed: %v\n", err)
		return err
	}
	fmt.Printf("[DEBUG] Linux api file written: %d bytes\n", len(raw))

	return nil
}

func updateWarpCredentialsLinuxKeyring(userJSON map[string]any) error {
	// 检查 secret-tool 是否可用
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return errors.New("secret-tool not found")
	}

	payload, err := json.Marshal(userJSON)
	if err != nil {
		return err
	}

	// 先删除旧的密钥环条目（两种格式都清理）
	_ = exec.Command("secret-tool", "clear",
		"service", "dev.warp.Warp",
		"key", "User").Run()
	_ = exec.Command("secret-tool", "clear",
		"service", "dev.warp.Warp-Stable",
		"account", "User").Run()

	// 写入密钥环 - Warp 使用 service=dev.warp.Warp + key=User
	cmd := exec.Command("secret-tool", "store", "--label=dev.warp.Warp: User",
		"service", "dev.warp.Warp",
		"key", "User")
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keyring write failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func updateWarpCredentialsMac(account Account) error {
	uid := account.UID
	if uid == "" {
		uid = strings.Split(account.Email, "@")[0]
	}
	tokenInfo, err := buildWarpTokenInfo(account)
	if err != nil {
		return err
	}

	userData := map[string]any{}
	readCmd := exec.Command("security", "find-generic-password", "-s", "dev.warp.Warp-Stable", "-a", "User", "-w")
	if out, err := readCmd.Output(); err == nil {
		raw := strings.TrimSpace(string(out))
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &userData)
		}
	}

	userData["email"] = account.Email
	userData["local_id"] = uid
	userData["is_onboarded"] = false
	userData["is_on_work_domain"] = false

	idToken, ok := userData["id_token"].(map[string]any)
	if !ok {
		idToken = map[string]any{}
	}
	idToken["id_token"] = tokenInfo.IDToken
	idToken["refresh_token"] = tokenInfo.RefreshToken
	idToken["expiration_time"] = formatWarpExpiration(tokenInfo.Expiration)
	userData["id_token"] = idToken
	if tokenInfo.RefreshToken != "" {
		userData["refresh_token"] = tokenInfo.RefreshToken
	}

	payload, _ := json.Marshal(userData)
	_ = exec.Command("security", "delete-generic-password", "-s", "dev.warp.Warp-Stable", "-a", "User").Run()

	writeCmd := exec.Command("security", "add-generic-password", "-s", "dev.warp.Warp-Stable", "-a", "User", "-w", string(payload), "-U")
	if out, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func updateWarpCredentialsWindows(account Account) error {
	userFile := warpUserFile()
	apiFile := warpAPIKeysFile()
	uid := account.UID
	if uid == "" {
		uid = strings.Split(account.Email, "@")[0]
	}
	tokenInfo, err := buildWarpTokenInfo(account)
	if err != nil {
		return err
	}

	userJSON := map[string]any{
		"id_token": map[string]any{
			"id_token":        tokenInfo.IDToken,
			"refresh_token":   tokenInfo.RefreshToken,
			"expiration_time": formatWarpExpiration(tokenInfo.Expiration),
		},
		"refresh_token":          tokenInfo.RefreshToken,
		"local_id":               uid,
		"email":                  account.Email,
		"display_name":           nil,
		"photo_url":              nil,
		"is_onboarded":           true,
		"needs_sso_link":         false,
		"anonymous_user_type":    nil,
		"linked_at":              nil,
		"personal_object_limits": nil,
		"is_on_work_domain":      false,
	}
	apiKeysJSON := map[string]any{
		"google":      nil,
		"anthropic":   nil,
		"openai":      nil,
		"open_router": nil,
	}

	userTemp, err := os.CreateTemp("", "warp_user_*.json")
	if err != nil {
		return err
	}
	defer os.Remove(userTemp.Name())
	apiTemp, err := os.CreateTemp("", "warp_keys_*.json")
	if err != nil {
		return err
	}
	defer os.Remove(apiTemp.Name())

	userRaw, _ := json.Marshal(userJSON)
	apiRaw, _ := json.Marshal(apiKeysJSON)
	if _, err := userTemp.Write(userRaw); err != nil {
		return err
	}
	if _, err := apiTemp.Write(apiRaw); err != nil {
		return err
	}
	_ = userTemp.Close()
	_ = apiTemp.Close()

	psScript := fmt.Sprintf(`Add-Type -AssemblyName System.Security
$userJson = Get-Content -Path '%s' -Raw
$apiKeysJson = Get-Content -Path '%s' -Raw
$userBytes = [System.Text.Encoding]::UTF8.GetBytes($userJson)
$apiKeysBytes = [System.Text.Encoding]::UTF8.GetBytes($apiKeysJson)
$encryptedUser = [System.Security.Cryptography.ProtectedData]::Protect($userBytes, $null, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)
$encryptedApiKeys = [System.Security.Cryptography.ProtectedData]::Protect($apiKeysBytes, $null, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)
[System.IO.File]::WriteAllBytes('%s', $encryptedUser)
[System.IO.File]::WriteAllBytes('%s', $encryptedApiKeys)
Write-Output 'OK'`, escapeWindowsPath(userTemp.Name()), escapeWindowsPath(apiTemp.Name()), escapeWindowsPath(userFile), escapeWindowsPath(apiFile))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %s", strings.TrimSpace(string(out)))
	}
	if !bytes.Contains(out, []byte("OK")) {
		return errors.New("powershell output invalid")
	}
	return nil
}

func updateWarpDatabaseCurrentUser(account Account) error {
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return nil
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	email := strings.TrimSpace(account.Email)
	// 优先从 JWT 中解析 firebase_uid
	tokenInfo, _ := buildWarpTokenInfo(account)
	uid := tokenInfo.UserID
	if uid == "" {
		uid = strings.TrimSpace(account.UID)
	}
	if uid == "" {
		uid = strings.Split(email, "@")[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 更新 current_user_information 表
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS current_user_information (email TEXT PRIMARY KEY NOT NULL)"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM current_user_information"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO current_user_information (email) VALUES (?)", email); err != nil {
		return err
	}

	// 更新 users 表 - 清除旧用户并插入新用户
	_, _ = db.ExecContext(ctx, "DELETE FROM users")
	_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO users (id, firebase_uid) VALUES (1, ?)", uid)

	// 更新 user_profiles 表
	_, _ = db.ExecContext(ctx, "DELETE FROM user_profiles")
	_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO user_profiles (firebase_uid, photo_url, email, display_name) VALUES (?, '', ?, ?)", uid, email, uid)

	return nil
}

func buildWarpTokenInfo(account Account) (warpTokenInfo, error) {
	apiKey := strings.TrimSpace(account.APIKey)
	refreshToken := strings.TrimSpace(account.RefreshToken)
	uid := strings.TrimSpace(account.UID)
	if apiKey == "" {
		if refreshToken == "" {
			return warpTokenInfo{}, errors.New("missing warp api key")
		}
		token, newRefresh, userID, expiresIn, err := fetchWarpIDToken(refreshToken)
		if err == nil && strings.TrimSpace(token) != "" {
			if strings.TrimSpace(newRefresh) != "" {
				refreshToken = newRefresh
			}
			if uid == "" && userID != "" {
				uid = userID
			}
			exp := time.Now().Add(time.Duration(warpTokenExpiryYears) * 365 * 24 * time.Hour)
			if expiresIn > 0 {
				exp = time.Now().Add(expiresIn)
				if expiresIn > 30*time.Second {
					exp = exp.Add(-30 * time.Second)
				}
			}
			return warpTokenInfo{
				IDToken:      token,
				RefreshToken: refreshToken,
				Expiration:   exp,
				UserID:       uid,
			}, nil
		}
		return warpTokenInfo{}, errors.New("missing warp api key")
	}
	// apiKey 格式是 wk-1.xxx，不是 JWT，无法解析 user_id
	// 需要用 refreshToken 换取 JWT 来获取 firebase_uid
	if uid == "" && refreshToken != "" {
		// 先检查缓存
		if cachedUID := cachedWarpUserID(refreshToken); cachedUID != "" {
			uid = cachedUID
		} else {
			// 缓存没有，调用 API
			idToken, _, userID, expiresIn, fetchErr := fetchWarpIDToken(refreshToken)
			// 写入调试文件
			if f, err := os.OpenFile("/tmp/warp-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				fmt.Fprintf(f, "[%s] fetchWarpIDToken: userID=%s idToken_len=%d err=%v\n", time.Now().Format("15:04:05"), userID, len(idToken), fetchErr)
				f.Close()
			}
			if userID != "" {
				uid = userID
				// 缓存结果
				cacheWarpAgentToken(refreshToken, idToken, userID, expiresIn)
			}
		}
	}
	exp := time.Now().Add(time.Duration(warpTokenExpiryYears) * 365 * 24 * time.Hour)
	return warpTokenInfo{
		IDToken:      apiKey,
		RefreshToken: refreshToken,
		Expiration:   exp,
		UserID:       uid,
	}, nil
}

func formatWarpExpiration(expiration time.Time) string {
	if expiration.IsZero() {
		expiration = time.Now().Add(time.Duration(warpTokenExpiryYears) * 365 * 24 * time.Hour)
	}
	return expiration.Format("2006-01-02T15:04:05.000") + "+08:00"
}

func resolveWarpExecutable(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("warp path is empty")
	}

	lower := strings.ToLower(input)
	if runtime.GOOS == "darwin" && strings.HasSuffix(lower, ".app") && pathExists(input) {
		if resolved := warpExecutableFromApp(input); resolved != "" {
			return resolved, nil
		}
	}

	if pathExists(input) {
		info, err := os.Stat(input)
		if err == nil && info.IsDir() {
			if resolved := resolveWarpExecutableInDir(input); resolved != "" {
				return resolved, nil
			}
			if runtime.GOOS == "darwin" {
				if resolved := warpExecutableFromApp(input); resolved != "" {
					return resolved, nil
				}
			}
			return "", fmt.Errorf("warp path invalid: %s", input)
		}
		if isExecutable(input) {
			return input, nil
		}
	}

	if resolved, err := exec.LookPath(input); err == nil && resolved != "" {
		return resolved, nil
	}
	return "", fmt.Errorf("warp path not found: %s", input)
}

func resolveWarpExecutableInDir(dir string) string {
	switch runtime.GOOS {
	case "windows":
		candidates := []string{
			filepath.Join(dir, "Warp.exe"),
			filepath.Join(dir, "warp.exe"),
			filepath.Join(dir, "bin", "Warp.exe"),
			filepath.Join(dir, "bin", "warp.exe"),
		}
		for _, candidate := range candidates {
			if pathExists(candidate) {
				return candidate
			}
		}
	case "darwin":
		if strings.HasSuffix(strings.ToLower(dir), ".app") {
			return warpExecutableFromApp(dir)
		}
		candidates := []string{
			filepath.Join(dir, "Warp"),
			filepath.Join(dir, "stable"),
		}
		for _, candidate := range candidates {
			if pathExists(candidate) {
				return candidate
			}
		}
	default:
		candidates := []string{
			filepath.Join(dir, "warp"),
			filepath.Join(dir, "warp-terminal"),
			filepath.Join(dir, "Warp"),
			filepath.Join(dir, "Warp.AppImage"),
			filepath.Join(dir, "warp.AppImage"),
		}
		for _, candidate := range candidates {
			if pathExists(candidate) && isExecutable(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func warpExecutableFromApp(appPath string) string {
	candidates := []string{
		filepath.Join(appPath, "Contents", "MacOS", "Warp"),
		filepath.Join(appPath, "Contents", "MacOS", "stable"),
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func findWarp() string {
	switch runtime.GOOS {
	case "windows":
		if path := findWarpFromEnv(); path != "" {
			return path
		}
		if path := findWarpInLocalAppData(); path != "" {
			return path
		}
		if path := findWarpFromRegistry(); path != "" {
			return path
		}
		if path := findWarpInProgramFiles(); path != "" {
			return path
		}
		if path := findWarpInWindowsApps(); path != "" {
			return path
		}
		return findWarpInPath()
	case "darwin":
		return findWarpMac()
	default:
		return findWarpLinux()
	}
}

func findWarpFromEnv() string {
	localApp := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(localApp, "Programs", "Warp", "Warp.exe"),
		filepath.Join(localApp, "Programs", "Warp", "warp.exe"),
		filepath.Join(localApp, "Programs", "Warp", "bin", "Warp.exe"),
		filepath.Join(localApp, "Programs", "Warp", "bin", "warp.exe"),
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func findWarpInLocalAppData() string {
	localApp := os.Getenv("LOCALAPPDATA")
	if localApp == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(localApp, "Warp", "Warp.exe"),
		filepath.Join(localApp, "Warp", "warp.exe"),
		filepath.Join(localApp, "Programs", "warp", "Warp.exe"),
		filepath.Join(localApp, "Programs", "warp", "warp.exe"),
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func findWarpFromRegistry() string {
	paths := []string{
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\warp.exe`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\warp.exe`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\warp.exe`,
	}
	for _, key := range paths {
		if value := queryRegistryValue(key, ""); value != "" {
			return value
		}
	}
	locations := []struct {
		key   string
		value string
	}{
		{`HKCU\SOFTWARE\Warp.dev\Warp`, "InstallLocation"},
		{`HKLM\SOFTWARE\Warp.dev\Warp`, "InstallLocation"},
	}
	for _, entry := range locations {
		if value := queryRegistryValue(entry.key, entry.value); value != "" {
			return value
		}
	}
	uninstall := []string{
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Warp`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Warp`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\Warp`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Warp.dev`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Warp.dev`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\Warp.dev`,
	}
	for _, key := range uninstall {
		if value := queryRegistryValue(key, "InstallLocation"); value != "" {
			return value
		}
		if value := queryRegistryValue(key, "DisplayIcon"); value != "" {
			return value
		}
	}
	return ""
}

func findWarpInProgramFiles() string {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Warp", "warp.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Warp", "warp.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Warp", "Warp.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Warp", "Warp.exe"),
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func findWarpInPath() string {
	pathEntries := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	names := []string{"warp"}
	if runtime.GOOS == "windows" {
		names = []string{"warp.exe", "Warp.exe"}
	} else if runtime.GOOS == "linux" {
		names = []string{"warp", "warp-terminal"}
	}
	for _, entry := range pathEntries {
		for _, name := range names {
			candidate := filepath.Join(entry, name)
			if pathExists(candidate) && isExecutable(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func findWarpInWindowsApps() string {
	localApp := os.Getenv("LOCALAPPDATA")
	if localApp == "" {
		return ""
	}
	candidate := filepath.Join(localApp, "Microsoft", "WindowsApps", "warp.exe")
	if pathExists(candidate) {
		return candidate
	}
	candidate = filepath.Join(localApp, "Microsoft", "WindowsApps", "Warp.exe")
	if pathExists(candidate) {
		return candidate
	}
	return ""
}

func findWarpLinux() string {
	if path := findWarpInPath(); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/bin/warp",
		"/usr/local/bin/warp",
		"/usr/bin/warp-terminal",
		"/usr/local/bin/warp-terminal",
		"/opt/warp/warp",
		"/opt/warp/warp-terminal",
		"/opt/Warp/warp",
		"/usr/lib/warp/warp",
		filepath.Join(home, ".local", "bin", "warp"),
		filepath.Join(home, ".local", "bin", "warp-terminal"),
		filepath.Join(home, "Applications", "Warp.AppImage"),
		filepath.Join(home, "Applications", "warp.AppImage"),
		filepath.Join(home, "AppImages", "Warp.AppImage"),
		filepath.Join(home, "AppImage", "Warp.AppImage"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if pathExists(candidate) && isExecutable(candidate) {
			return candidate
		}
	}
	return ""
}

func findWarpMac() string {
	if path := findWarpInPath(); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/Applications/Warp.app/Contents/MacOS/stable",
		filepath.Join(home, "Applications", "Warp.app", "Contents", "MacOS", "stable"),
		"/Applications/Warp.app/Contents/MacOS/Warp",
		filepath.Join(home, "Applications", "Warp.app", "Contents", "MacOS", "Warp"),
	}
	for _, candidate := range candidates {
		if candidate != "" && pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func queryRegistryValue(regPath, valueName string) string {
	args := []string{"query", regPath}
	if valueName == "" {
		args = append(args, "/ve")
	} else {
		args = append(args, "/v", valueName)
	}
	cmd := exec.Command("reg", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "REG_SZ") || strings.Contains(line, "REG_EXPAND_SZ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return normalizeWarpPath(strings.Join(parts[2:], " "))
			}
		}
	}
	return ""
}

func normalizeWarpPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		if strings.HasSuffix(strings.ToLower(value), ".exe") && pathExists(value) {
			return value
		}
		exePath := filepath.Join(value, "warp.exe")
		if pathExists(exePath) {
			return exePath
		}
	}
	if pathExists(value) {
		return value
	}
	return ""
}

func warpDataDir() string {
	if override := strings.TrimSpace(os.Getenv("WARP_DATA_DIR")); override != "" && pathExists(override) {
		return override
	}
	if override := strings.TrimSpace(os.Getenv("WARP_DATA_PATH")); override != "" && pathExists(override) {
		return override
	}

	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "warp", "Warp", "data")
	case "darwin":
		candidates := []string{
			filepath.Join(home, "Library", "Application Support", "dev.warp.Warp"),
			filepath.Join(home, "Library", "Application Support", "warp"),
			filepath.Join(home, ".warp"),
		}
		for _, candidate := range candidates {
			if pathExists(filepath.Join(candidate, "warp.sqlite")) {
				return candidate
			}
		}
		for _, candidate := range candidates {
			if pathExists(candidate) {
				return candidate
			}
		}
		return filepath.Join(home, ".warp")
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		xdgState := os.Getenv("XDG_STATE_HOME")
		if xdgState == "" {
			xdgState = filepath.Join(home, ".local", "state")
		}
		candidates := []string{
			filepath.Join(xdgState, "warp-terminal"),
			filepath.Join(home, ".local", "state", "warp-terminal"),
			filepath.Join(home, ".config", "warp-terminal"),
			filepath.Join(home, ".config", "warp"),
			filepath.Join(xdg, "warp", "Warp", "data"),
			filepath.Join(xdg, "warp"),
			filepath.Join(xdg, "warp-terminal"),
			filepath.Join(home, ".var", "app", "dev.warp.Warp", "data", "warp", "Warp", "data"),
			filepath.Join(home, ".var", "app", "dev.warp.Warp", "data", "warp"),
			filepath.Join(home, ".var", "app", "dev.warp.Warp", "data"),
			filepath.Join(home, "snap", "warp", "common"),
		}
		for _, candidate := range candidates {
			if pathExists(filepath.Join(candidate, "warp.sqlite")) {
				return candidate
			}
		}
		for _, candidate := range candidates {
			if pathExists(candidate) {
				return candidate
			}
		}
		return filepath.Join(xdg, "warp")
	}
}

func warpSQLiteDB() string {
	dir := warpDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "warp.sqlite")
}

func warpUserFile() string {
	dir := warpDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "dev.warp.Warp-User")
}

func warpAPIKeysFile() string {
	dir := warpDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "dev.warp.Warp-AiApiKeys")
}

func machineIDCandidateFiles() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{}
	names := []string{"settings.json", "state.json", "config.json", "app_state.json", "warp_settings.json"}

	if runtime.GOOS == "darwin" {
		dirs := []string{
			warpDataDir(),
			filepath.Join(home, "Library", "Application Support", "dev.warp.Warp"),
			filepath.Join(home, "Library", "Preferences", "dev.warp.Warp"),
			filepath.Join(home, ".warp"),
		}
		for _, dir := range dirs {
			for _, name := range names {
				candidates = append(candidates, filepath.Join(dir, name))
			}
		}
	} else {
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		dirs := []string{
			warpDataDir(),
			filepath.Join(xdg, "warp"),
			filepath.Join(xdg, "warp-terminal"),
		}
		for _, dir := range dirs {
			for _, name := range names {
				candidates = append(candidates, filepath.Join(dir, name))
			}
		}
	}
	return candidates
}

func setKeyRecursive(obj map[string]any, keys []string, value string) bool {
	for _, key := range keys {
		if _, ok := obj[key]; ok {
			obj[key] = value
			return true
		}
	}
	for _, child := range obj {
		if childMap, ok := child.(map[string]any); ok {
			if setKeyRecursive(childMap, keys, value) {
				return true
			}
		}
	}
	return false
}

func fileLooksLikeJSON(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return false
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "{") || !strings.HasSuffix(text, "}") {
		return false
	}
	var data map[string]any
	return json.Unmarshal(raw, &data) == nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	mode := info.Mode()
	if runtime.GOOS == "windows" {
		return strings.HasSuffix(strings.ToLower(path), ".exe")
	}
	return mode&0o111 != 0
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func escapeWindowsPath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}

func fetchWarpIDToken(refreshToken string) (string, string, string, time.Duration, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return "", "", "", 0, errors.New("refresh token empty")
	}
	values := url.Values{}
	values.Set("key", getWarpFirebaseAPIKey())
	tokenURL := warpProxyTokenURL + "?" + values.Encode()

	payload := "grant_type=refresh_token&refresh_token=" + url.QueryEscape(refreshToken)
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(payload))
	if err != nil {
		return "", "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("x-warp-client-version", "v0.2025.12.17.17.17.stable_02")
	req.Header.Set("x-warp-os-category", "desktop")
	req.Header.Set("x-warp-os-name", warpOSName())
	req.Header.Set("x-warp-os-version", warpOSVersion())

	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", "", 0, fmt.Errorf("token refresh failed: status %d", resp.StatusCode)
	}

	var payloadData map[string]any
	if json.Unmarshal(body, &payloadData) != nil {
		return "", "", "", 0, errors.New("token refresh invalid json")
	}

	accessToken := asString(payloadData["access_token"])
	if accessToken == "" {
		accessToken = asString(payloadData["id_token"])
	}
	newRefresh := asString(payloadData["refresh_token"])
	expiresIn := parseExpiresIn(payloadData["expires_in"])
	userID := parseJWTUserID(accessToken)
	return accessToken, newRefresh, userID, expiresIn, nil
}

func parseJWTUserID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payloadPart := parts[1]
	// JWT payload 使用 base64url 编码，需要补齐 padding
	switch len(payloadPart) % 4 {
	case 2:
		payloadPart += "=="
	case 3:
		payloadPart += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payloadPart)
	if err != nil {
		// 尝试标准 base64
		decoded, err = base64.StdEncoding.DecodeString(payloadPart)
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if json.Unmarshal(decoded, &claims) != nil {
		return ""
	}
	// 优先 user_id，其次 sub
	if uid := asString(claims["user_id"]); uid != "" {
		return uid
	}
	return asString(claims["sub"])
}

func parseExpiresIn(value any) time.Duration {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return time.Duration(int64(v)) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case string:
		if v == "" {
			return 0
		}
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed > 0 {
			return time.Duration(parsed) * time.Second
		}
	}
	return 0
}

// AgentProfileBackup Agent Profile备份结构
type AgentProfileBackup struct {
	Profiles  []AgentProfileRecord `json:"profiles"`
	CreatedAt time.Time            `json:"createdAt"`
}

// AgentProfileRecord 单个Agent Profile记录
type AgentProfileRecord struct {
	ID   int64  `json:"id"`
	Data string `json:"data"`
}

// backupWarpDefaultTable 备份Warp数据库中的Default Agent Profile数据
func backupWarpDefaultTable() ([]byte, error) {
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return nil, errors.New("warp database not found")
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 查找所有包含name="Default"且is_default_profile=true的Agent Profile记录，按ID降序排列取最新的一条
	rows, err := db.QueryContext(ctx, `SELECT id, data FROM generic_string_objects WHERE data LIKE '%"name"%' AND data LIKE '%"Default"%' AND data LIKE '%"is_default_profile"%' ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query generic_string_objects: %w", err)
	}
	defer rows.Close()

	var latestProfile *AgentProfileRecord
	for rows.Next() {
		var id int64
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		// 验证是Agent Profile
		var obj map[string]any
		if json.Unmarshal([]byte(data), &obj) == nil {
			if name, ok := obj["name"].(string); ok && name == "Default" {
				if isDefault, ok := obj["is_default_profile"].(bool); ok && isDefault {
					// 只保留第一条（ID最大的）
					if latestProfile == nil {
						latestProfile = &AgentProfileRecord{ID: id, Data: data}
					}
					break
				}
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if latestProfile == nil {
		return nil, errors.New("Default Agent Profile not found")
	}

	profiles := []AgentProfileRecord{*latestProfile}

	backup := AgentProfileBackup{
		Profiles:  profiles,
		CreatedAt: time.Now(),
	}

	finalData, err := json.Marshal(backup)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backup: %w", err)
	}

	return finalData, nil
}

// restoreWarpDefaultTable 还原Warp数据库中的Default Agent Profile数据（在线更新，不关闭Warp）
func restoreWarpDefaultTable(backupData []byte) error {
	if len(backupData) == 0 {
		return errors.New("backup data is empty")
	}

	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return errors.New("warp database not found")
	}

	// 解析备份数据
	var backup AgentProfileBackup
	if err := json.Unmarshal(backupData, &backup); err != nil {
		return fmt.Errorf("failed to unmarshal backup: %w", err)
	}

	if len(backup.Profiles) == 0 {
		return errors.New("no profiles to restore")
	}

	// 使用WAL模式连接，避免锁定问题
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 开始事务
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 先查找并删除所有现有的Default Agent Profile记录
	delRows, err := tx.QueryContext(ctx, `SELECT id FROM generic_string_objects WHERE data LIKE '%"name"%' AND data LIKE '%"Default"%' AND data LIKE '%"is_default_profile"%'`)
	if err != nil {
		return fmt.Errorf("failed to query existing profiles: %w", err)
	}
	var idsToDelete []int64
	for delRows.Next() {
		var id int64
		if err := delRows.Scan(&id); err != nil {
			delRows.Close()
			return fmt.Errorf("failed to scan id: %w", err)
		}
		idsToDelete = append(idsToDelete, id)
	}
	delRows.Close()

	// 删除所有旧的Default Profile记录
	for _, id := range idsToDelete {
		_, err = tx.ExecContext(ctx, "DELETE FROM generic_string_objects WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("failed to delete old profile id=%d: %w", id, err)
		}
	}

	// 插入备份的Profile
	for _, profile := range backup.Profiles {
		_, err = tx.ExecContext(ctx, "INSERT INTO generic_string_objects (id, data) VALUES (?, ?)", profile.ID, profile.Data)
		if err != nil {
			return fmt.Errorf("failed to insert profile: %w", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// hasDefaultTableBackup 检查是否存在Default表备份
func hasDefaultTableBackup(backupPath string) bool {
	if backupPath == "" {
		return false
	}
	return pathExists(backupPath)
}

// loadDefaultTableBackup 加载Default表备份
func loadDefaultTableBackup(backupPath string) ([]byte, error) {
	if !pathExists(backupPath) {
		return nil, errors.New("backup file not found")
	}
	return os.ReadFile(backupPath)
}

// saveDefaultTableBackup 保存Default表备份
func saveDefaultTableBackup(backupPath string, data []byte) error {
	if backupPath == "" {
		return errors.New("backup path is empty")
	}
	// 确保目录存在
	dir := filepath.Dir(backupPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return os.WriteFile(backupPath, data, 0o644)
}
