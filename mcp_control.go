package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)
const mcpGlobalBackupKey = "global_mcp"
const mcpGlobalRestoreDelay = 3 * time.Second

func mcpServerInstallationsCount() int {
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return -1
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return -1
	}
	defer db.Close()
	row := db.QueryRow("SELECT COUNT(*) FROM mcp_server_installations")
	var count int
	if err := row.Scan(&count); err != nil {
		return -1
	}
	return count
}

// MCPServer represents an MCP server configuration
type MCPServer struct {
	ID              string         `json:"id"`
	UUID            string         `json:"uuid"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Template        map[string]any `json:"template"`
	TemplateVersion string         `json:"template_version"`
	VariableValues  map[string]any `json:"variable_values"`
	RestoreRunning  bool           `json:"restore_running"`
	LastModified    string         `json:"last_modified"`
}

// MCPBackup represents a backup of MCP configuration for an account
type MCPBackup struct {
	AccountID              string           `json:"account_id"`
	AccountEmail           string           `json:"account_email"`
	BackupTime             string           `json:"backup_time"`
	MCPServerInstallations []MCPInstallRow  `json:"mcp_server_installations"`
	MCPEnvironmentVars     []MCPEnvVarRow   `json:"mcp_environment_variables"`
	ActiveMCPServers       []MCPActiveRow   `json:"active_mcp_servers"`
	MCPAllowlist           []string         `json:"mcp_allowlist,omitempty"`
}

// MCPInstallRow represents a row from mcp_server_installations table
type MCPInstallRow struct {
	ID                   string `json:"id"`
	TemplatableMCPServer string `json:"templatable_mcp_server"`
	TemplateVersionTS    string `json:"template_version_ts"`
	VariableValues       string `json:"variable_values"`
	RestoreRunning       bool   `json:"restore_running"`
	LastModifiedAt       string `json:"last_modified_at"`
}

// MCPEnvVarRow represents a row from mcp_environment_variables table
type MCPEnvVarRow struct {
	MCPServerUUID        string `json:"mcp_server_uuid"`
	EnvironmentVariables string `json:"environment_variables"`
}

// MCPActiveRow represents a row from active_mcp_servers table
type MCPActiveRow struct {
	ID            int    `json:"id"`
	MCPServerUUID string `json:"mcp_server_uuid"`
}

// MCPBackupInfo contains summary info about an MCP backup
type MCPBackupInfo struct {
	AccountID    string `json:"account_id"`
	AccountEmail string `json:"account_email"`
	BackupTime   string `json:"backup_time"`
	ServerCount  int    `json:"server_count"`
	ActiveCount  int    `json:"active_count"`
}

// mcpBackupDir returns the directory for MCP backups
func mcpBackupDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".warp-gateway", "mcp_backups")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// mcpBackupPath returns the backup file path for an account
func mcpBackupPath(accountID string) string {
	return filepath.Join(mcpBackupDir(), accountID+"_mcp.json")
}

// getMCPServers reads all MCP server configurations from Warp database
func getMCPServers() ([]MCPServer, error) {
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return nil, errors.New("warp database not found")
	}

	// 使用WAL模式连接，支持在线热读取
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, templatable_mcp_server, template_version_ts, variable_values, restore_running, last_modified_at FROM mcp_server_installations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []MCPServer
	for rows.Next() {
		var id, configJSON, templateVersion, variableValues, lastModified string
		var restoreRunning bool

		if err := rows.Scan(&id, &configJSON, &templateVersion, &variableValues, &restoreRunning, &lastModified); err != nil {
			continue
		}

		var config map[string]any
		if json.Unmarshal([]byte(configJSON), &config) != nil {
			continue
		}

		var varValues map[string]any
		_ = json.Unmarshal([]byte(variableValues), &varValues)

		server := MCPServer{
			ID:              id,
			UUID:            asString(config["uuid"]),
			Name:            asString(config["name"]),
			Description:     asString(config["description"]),
			Template:        asMap(config["template"]),
			TemplateVersion: templateVersion,
			VariableValues:  varValues,
			RestoreRunning:  restoreRunning,
			LastModified:    lastModified,
		}
		servers = append(servers, server)
	}

	return servers, nil
}

// getActiveMCPServers returns the list of active MCP server UUIDs
func getActiveMCPServers() ([]string, error) {
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return nil, errors.New("warp database not found")
	}

	// 使用WAL模式连接，支持在线热读取
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT mcp_server_uuid FROM active_mcp_servers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uuids []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			continue
		}
		uuids = append(uuids, uuid)
	}

	return uuids, nil
}

// backupMCPConfig backs up current MCP configuration for an account
func backupMCPConfig(accountID, accountEmail string) error {
	if accountID == "" {
		return errors.New("account ID is empty")
	}

	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return errors.New("warp database not found")
	}

	// 使用WAL模式连接，支持在线热读取
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return err
	}
	defer db.Close()

	backup := MCPBackup{
		AccountID:    accountID,
		AccountEmail: accountEmail,
		BackupTime:   time.Now().UTC().Format(time.RFC3339),
	}

	// Backup mcp_server_installations
	rows, err := db.Query("SELECT id, templatable_mcp_server, template_version_ts, variable_values, restore_running, last_modified_at FROM mcp_server_installations")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row MCPInstallRow
			var restoreRunning int
			if err := rows.Scan(&row.ID, &row.TemplatableMCPServer, &row.TemplateVersionTS, &row.VariableValues, &restoreRunning, &row.LastModifiedAt); err != nil {
				continue
			}
			// 强制设置为 true，确保恢复时 restore_running = 1
			row.RestoreRunning = true
			backup.MCPServerInstallations = append(backup.MCPServerInstallations, row)
		}
	}

	// Backup mcp_environment_variables
	rows2, err := db.Query("SELECT mcp_server_uuid, environment_variables FROM mcp_environment_variables")
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var row MCPEnvVarRow
			var uuidBytes []byte
			if err := rows2.Scan(&uuidBytes, &row.EnvironmentVariables); err != nil {
				continue
			}
			row.MCPServerUUID = string(uuidBytes)
			backup.MCPEnvironmentVars = append(backup.MCPEnvironmentVars, row)
		}
	}

	// Backup active_mcp_servers
	rows3, err := db.Query("SELECT id, mcp_server_uuid FROM active_mcp_servers")
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var row MCPActiveRow
			if err := rows3.Scan(&row.ID, &row.MCPServerUUID); err != nil {
				continue
			}
			backup.ActiveMCPServers = append(backup.ActiveMCPServers, row)
		}
	}

	// Backup mcp_allowlist from generic_string_objects (agent profiles)
	backup.MCPAllowlist = getMCPAllowlistFromProfiles(db)

	// Skip saving if no MCP data found
	if len(backup.MCPServerInstallations) == 0 {
		return nil
	}

	// Save backup to file
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(mcpBackupPath(accountID), data, 0o644)
}

// restoreMCPConfig restores MCP configuration from backup for an account
func restoreMCPConfig(accountID string) error {
	if accountID == "" {
		return errors.New("account ID is empty")
	}

	backupFile := mcpBackupPath(accountID)
	if !pathExists(backupFile) {
		return errors.New("no MCP backup found for account")
	}

	data, err := os.ReadFile(backupFile)
	if err != nil {
		return err
	}

	var backup MCPBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return err
	}

	// Skip restore if backup has no MCP data
	if len(backup.MCPServerInstallations) == 0 {
		return nil
	}

	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return errors.New("warp database not found")
	}

	// 使用WAL模式连接，支持在线热更新
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return err
	}
	defer db.Close()

	// 设置连接池参数，减少锁竞争
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Clear existing MCP data
	_, _ = tx.Exec("DELETE FROM mcp_server_installations")
	_, _ = tx.Exec("DELETE FROM mcp_environment_variables")
	_, _ = tx.Exec("DELETE FROM active_mcp_servers")

	// Restore mcp_server_installations
	for _, row := range backup.MCPServerInstallations {
		// 强制设置 restore_running = 1，触发 Warp 客户端自动安装 MCP 服务器
		// 这样 Warp 会识别这些 MCP 为"需要恢复运行"状态，自动完成安装流程
		restoreRunning := 1
		_, err = tx.Exec(
			"INSERT INTO mcp_server_installations (id, templatable_mcp_server, template_version_ts, variable_values, restore_running, last_modified_at) VALUES (?, ?, ?, ?, ?, ?)",
			row.ID, row.TemplatableMCPServer, row.TemplateVersionTS, row.VariableValues, restoreRunning, row.LastModifiedAt,
		)
		if err != nil {
			return err
		}
	}

	// Restore mcp_environment_variables
	for _, row := range backup.MCPEnvironmentVars {
		_, err = tx.Exec(
			"INSERT INTO mcp_environment_variables (mcp_server_uuid, environment_variables) VALUES (?, ?)",
			[]byte(row.MCPServerUUID), row.EnvironmentVariables,
		)
		if err != nil {
			return err
		}
	}

	// 不恢复 active_mcp_servers 表，让 Warp 客户端自己处理
	// 这样可以避免与 Warp 的恢复机制冲突

	if err = tx.Commit(); err != nil {
		return err
	}

	// Restore mcp_allowlist to agent profiles in generic_string_objects
	// This is the actual mechanism Warp uses to track enabled MCP servers
	allowlist := backup.MCPAllowlist
	if len(allowlist) == 0 {
		// Fallback: use UUIDs from installations
		seen := map[string]struct{}{}
		for _, row := range backup.MCPServerInstallations {
			uuid := mcpServerUUIDFromTemplate(row.TemplatableMCPServer)
			if uuid == "" {
				continue
			}
			if _, ok := seen[uuid]; ok {
				continue
			}
			seen[uuid] = struct{}{}
			allowlist = append(allowlist, uuid)
		}
	}
	if len(allowlist) > 0 {
		if err := setMCPAllowlistToProfiles(db, allowlist); err != nil {
			return err
		}
	}

	return nil
}

// hasMCPBackup checks if an account has an MCP backup
func hasMCPBackup(accountID string) bool {
	if accountID == "" {
		return false
	}
	return pathExists(mcpBackupPath(accountID))
}

// getMCPBackupInfo returns summary info about an account's MCP backup
func getMCPBackupInfo(accountID string) (*MCPBackupInfo, error) {
	if accountID == "" {
		return nil, errors.New("account ID is empty")
	}

	backupFile := mcpBackupPath(accountID)
	if !pathExists(backupFile) {
		return nil, errors.New("no MCP backup found")
	}

	data, err := os.ReadFile(backupFile)
	if err != nil {
		return nil, err
	}

	var backup MCPBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, err
	}

	return &MCPBackupInfo{
		AccountID:    backup.AccountID,
		AccountEmail: backup.AccountEmail,
		BackupTime:   backup.BackupTime,
		ServerCount:  len(backup.MCPServerInstallations),
		ActiveCount:  len(backup.ActiveMCPServers),
	}, nil
}

// listMCPBackups returns all MCP backups
func listMCPBackups() ([]MCPBackupInfo, error) {
	dir := mcpBackupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var backups []MCPBackupInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var backup MCPBackup
		if json.Unmarshal(data, &backup) != nil {
			continue
		}

		backups = append(backups, MCPBackupInfo{
			AccountID:    backup.AccountID,
			AccountEmail: backup.AccountEmail,
			BackupTime:   backup.BackupTime,
			ServerCount:  len(backup.MCPServerInstallations),
			ActiveCount:  len(backup.ActiveMCPServers),
		})
	}

	return backups, nil
}

// deleteMCPBackup deletes an account's MCP backup
func deleteMCPBackup(accountID string) error {
	if accountID == "" {
		return errors.New("account ID is empty")
	}
	backupFile := mcpBackupPath(accountID)
	if !pathExists(backupFile) {
		return nil
	}
	return os.Remove(backupFile)
}

// switchAccountWithMCP performs account switch with MCP config sync
// It backs up current account's MCP config and restores target account's MCP config
func switchAccountWithMCP(currentAccount, targetAccount *Account, log *Logger) error {
	// Backup global MCP config before switching accounts
	accountEmail := ""
	if currentAccount != nil {
		accountEmail = currentAccount.Email
	}
	currentCount := mcpServerInstallationsCount()
	if currentCount <= 0 && hasMCPBackup(mcpGlobalBackupKey) {
		if log != nil {
			log.Info("[MCP] skip backup: no MCP servers in database")
		}
	} else {
		if log != nil {
			log.Info("[MCP] backing up global MCP config")
		}
		if err := backupMCPConfig(mcpGlobalBackupKey, accountEmail); err != nil {
			if log != nil {
				log.Warn("[MCP] backup failed: " + err.Error())
			}
			// Don't fail the switch, just log the error
		}
	}

	// Update Warp credentials for target account
	if err := updateWarpCredentials(*targetAccount); err != nil {
		return err
	}

	// Restore global MCP config after switching
	if hasMCPBackup(mcpGlobalBackupKey) {
		if log != nil {
			log.Info("[MCP] restoring global MCP config")
		}
		if err := restoreMCPConfig(mcpGlobalBackupKey); err != nil {
			if log != nil {
				log.Warn("[MCP] restore failed: " + err.Error())
			}
			// Don't fail the switch, just log the error
		}
	} else if log != nil {
		log.Info("[MCP] no global MCP backup found")
	}
	scheduleGlobalMCPRestore(mcpGlobalRestoreDelay, log)

	return nil
}

func scheduleGlobalMCPRestore(delay time.Duration, log *Logger) {
	if delay <= 0 {
		delay = 2 * time.Second
	}
	delays := []time.Duration{delay, 10 * time.Second, 30 * time.Second, 60 * time.Second, 120 * time.Second}
	expected := -1
	if info, err := getMCPBackupInfo(mcpGlobalBackupKey); err == nil && info != nil {
		expected = info.ServerCount
	}
	go func() {
		for i, wait := range delays {
			time.Sleep(wait)
			if !hasMCPBackup(mcpGlobalBackupKey) {
				return
			}
			current := mcpServerInstallationsCount()
			if expected > 0 && current >= expected {
				return
			}
			if log != nil {
				log.Info("[MCP] restoring global MCP config (delayed #" + strconv.Itoa(i+1) + ")")
			}
			if err := restoreMCPConfig(mcpGlobalBackupKey); err != nil {
				if log != nil {
					log.Warn("[MCP] delayed restore failed: " + err.Error())
				}
				continue
			}
			if expected > 0 && mcpServerInstallationsCount() >= expected {
				return
			}
		}
	}()
}

// accountIdentifier generates a unique identifier for an account
// Uses email as the primary identifier, falling back to API key hash
func accountIdentifier(acc *Account) string {
	if acc == nil {
		return ""
	}
	if acc.Email != "" {
		// Use email as identifier (sanitized)
		return sanitizeFilename(acc.Email)
	}
	if acc.APIKey != "" {
		// Use first 16 chars of API key as fallback
		if len(acc.APIKey) > 16 {
			return "apikey_" + acc.APIKey[:16]
		}
		return "apikey_" + acc.APIKey
	}
	return ""
}

// sanitizeFilename removes characters not allowed in filenames
func sanitizeFilename(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '@' {
			result = append(result, c)
		}
	}
	return string(result)
}

// asMap converts interface{} to map[string]any
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func mcpServerUUIDFromTemplate(raw string) string {
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	return asString(payload["uuid"])
}

// getMCPAllowlistFromProfiles reads mcp_allowlist from agent profiles in generic_string_objects
func getMCPAllowlistFromProfiles(db *sql.DB) []string {
	rows, err := db.Query("SELECT id, data FROM generic_string_objects")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var allowlist []string
	seen := map[string]struct{}{}

	for rows.Next() {
		var id int
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			continue
		}
		if data == "" {
			continue
		}

		var profile map[string]any
		if json.Unmarshal([]byte(data), &profile) != nil {
			continue
		}

		// Check if this is an agent profile (has mcp_allowlist field)
		if mcpList, ok := profile["mcp_allowlist"]; ok {
			if arr, ok := mcpList.([]any); ok {
				for _, v := range arr {
					if uuid, ok := v.(string); ok && uuid != "" {
						if _, exists := seen[uuid]; !exists {
							seen[uuid] = struct{}{}
							allowlist = append(allowlist, uuid)
						}
					}
				}
			}
		}
	}

	return allowlist
}

// setMCPAllowlistToProfiles updates mcp_allowlist in all agent profiles in generic_string_objects
// If no agent profile exists, creates a default one
func setMCPAllowlistToProfiles(db *sql.DB, allowlist []string) error {
	rows, err := db.Query("SELECT id, data FROM generic_string_objects")
	if err != nil {
		return err
	}
	defer rows.Close()

	type profileRow struct {
		id   int
		data string
	}
	var profiles []profileRow
	var agentProfileFound bool

	for rows.Next() {
		var id int
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			continue
		}
		if data == "" {
			continue
		}
		profiles = append(profiles, profileRow{id: id, data: data})
	}
	rows.Close()

	for _, p := range profiles {
		var profile map[string]any
		if json.Unmarshal([]byte(p.data), &profile) != nil {
			continue
		}

		// Check if this is an agent profile (has mcp_allowlist or mcp_permissions field)
		if _, ok := profile["mcp_allowlist"]; !ok {
			if _, ok := profile["mcp_permissions"]; !ok {
				continue
			}
		}

		agentProfileFound = true

		// Update mcp_allowlist
		profile["mcp_allowlist"] = allowlist

		// Serialize back
		newData, err := json.Marshal(profile)
		if err != nil {
			continue
		}

		// Update in database
		_, err = db.Exec("UPDATE generic_string_objects SET data = ? WHERE id = ?", string(newData), p.id)
		if err != nil {
			return err
		}
	}

	// If no agent profile found, create a default one with the MCP allowlist
	if !agentProfileFound && len(allowlist) > 0 {
		defaultProfile := map[string]any{
			"name":                        "Default",
			"is_default_profile":          true,
			"apply_code_diffs":            "AgentDecides",
			"read_files":                  "AgentDecides",
			"execute_commands":            "AlwaysAsk",
			"write_to_pty":                "AlwaysAsk",
			"mcp_permissions":             "AgentDecides",
			"command_denylist":            []any{},
			"command_allowlist":           []any{},
			"directory_allowlist":         []any{},
			"mcp_allowlist":               allowlist,
			"mcp_denylist":                []any{},
			"base_model":                  nil,
			"coding_model":                nil,
			"cli_agent_model":             nil,
			"autosync_plans_to_warp_drive": true,
			"web_search_enabled":          true,
		}
		newData, err := json.Marshal(defaultProfile)
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO generic_string_objects (data) VALUES (?)", string(newData))
		if err != nil {
			return err
		}
	}

	return nil
}
