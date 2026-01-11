package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const remoteBaseURL = "http://125.208.17.29:8088"

var errRemoteUnauthorized = errors.New("remote unauthorized")

type RemoteClient struct {
	baseURL    string
	httpClient *http.Client
}

type RemoteActivateResponse struct {
	Token        string         `json:"token"`
	ExpiresAt    int64          `json:"expires_at"`
	AccountCount int            `json:"account_count"`
	Stats        map[string]any `json:"stats"`
}

type RemoteStatusResponse struct {
	Active       bool           `json:"active"`
	ExpiresAt    int64          `json:"expires_at"`
	AccountCount int            `json:"account_count"`
	Stats        map[string]any `json:"stats"`
	ServerTime   int64          `json:"server_time"`
	DeviceID     string         `json:"device_id"`
}

type remoteAccountWire struct {
	ID              int     `json:"id"`
	Email           string  `json:"email"`
	APIKey          string  `json:"apiKey"`
	APIKeyAlt       string  `json:"api_key"`
	UID             string  `json:"uid"`
	RefreshToken    string  `json:"refreshToken"`
	RefreshTokenAlt string  `json:"refresh_token"`
	Quota           int     `json:"quota"`
	Used            int     `json:"used"`
	Status          string  `json:"status"`
	Type            string  `json:"type"`
	NextRefresh     string  `json:"nextRefreshTime"`
	NextRefreshAlt  string  `json:"next_refresh_time"`
	ErrorCount      int     `json:"errorCount"`
	LastUsed        float64 `json:"lastUsed"`
	ExperimentID    string  `json:"experimentId"`
	BindingUsed     bool    `json:"bindingUsed"`
}

type RemoteAccount struct {
	ID           int
	Email        string
	APIKey       string
	UID          string
	RefreshToken string
	Quota        int
	Used         int
	Status       string
	Type         string
	NextRefresh  string
	ErrorCount   int
	LastUsed     float64
	ExperimentID string
	BindingUsed  bool
}

func (a *RemoteAccount) UnmarshalJSON(data []byte) error {
	var w remoteAccountWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	a.ID = w.ID
	a.Email = w.Email
	a.APIKey = w.APIKey
	if a.APIKey == "" {
		a.APIKey = w.APIKeyAlt
	}
	a.UID = w.UID
	a.RefreshToken = w.RefreshToken
	if a.RefreshToken == "" {
		a.RefreshToken = w.RefreshTokenAlt
	}
	a.Quota = w.Quota
	a.Used = w.Used
	a.Status = w.Status
	a.Type = w.Type
	a.NextRefresh = w.NextRefresh
	if a.NextRefresh == "" {
		a.NextRefresh = w.NextRefreshAlt
	}
	a.ErrorCount = w.ErrorCount
	a.LastUsed = w.LastUsed
	a.ExperimentID = w.ExperimentID
	a.BindingUsed = w.BindingUsed
	return nil
}

type RemoteNotice struct {
	Message   string `json:"message"`
	Link      string `json:"link"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt int64  `json:"updatedAt"`
}

type remoteJSONResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"-"`
}

func NewRemoteClient() *RemoteClient {
	return &RemoteClient{
		baseURL: remoteBaseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy: nil, // 显式绕过代理，避免网关未启动时连接失败
			},
		},
	}
}

func (c *RemoteClient) Activate(ctx context.Context, code, deviceID string) (*RemoteActivateResponse, error) {
	payload := map[string]string{
		"code":      code,
		"device_id": deviceID,
	}
	var out RemoteActivateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/activate", deviceID, "", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteClient) Status(ctx context.Context, token, deviceID string) (*RemoteStatusResponse, error) {
	var out RemoteStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/status", deviceID, token, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteClient) Unbind(ctx context.Context, token, deviceID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/unbind", deviceID, token, map[string]any{}, nil)
}

func (c *RemoteClient) Accounts(ctx context.Context, token, deviceID string) ([]RemoteAccount, error) {
	var resp struct {
		Accounts []RemoteAccount `json:"accounts"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/accounts", deviceID, token, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Accounts, nil
}

func (c *RemoteClient) UpdateAccount(ctx context.Context, token, deviceID string, id int, payload map[string]any) error {
	if id <= 0 {
		return errors.New("invalid account id")
	}
	path := fmt.Sprintf("/api/accounts/%d", id)
	return c.doJSON(ctx, http.MethodPost, path, deviceID, token, payload, nil)
}

func (c *RemoteClient) Notice(ctx context.Context) (*RemoteNotice, error) {
	var resp struct {
		Notice RemoteNotice `json:"notice"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/notice", "", "", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Notice, nil
}

// RotateAccount 请求远程服务器换号（仅无限额度激活码可用）
func (c *RemoteClient) RotateAccount(ctx context.Context, token, deviceID string) (*RemoteAccount, error) {
	var resp struct {
		Account RemoteAccount `json:"account"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/accounts/rotate", deviceID, token, map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp.Account, nil
}

func (c *RemoteClient) doJSON(ctx context.Context, method, path, deviceID, token string, payload any, out any) error {
	if c == nil {
		return errors.New("remote client not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	url := strings.TrimRight(c.baseURL, "/") + path
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errRemoteUnauthorized
	}

	var envelope map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}

	if successRaw, ok := envelope["success"]; ok {
		if success, ok := successRaw.(bool); ok && !success {
			if msg, ok := envelope["error"].(string); ok && msg != "" {
				return errors.New(msg)
			}
			return errors.New("remote request failed")
		}
	}

	if out == nil {
		return nil
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
