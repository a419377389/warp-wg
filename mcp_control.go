package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)
const mcpGlobalBackupKey = "global_mcp"
const mcpGlobalRestoreDelay = 3 * time.Second

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

	db, err := sql.Open("sqlite", dbPath)
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

	db, err := sql.Open("sqlite", dbPath)
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

	db, err := sql.Open("sqlite", dbPath)
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
			row.RestoreRunning = restoreRunning != 0
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

	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return errors.New("warp database not found")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

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
		restoreRunning := 0
		if row.RestoreRunning {
			restoreRunning = 1
		}
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

	// Restore active_mcp_servers
	for _, row := range backup.ActiveMCPServers {
		_, err = tx.Exec(
			"INSERT INTO active_mcp_servers (id, mcp_server_uuid) VALUES (?, ?)",
			row.ID, row.MCPServerUUID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
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
	if log != nil {
		log.Info("[MCP] backing up global MCP config")
	}
	if err := backupMCPConfig(mcpGlobalBackupKey, accountEmail); err != nil {
		if log != nil {
			log.Warn("[MCP] backup failed: " + err.Error())
		}
		// Don't fail the switch, just log the error
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
	time.AfterFunc(delay, func() {
		if !hasMCPBackup(mcpGlobalBackupKey) {
			return
		}
		if log != nil {
			log.Info("[MCP] restoring global MCP config (delayed)")
		}
		if err := restoreMCPConfig(mcpGlobalBackupKey); err != nil {
			if log != nil {
				log.Warn("[MCP] delayed restore failed: " + err.Error())
			}
		}
	})
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
