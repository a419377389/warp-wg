package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	warpGraphQLURL = "https://app.warp.dev/graphql/v2?op=GetRequestLimitInfo"
	warpUserAgent  = "Warp Terminal/v0.2025.11.19.08.12.stable_03 (windows/x64)"
)

var warpRequestContext = map[string]any{
	"clientContext": map[string]any{
		"version": "v0.2025.11.19.08.12.stable_03",
	},
	"osContext": map[string]any{
		"category":           "Windows",
		"linuxKernelVersion": nil,
		"name":               "Windows",
		"version":            "11 (26100)",
	},
}

type WarpUsage struct {
	Quota       int
	Used        int
	NextRefresh string
	Status      string
	Type        string
	Error       string
}

func fetchWarpUsage(ctx context.Context, apiKey string) WarpUsage {
	if !isValidAPIKey(apiKey) {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: "invalid api key"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	payload := map[string]any{
		"query":         "query GetRequestLimitInfo($requestContext: RequestContext!) { user(requestContext: $requestContext) { __typename ... on UserOutput { user { requestLimitInfo { isUnlimited nextRefreshTime requestLimit requestsUsedSinceLastRefresh } } } ... on UserFacingError { error { __typename message } } } }",
		"variables":     map[string]any{"requestContext": warpRequestContext},
		"operationName": "GetRequestLimitInfo",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, warpGraphQLURL, bytes.NewReader(body))
	if err != nil {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("User-Agent", warpUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://app.warp.dev")
	req.Header.Set("Referer", "https://app.warp.dev/")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return WarpUsage{Status: "banned", Type: "BANNED", Error: fmt.Sprintf("forbidden (%d)", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: fmt.Sprintf("http %d", resp.StatusCode)}
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: err.Error()}
	}

	if errs, ok := data["errors"]; ok {
		msg := collectErrorMessages(errs)
		lowered := strings.ToLower(msg)
		if strings.Contains(lowered, "banned") || strings.Contains(lowered, "suspended") || strings.Contains(lowered, "disabled") {
			return WarpUsage{Status: "banned", Type: "BANNED", Error: msg}
		}
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: msg}
	}

	dataMap, _ := data["data"].(map[string]any)
	user, ok := dataMap["user"].(map[string]any)
	if !ok {
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: "invalid response"}
	}

	switch asString(user["__typename"]) {
	case "UserOutput":
		userInfo, _ := user["user"].(map[string]any)
		info, ok := userInfo["requestLimitInfo"].(map[string]any)
		if !ok {
			return WarpUsage{Status: "error", Type: "UNKNOWN", Error: "missing limit info"}
		}
		quota := asInt(info["requestLimit"], 0)
		used := asInt(info["requestsUsedSinceLastRefresh"], 0)
		nextRefresh := formatNextRefreshTime(asString(info["nextRefreshTime"]))
		status := "normal"
		if quota > 0 && used >= quota {
			status = "limited"
		}
		accountType := "FREE"
		if asBool(info["isUnlimited"]) {
			accountType = "UNLIMITED"
		}
		return WarpUsage{
			Quota:       quota,
			Used:        used,
			NextRefresh: nextRefresh,
			Status:      status,
			Type:        accountType,
		}
	case "UserFacingError":
		errorInfo, _ := user["error"].(map[string]any)
		errorType := strings.ToLower(asString(errorInfo["__typename"]))
		message := firstNonEmpty(asString(errorInfo["message"]), errorType, "unknown error")
		if strings.Contains(errorType, "banned") || strings.Contains(errorType, "suspended") || strings.Contains(errorType, "disabled") {
			return WarpUsage{Status: "banned", Type: "BANNED", Error: message}
		}
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: message}
	default:
		return WarpUsage{Status: "error", Type: "UNKNOWN", Error: "unknown response"}
	}
}

func isValidAPIKey(apiKey string) bool {
	return apiKey != "" && strings.HasPrefix(apiKey, "wk-1.")
}

func collectErrorMessages(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	messages := make([]string, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			msg := asString(obj["message"])
			if msg != "" {
				messages = append(messages, msg)
			}
		}
	}
	return strings.Join(messages, ", ")
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func asInt(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := strconv.ParseBool(v)
		return parsed
	case float64:
		return v != 0
	default:
		return false
	}
}

func formatNextRefreshTime(value string) string {
	if value == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	if len(value) >= 19 {
		return strings.ReplaceAll(value[:19], "T", " ")
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
