package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ruleEntry struct {
	Name    string
	Content string
}

func loadSelectedRules(paths Paths, log *Logger) []string {
	if paths.DataDir == "" {
		return nil
	}

	injectConfigPath := filepath.Join(paths.DataDir, "inject_rules_enabled.json")
	if raw, err := os.ReadFile(injectConfigPath); err == nil && len(raw) > 0 {
		var cfg map[string]any
		if json.Unmarshal(raw, &cfg) == nil {
			if enabled, ok := cfg["enabled"].(bool); ok && !enabled {
				return nil
			}
		}
	}

	selectedPath := filepath.Join(paths.DataDir, "selected_rule_ids.json")
	raw, err := os.ReadFile(selectedPath)
	if err != nil || len(raw) == 0 {
		return nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	rawIDs, ok := payload["ruleIds"].([]any)
	if !ok || len(rawIDs) == 0 {
		return nil
	}

	ruleIDs := make([]string, 0, len(rawIDs))
	for _, id := range rawIDs {
		switch v := id.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				ruleIDs = append(ruleIDs, v)
			}
		case float64:
			ruleIDs = append(ruleIDs, strconv.FormatInt(int64(v), 10))
		case int:
			ruleIDs = append(ruleIDs, strconv.Itoa(v))
		default:
		}
	}

	if len(ruleIDs) == 0 {
		return nil
	}

	configRules := loadRulesFromConfig(paths, log)
	missing := make([]string, 0)
	for _, id := range ruleIDs {
		if _, ok := configRules[id]; !ok {
			missing = append(missing, id)
		}
	}

	dbRules := map[string]ruleEntry{}
	if len(missing) > 0 {
		dbRules = loadRulesFromWarpDB(missing, log)
	}

	merged := map[string]ruleEntry{}
	for id, entry := range dbRules {
		merged[id] = entry
	}
	for id, entry := range configRules {
		merged[id] = entry
	}

	results := make([]string, 0)
	for _, id := range ruleIDs {
		entry, ok := merged[id]
		if !ok {
			continue
		}
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = "Unnamed Rule"
		}
		results = append(results, formatRuleText(name, content))
	}
	return results
}

func loadRulesFromConfig(paths Paths, log *Logger) map[string]ruleEntry {
	rules := map[string]ruleEntry{}
	if paths.DataDir == "" {
		return rules
	}

	path := filepath.Join(paths.DataDir, "warp_rules_config.json")
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return rules
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		if log != nil {
			log.Warn("rules config parse failed: " + err.Error())
		}
		return rules
	}

	items, ok := payload["rules"].([]any)
	if !ok {
		return rules
	}
	for _, item := range items {
		entryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		idVal, ok := entryMap["id"]
		if !ok {
			continue
		}
		id := ""
		switch v := idVal.(type) {
		case string:
			id = v
		case float64:
			id = strconv.FormatInt(int64(v), 10)
		}
		if id == "" {
			continue
		}
		name, _ := entryMap["name"].(string)
		content, _ := entryMap["content"].(string)
		rules[id] = ruleEntry{Name: name, Content: content}
	}
	return rules
}

func loadRulesFromWarpDB(ruleIDs []string, log *Logger) map[string]ruleEntry {
	rules := map[string]ruleEntry{}
	if len(ruleIDs) == 0 {
		return rules
	}
	dbPath := warpSQLiteDB()
	if dbPath == "" || !pathExists(dbPath) {
		return rules
	}

	ids := make([]int, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		if parsed, err := strconv.Atoi(id); err == nil {
			ids = append(ids, parsed)
		}
	}
	if len(ids) == 0 {
		return rules
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		if log != nil {
			log.Warn("warp db open failed: " + err.Error())
		}
		return rules
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	query := "SELECT id, data FROM generic_string_objects WHERE id IN (" + placeholders + ")"
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if log != nil {
			log.Warn("warp db query failed: " + err.Error())
		}
		return rules
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		mem, ok := payload["memory"].(map[string]any)
		if !ok {
			continue
		}
		content, _ := mem["content"].(string)
		name, _ := mem["name"].(string)
		if content == "" {
			continue
		}
		rules[strconv.Itoa(id)] = ruleEntry{Name: name, Content: content}
	}
	return rules
}

func formatRuleText(name, content string) string {
	ruleName := "\u89c4\u5219\u540d\uff1a"
	ruleContent := "\u89c4\u5219\u5185\u5bb9\uff1a"
	return ruleName + name + "\n" + ruleContent + content
}

func buildRulesBlock(selected []string) string {
	if len(selected) == 0 {
		return ""
	}
	lines := make([]string, 0, len(selected)+6)
	lines = append(lines, "[[WARP_AI_RULES_BEGIN]]")
	lines = append(lines, "\u4ee5\u4e0b\u89c4\u5219\u5728\u672c\u6b21\u5bf9\u8bdd\u4e2d\u5fc5\u987b\u4f18\u5148\u9075\u5b88\uff08\u6765\u81eaWarp AI Rules \u52fe\u9009\u9879\uff09\uff1a")
	lines = append(lines, "")
	for idx, rule := range selected {
		lines = append(lines, "\u2014\u2014\u2014 \u89c4\u5219 "+strconv.Itoa(idx+1)+" \u2014\u2014\u2014")
		lines = append(lines, rule)
		lines = append(lines, "")
	}
	lines = append(lines, "[[WARP_AI_RULES_END]]")
	return strings.Join(lines, "\n")
}

func buildProtobufRulesText(selected []string) string {
	if len(selected) == 0 {
		return ""
	}
	separator := strings.Repeat("=", 50)
	header := "\u3010\u91cd\u8981\u3011\u4ee5\u4e0b\u89c4\u5219\u5fc5\u987b\u9075\u5b88\uff1a"
	lines := make([]string, 0, len(selected)+4)
	lines = append(lines, "")
	lines = append(lines, "")
	lines = append(lines, separator)
	lines = append(lines, header)
	lines = append(lines, "")
	for idx, rule := range selected {
		lines = append(lines, strconv.Itoa(idx+1)+". "+rule)
		lines = append(lines, "")
	}
	lines = append(lines, separator)
	return strings.Join(lines, "\n")
}
