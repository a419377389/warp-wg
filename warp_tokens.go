package main

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type warpAgentTokenEntry struct {
	token     string
	userID    string
	expiresAt time.Time
}

var (
	warpAgentTokenMu    sync.Mutex
	warpAgentTokenCache = map[string]warpAgentTokenEntry{}
)

func getWarpAgentToken(account Account) (string, string, error) {
	refresh := strings.TrimSpace(account.RefreshToken)
	if refresh == "" {
		apiKey := strings.TrimSpace(account.APIKey)
		if apiKey == "" {
			return "", "none", errors.New("missing refresh token")
		}
		return apiKey, "wk", nil
	}

	if token := cachedWarpAgentToken(refresh); strings.TrimSpace(token) != "" {
		return token, "jwt", nil
	}

	token, _, userID, expiresIn, err := fetchWarpIDToken(refresh)
	if err != nil || strings.TrimSpace(token) == "" {
		apiKey := strings.TrimSpace(account.APIKey)
		if apiKey != "" {
			return apiKey, "wk", err
		}
		if err == nil {
			err = errors.New("empty jwt token")
		}
		return "", "none", err
	}

	cacheWarpAgentToken(refresh, token, userID, expiresIn)
	return token, "jwt", nil
}

func cachedWarpAgentToken(refresh string) string {
	refresh = strings.TrimSpace(refresh)
	if refresh == "" {
		return ""
	}
	warpAgentTokenMu.Lock()
	entry, ok := warpAgentTokenCache[refresh]
	if !ok {
		warpAgentTokenMu.Unlock()
		return ""
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(warpAgentTokenCache, refresh)
		warpAgentTokenMu.Unlock()
		return ""
	}
	token := entry.token
	warpAgentTokenMu.Unlock()
	return token
}

func cachedWarpUserID(refresh string) string {
	refresh = strings.TrimSpace(refresh)
	if refresh == "" {
		return ""
	}
	warpAgentTokenMu.Lock()
	entry, ok := warpAgentTokenCache[refresh]
	if !ok {
		warpAgentTokenMu.Unlock()
		return ""
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(warpAgentTokenCache, refresh)
		warpAgentTokenMu.Unlock()
		return ""
	}
	userID := entry.userID
	warpAgentTokenMu.Unlock()
	return userID
}

func cacheWarpAgentToken(refresh, token, userID string, expiresIn time.Duration) {
	refresh = strings.TrimSpace(refresh)
	token = strings.TrimSpace(token)
	if refresh == "" || token == "" {
		return
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	if expiresIn > 0 {
		expiresAt = time.Now().Add(expiresIn)
		if expiresIn > 30*time.Second {
			expiresAt = expiresAt.Add(-30 * time.Second)
		}
	}
	warpAgentTokenMu.Lock()
	warpAgentTokenCache[refresh] = warpAgentTokenEntry{
		token:     token,
		userID:    userID,
		expiresAt: expiresAt,
	}
	warpAgentTokenMu.Unlock()
}
