package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/andybalholm/brotli"
	"github.com/lqqyt2423/go-mitmproxy/addon"
	"github.com/lqqyt2423/go-mitmproxy/flow"
)

var (
	bannedPatterns = []string{
		"account is blocked",
		"account is banned",
		"account has been blocked",
		"account has been banned",
		"account suspended",
		"user is blocked",
		"user is banned",
		"access denied",
	}
	blockedHosts = []string{
		"dataplane.rudderstack.com",
		"rudderstack.com",
		"warpianwzlfqdq.dataplane.rudderstack.com",
	}
	rulesBlockRe = regexp.MustCompile(`(?s)\Q[[WARP_AI_RULES_BEGIN]]\E.*?\Q[[WARP_AI_RULES_END]]\E`)
)

const (
	warpProxyHost            = "app.warp.dev"
	warpProxyTokenPath       = "/proxy/token"
	defaultWarpClientVersion = "v0.2025.12.17.17.17.stable_02"
)

type WarpAddon struct {
	addon.Base
	app      *App
	accounts *GatewayAccountManager

	mu                 sync.Mutex
	lastFileMtime      int64
	lastVirtualUsed    int
	lastVirtualUsedSet bool
	lastCurrentEmail   string
	accountsSwitched   int
}

func NewWarpAddon(app *App, accounts *GatewayAccountManager) *WarpAddon {
	return &WarpAddon{
		app:      app,
		accounts: accounts,
	}
}

func (w *WarpAddon) Request(f *flow.Flow) {
	if f == nil || f.Request == nil || f.Request.URL == nil {
		return
	}

	host := f.Request.URL.Hostname()
	if w.app != nil && w.app.log != nil {
		if f.Request.Body != nil && len(f.Request.Body) > 0 {
			w.logDebug("request body size: " + strconv.Itoa(len(f.Request.Body)) + " bytes")
		}
		if contentType := strings.TrimSpace(f.Request.Header.Get("Content-Type")); contentType != "" {
			w.logDebug("content-type=" + contentType)
		}
	}

	if isGatewaySkipRequest(f.Request) {
		return
	}

	w.captureFirebaseKey(f.Request)

	rewroteToken := rewriteSecureTokenRequest(f)
	if rewroteToken && w.app != nil && w.app.log != nil {
		w.app.log.Info("securetoken redirected to app.warp.dev/proxy/token")
	}

	w.reloadAccountsIfChanged()
	if isWarpProxyTokenRequest(f.Request) {
		replaced, reason := w.replaceRefreshToken(f)
		if !replaced && reason != "" && w.app != nil && w.app.log != nil {
			w.app.log.Info("proxy token refresh skipped: " + reason)
		}
		if rewroteToken {
			return
		}
	}

	if rewroteToken {
		return
	}

	if hostBlocked(host) {
		f.Response = &flow.Response{
			StatusCode: http.StatusNoContent,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       []byte{},
		}
		return
	}

	if strings.HasSuffix(host, "warp.dev") {
		w.replaceAPIKey(f)
		w.injectRulesToRequest(f)
		w.normalizeAIHeaders(f)
		w.logAIRequestSummary(f)
		w.logAIHeaderDetails(f)
	}
}

func (w *WarpAddon) Requestheaders(f *flow.Flow) {
	if w == nil || w.app == nil || w.app.log == nil || f == nil || f.Request == nil || f.Request.URL == nil {
		return
	}
	remote, proto := requestRemoteProto(f.Request)
	if remote == "" {
		remote = "unknown"
	}
	if proto == "" {
		proto = "HTTP/1.1"
	}
	url := f.Request.URL.String()
	w.app.log.Info("mitmdump: " + remote + ": " + f.Request.Method + " " + url + " " + proto)
}

func (w *WarpAddon) logDebug(msg string) {
	if w == nil || w.app == nil || w.app.log == nil {
		return
	}
	w.app.log.Info("[DEBUG] " + msg)
}

func headerValue(header http.Header, keys ...string) string {
	if header == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func previewAuthHeader(value string) string {
	if token, ok := bearerToken(value); ok {
		if token == "" {
			return "bearer"
		}
		return "bearer:" + apiKeyPreview(token)
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "non-bearer"
}

func apiKeyPreview(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func requestRemoteProto(req *flow.Request) (string, string) {
	if req == nil {
		return "", ""
	}
	remote := ""
	proto := req.Proto
	if raw := req.Raw(); raw != nil {
		if remote == "" {
			remote = raw.RemoteAddr
		}
		if proto == "" {
			proto = raw.Proto
		}
	}
	return remote, proto
}

func responseContentSize(resp *flow.Response) int {
	if resp == nil {
		return 0
	}
	if resp.Body != nil {
		return len(resp.Body)
	}
	if resp.Header != nil {
		if raw := strings.TrimSpace(resp.Header.Get("Content-Length")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				return n
			}
		}
	}
	return 0
}

func (w *WarpAddon) logMitmResponseLine(f *flow.Flow) {
	if w == nil || w.app == nil || w.app.log == nil || f == nil || f.Request == nil || f.Response == nil {
		return
	}
	_, proto := requestRemoteProto(f.Request)
	if proto == "" {
		proto = "HTTP/1.1"
	}
	status := f.Response.StatusCode
	reason := http.StatusText(status)
	size := responseContentSize(f.Response)
	w.app.log.Info("mitmdump: << " + proto + " " + strconv.Itoa(status) + " " + reason + " " + strconv.Itoa(size) + "b")
}

func (w *WarpAddon) captureFirebaseKey(req *flow.Request) {
	if w == nil || w.app == nil || req == nil || req.URL == nil {
		return
	}
	host := strings.ToLower(requestHostname(req))
	path := strings.ToLower(req.URL.Path)
	if host != "securetoken.googleapis.com" && !(host == warpProxyHost && strings.HasPrefix(path, warpProxyTokenPath)) {
		return
	}
	key := req.URL.Query().Get("key")
	if key == "" {
		return
	}
	_ = w.app.updateWarpFirebaseAPIKey(key, "request")
}

func (w *WarpAddon) logAIRequestSummary(f *flow.Flow) {
	if w == nil || w.app == nil || w.app.log == nil || f == nil || f.Request == nil || f.Request.URL == nil {
		return
	}
	if !strings.HasSuffix(f.Request.URL.Hostname(), "warp.dev") {
		return
	}
	path := f.Request.URL.Path
	if !isAgentEndpoint(path) {
		return
	}

	auth := f.Request.Header.Get("Authorization")
	authPrefix := "none"
	if token, ok := bearerToken(auth); ok {
		if len(token) >= 3 {
			authPrefix = token[:3]
		} else {
			authPrefix = token
		}
	} else if strings.TrimSpace(auth) != "" {
		authPrefix = "non-bearer"
	}

	hasCookie := "false"
	if cookie := f.Request.Header.Get("Cookie"); strings.TrimSpace(cookie) != "" {
		hasCookie = "true"
	}

	bodyLen := 0
	if f.Request.Body != nil {
		bodyLen = len(f.Request.Body)
	}

	encoding := strings.ToLower(strings.TrimSpace(f.Request.Header.Get("Content-Encoding")))
	if encoding == "" {
		encoding = "none"
	}

	msg := "ai request: auth_prefix=" + authPrefix +
		" cookie=" + hasCookie +
		" encoding=" + encoding +
		" content_type=" + strings.ToLower(f.Request.Header.Get("Content-Type")) +
		" body_len=" + strconv.Itoa(bodyLen)
	w.app.log.Info(msg)
}

func (w *WarpAddon) logAIHeaderDetails(f *flow.Flow) {
	if w == nil || w.app == nil || w.app.log == nil || f == nil || f.Request == nil || f.Request.URL == nil {
		return
	}
	if !isAgentEndpoint(f.Request.URL.Path) {
		return
	}
	header := f.Request.Header
	if header == nil {
		return
	}

	authPreview := previewAuthHeader(header.Get("Authorization"))
	xapi := headerValue(header, "X-API-Key", "x-api-key")
	xapiPreview := ""
	if xapi != "" {
		xapiPreview = apiKeyPreview(xapi)
	}
	expHeader := headerValue(header, "x-warp-experiment-id", "X-Warp-Experiment-Id")
	expPreview := "none"
	if expHeader != "" {
		expPreview = truncateForLog(expHeader, 32)
	}
	cookie := header.Get("Cookie")

	parts := []string{
		"cookie_len=" + strconv.Itoa(len(cookie)),
		"exp_id=" + expPreview,
	}
	if authPreview != "" {
		parts = append(parts, "auth="+authPreview)
	}
	if xapiPreview != "" {
		parts = append(parts, "x-api-key="+xapiPreview)
	}

	if ua := strings.TrimSpace(header.Get("User-Agent")); ua != "" {
		parts = append(parts, "ua="+truncateForLog(ua, 80))
	}
	if clientVer := strings.TrimSpace(header.Get("x-warp-client-version")); clientVer != "" {
		parts = append(parts, "client_ver="+truncateForLog(clientVer, 40))
	}
	if osName := strings.TrimSpace(header.Get("x-warp-os-name")); osName != "" {
		parts = append(parts, "os_name="+truncateForLog(osName, 20))
	}
	if osVer := strings.TrimSpace(header.Get("x-warp-os-version")); osVer != "" {
		parts = append(parts, "os_ver="+truncateForLog(osVer, 20))
	}
	if deviceID := strings.TrimSpace(header.Get("x-warp-device-id")); deviceID != "" {
		parts = append(parts, "device_id="+truncateForLog(deviceID, 40))
	}
	if sessionID := strings.TrimSpace(header.Get("x-warp-session-id")); sessionID != "" {
		parts = append(parts, "session_id="+truncateForLog(sessionID, 40))
	}

	w.logDebug("ai headers: " + strings.Join(parts, " "))
}

func (w *WarpAddon) Response(f *flow.Flow) {
	if f == nil || f.Request == nil || f.Response == nil || f.Request.URL == nil {
		return
	}

	w.logMitmResponseLine(f)

	host := f.Request.URL.Hostname()
	if !strings.HasSuffix(host, "warp.dev") {
		return
	}

	path := f.Request.URL.Path
	if isAgentEndpoint(path) && w.app != nil && w.app.log != nil {
		w.app.log.Info("ai response: status=" + strconv.Itoa(f.Response.StatusCode))
		if f.Response.StatusCode >= http.StatusBadRequest {
			f.Response.ReplaceToDecodedBody()
			if msg := responseMessage(f.Response.Body); msg != "" {
				w.logDebug("ai response message: " + truncateForLog(msg, 240))
			}
		}
	}

	if w.checkBannedResponse(f) {
		return
	}
	if w.checkMultiAgentResponse(f) {
		return
	}
	if w.checkLimitedResponse(f) {
		return
	}

	if strings.Contains(path, "GetRequestLimitInfo") || strings.Contains(path, "graphql") || strings.Contains(strings.ToLower(path), "limit") {
		w.modifyQuotaResponse(f)
	}
}

func (w *WarpAddon) reloadAccountsIfChanged() {
	if w.app == nil {
		return
	}
	info, err := os.Stat(w.app.paths.AccountsFile)
	if err != nil {
		return
	}
	mtime := info.ModTime().UnixNano()

	w.mu.Lock()
	if w.lastFileMtime != 0 && mtime == w.lastFileMtime {
		w.mu.Unlock()
		return
	}
	w.lastFileMtime = mtime
	w.mu.Unlock()

	w.logDebug("accounts file changed, reloading")
	snapshot, err := loadAccountsSnapshot(w.app.paths.AccountsFile)
	if err != nil {
		if w.app != nil && w.app.log != nil {
			w.app.log.Warn("accounts reload failed: " + err.Error())
		}
		return
	}
	snapshot = w.app.mergeSnapshotWithMemory(snapshot)
	prevEmail := w.accounts.CurrentEmail()
	w.accounts.LoadSnapshot(snapshot, true)
	current := w.accounts.Current()
	if current == nil {
		return
	}
	w.logDebug("accounts reloaded: total=" + strconv.Itoa(w.accounts.Count()))
	w.logDebug("current account: " + current.Email + " used=" + strconv.Itoa(current.Used) +
		" quota=" + strconv.Itoa(current.Quota) + " remaining=" + strconv.Itoa(accountRemaining(current)))
	if prevEmail != "" && strings.EqualFold(prevEmail, current.Email) {
		return
	}
	_ = updateWarpCredentialsWithLog(*current, w.app.log, "reload")

	w.mu.Lock()
	w.lastVirtualUsedSet = false
	w.lastCurrentEmail = current.Email
	w.mu.Unlock()
}

func (w *WarpAddon) replaceAPIKey(f *flow.Flow) {
	if w.accounts == nil || f == nil || f.Request == nil {
		return
	}
	current := w.ensureAvailableAccount()
	if current == nil {
		if w.app != nil && w.app.log != nil {
			w.app.log.Error("no available account")
		}
		return
	}
	if current.APIKey == "" {
		if w.app != nil && w.app.log != nil {
			w.app.log.Error("missing api key for account: " + current.Email)
		}
		return
	}

	path := ""
	if f.Request.URL != nil {
		path = f.Request.URL.Path
	}
	if isMultiAgentPath(path) {
		applyExperimentIDHeader(f.Request.Header, current.ExperimentID, w.app.log)
		applyAPIKeyHeaders(f.Request.Header, current.APIKey, false)
		if shouldInjectAPIKey(path) {
			exp := strings.TrimSpace(current.ExperimentID)
			if exp == "" {
				exp = "none"
			}
			w.logDebug("inject account: email=" + current.Email +
				" api_key=" + apiKeyPreview(current.APIKey) + " exp_id=" + exp +
				" path=" + truncateForLog(path, 120))
		}
		return
	}

	applyExperimentIDHeader(f.Request.Header, current.ExperimentID, w.app.log)
	applyAPIKeyHeaders(f.Request.Header, current.APIKey, false)
	if shouldInjectAPIKey(path) {
		exp := strings.TrimSpace(current.ExperimentID)
		if exp == "" {
			exp = "none"
		}
		w.logDebug("inject account: email=" + current.Email +
			" api_key=" + apiKeyPreview(current.APIKey) + " exp_id=" + exp +
			" path=" + truncateForLog(path, 120))
	}
}

func (w *WarpAddon) ensureAvailableAccount() *Account {
	if w == nil || w.accounts == nil {
		return nil
	}
	maxAttempts := w.accounts.Count() + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	attempts := 0
	for attempts < maxAttempts {
		attempts++
		current := w.accounts.Current()
		if current == nil {
			w.accounts.SelectAvailableAccount()
			current = w.accounts.Current()
			if current == nil {
				break
			}
		}

		if accountIsBanned(current) {
			oldEmail := current.Email
			next := w.accounts.SelectAvailableAccount()
			if next != nil && !strings.EqualFold(oldEmail, next.Email) {
				w.incrementSwitch()
				continue
			}
			break
		}

		if !accountIsAvailable(current) || accountRemaining(current) <= 0 {
			oldEmail := current.Email
			// 当前账号在本地看来额度已不可用时，通知远端做一次额度复查和分类
			if w != nil && current.ID > 0 {
				w.syncAccountStatusToRemote(current.ID, "limited", current.Used, current.Quota)
			}
			w.accounts.MarkCurrentLimitedSoft()
			next := w.accounts.SelectAvailableAccount()
			if next != nil && !strings.EqualFold(oldEmail, next.Email) {
				w.incrementSwitch()
				continue
			}
			break
		}
		break
	}

	// 本地账号全部用尽，尝试远程换号（仅无限额度激活码有效）
	current := w.accounts.Current()
	if current == nil || !accountIsAvailable(current) || accountRemaining(current) <= 0 {
		if rotated := w.tryRemoteRotate(); rotated != nil {
			return rotated
		}
	}

	return w.accounts.Current()
}

// tryRemoteRotate 尝试调用远程换号API（仅无限额度激活码有效）
func (w *WarpAddon) tryRemoteRotate() *Account {
	if w.app == nil || w.app.remote == nil {
		return nil
	}

	// 获取 token 和 deviceID
	token, deviceID := w.app.getRemoteCredentials()
	if token == "" || deviceID == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	newAcc, err := w.app.remote.RotateAccount(ctx, token, deviceID)
	if err != nil {
		if w.app.log != nil {
			w.app.log.Warn("remote rotate failed: " + err.Error())
		}
		return nil
	}

	if newAcc == nil {
		return nil
	}

	// 转换为本地 Account 类型
	localAcc := Account{
		ID:           newAcc.ID,
		Email:        newAcc.Email,
		APIKey:       newAcc.APIKey,
		UID:          newAcc.UID,
		RefreshToken: newAcc.RefreshToken,
		Quota:        newAcc.Quota,
		Used:         newAcc.Used,
		Status:       newAcc.Status,
		Type:         newAcc.Type,
		NextRefresh:  newAcc.NextRefresh,
		ErrorCount:   newAcc.ErrorCount,
	}

	// 替换账号列表
	w.accounts.ReplaceAccounts([]Account{localAcc})

	// 更新凭证
	_ = updateWarpCredentialsWithLog(localAcc, w.app.log, "rotate")

	if w.app.log != nil {
		w.app.log.Info("remote rotate success: " + localAcc.Email)
	}

	w.incrementSwitch()
	return w.accounts.Current()
}

func (w *WarpAddon) injectRulesToRequest(f *flow.Flow) {
	if w.app == nil || f == nil || f.Request == nil {
		return
	}
	path := ""
	if f.Request.URL != nil {
		path = f.Request.URL.Path
	}
	w.logDebug("[Rules] inject called path=" + truncateForLog(path, 120))
	if f.Request.Method != http.MethodPost {
		w.logDebug("[Rules] skip: method=" + f.Request.Method)
		return
	}
	if len(f.Request.Body) == 0 {
		w.logDebug("[Rules] skip: empty body")
		return
	}

	contentType := strings.ToLower(f.Request.Header.Get("Content-Type"))
	if contentType != "" {
		w.logDebug("[Rules] content-type=" + truncateForLog(contentType, 80))
	}
	var requestData map[string]any
	var multipartParts []multipartPart
	var boundary string

	if json.Unmarshal(f.Request.Body, &requestData) == nil {
		w.logDebug("[Rules] request body is JSON")
	} else if strings.Contains(contentType, "multipart/form-data") {
		w.logDebug("[Rules] multipart detected")
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			return
		}
		boundary = params["boundary"]
		if boundary == "" {
			return
		}

		parts, parsed, err := parseMultipartJSON(f.Request.Body, boundary)
		if err != nil || parsed == nil {
			return
		}
		multipartParts = parts
		requestData = parsed
	} else if strings.Contains(contentType, "protobuf") {
		w.logDebug("[Rules] protobuf detected")
		w.injectRulesToProtobuf(f)
		return
	} else {
		w.logDebug("[Rules] skip: unsupported content-type")
		return
	}

	if requestData == nil {
		w.logDebug("[Rules] skip: no request data")
		return
	}

	selected := loadSelectedRules(w.app.paths, w.app.log)
	if len(selected) == 0 {
		w.logDebug("[Rules] skip: no selected rules")
		return
	}

	ref := findMessagesRef(requestData)
	if ref == nil {
		if prompt, ok := requestData["prompt"].(string); ok && prompt != "" {
			block := buildRulesBlock(selected)
			if block == "" {
				return
			}
			requestData["prompt"] = block + "\n\n" + prompt
			w.logDebug("[Rules] injected into prompt rules=" + strconv.Itoa(len(selected)))
			w.writeRequestBody(f, requestData, multipartParts, boundary)
		}
		return
	}

	block := buildRulesBlock(selected)
	if block == "" {
		return
	}

	msgs := ref.msgs
	systemIdx := -1
	for i, item := range msgs {
		msgMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msgMap["role"].(string); role == "system" {
			systemIdx = i
			break
		}
	}

	var sampleMsg map[string]any
	if systemIdx >= 0 {
		if msgMap, ok := msgs[systemIdx].(map[string]any); ok {
			sampleMsg = msgMap
		}
	} else if len(msgs) > 0 {
		if msgMap, ok := msgs[0].(map[string]any); ok {
			sampleMsg = msgMap
		}
	}
	sampleFmt := inferMsgFormat(sampleMsg)

	if systemIdx < 0 {
		sysMsg := map[string]any{"role": "system"}
		setMsgText(sysMsg, block, sampleFmt)
		msgs = append([]any{sysMsg}, msgs...)
		ref.parent[ref.key] = msgs
		w.logDebug("[Rules] injected new system message rules=" + strconv.Itoa(len(selected)))
	} else {
		sysAny := msgs[systemIdx]
		sysMsg, ok := sysAny.(map[string]any)
		if !ok {
			sysMsg = map[string]any{"role": "system"}
			setMsgText(sysMsg, block, sampleFmt)
			msgs = append([]any{sysMsg}, msgs...)
			ref.parent[ref.key] = msgs
			w.logDebug("[Rules] injected system message rules=" + strconv.Itoa(len(selected)))
		} else {
			orig := getMsgText(sysMsg)
			cleaned := strings.TrimSpace(rulesBlockRe.ReplaceAllString(orig, ""))
			newText := block
			if cleaned != "" {
				newText = block + "\n\n" + cleaned
			}
			setMsgText(sysMsg, newText, sampleFmt)
			w.logDebug("[Rules] updated system message rules=" + strconv.Itoa(len(selected)))
		}
	}

	w.writeRequestBody(f, requestData, multipartParts, boundary)
}

func (w *WarpAddon) injectRulesToProtobuf(f *flow.Flow) {
	if w.app == nil || f == nil || f.Request == nil || len(f.Request.Body) == 0 {
		return
	}
	selected := loadSelectedRules(w.app.paths, w.app.log)
	if len(selected) == 0 {
		w.logDebug("[Rules] protobuf skip: no selected rules")
		return
	}
	rulesText := buildProtobufRulesText(selected)
	if rulesText == "" {
		return
	}
	rulesBytes := []byte(rulesText)

	encoding := strings.ToLower(strings.TrimSpace(f.Request.Header.Get("Content-Encoding")))
	if strings.Contains(encoding, ",") {
		return
	}

	original := f.Request.Body
	if encoding != "" && encoding != "identity" {
		decoded, err := decodeRequestBody(encoding, original)
		if err != nil || len(decoded) == 0 {
			return
		}
		original = decoded
	}
	lastPos := -1
	lastLen := 0
	for i := 0; i < len(original)-2; i++ {
		length := int(original[i])
		if length < 10 || length >= 200 {
			continue
		}
		if i+1+length > len(original) {
			continue
		}
		segment := original[i+1 : i+1+length]
		if !utf8.Valid(segment) {
			continue
		}
		text := string(segment)
		if containsReadableText(text) {
			lastPos = i
			lastLen = length
		}
	}

	if lastPos < 0 {
		return
	}

	newText := append([]byte{}, original[lastPos+1:lastPos+1+lastLen]...)
	newText = append(newText, rulesBytes...)
	if len(newText) >= 128 {
		return
	}

	out := make([]byte, 0, len(original)+(len(newText)-lastLen))
	out = append(out, original[:lastPos]...)
	out = append(out, byte(len(newText)))
	out = append(out, newText...)
	out = append(out, original[lastPos+1+lastLen:]...)
	if encoding != "" && encoding != "identity" {
		encoded, err := encodeRequestBody(encoding, out)
		if err != nil || len(encoded) == 0 {
			return
		}
		f.Request.Body = encoded
		f.Request.Header.Set("Content-Length", strconv.Itoa(len(encoded)))
		w.logDebug("[Rules] protobuf injected rules=" + strconv.Itoa(len(selected)))
		return
	}
	f.Request.Body = out
	f.Request.Header.Set("Content-Length", strconv.Itoa(len(out)))
	w.logDebug("[Rules] protobuf injected rules=" + strconv.Itoa(len(selected)))
}

func (w *WarpAddon) writeRequestBody(f *flow.Flow, data map[string]any, parts []multipartPart, boundary string) {
	if f == nil || f.Request == nil {
		return
	}
	body, err := marshalJSON(data)
	if err != nil {
		return
	}

	if len(parts) > 0 && boundary != "" {
		replaced := false
		for i := range parts {
			if !replaced && isJSONPartName(parts[i].Name) {
				parts[i].Content = body
				replaced = true
			}
		}
		if newBody, err := buildMultipart(parts, boundary); err == nil {
			f.Request.Body = newBody
			f.Request.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
			return
		}
	}

	f.Request.Body = body
	f.Request.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

func (w *WarpAddon) checkBannedResponse(f *flow.Flow) bool {
	if f == nil || f.Response == nil {
		return false
	}
	if f.Request != nil && f.Request.URL != nil && isMultiAgentPath(f.Request.URL.Path) {
		return false
	}
	status := f.Response.StatusCode
	if status != http.StatusForbidden {
		return false
	}

	f.Response.ReplaceToDecodedBody()
	content := strings.ToLower(string(f.Response.Body))
	if content == "" {
		return false
	}

	isBanned := false
	for _, pattern := range bannedPatterns {
		if strings.Contains(content, pattern) {
			isBanned = true
			break
		}
	}
	if !isBanned {
		return false
	}

	current := w.accounts.Current()
	if current != nil && w.app != nil && w.app.log != nil {
		w.app.log.Error("account banned detected: " + current.Email)
	}
	w.incrementSwitch()
	w.accounts.MarkCurrentBanned()
	w.retryWithNewAccount(f, 10)
	return true
}

func (w *WarpAddon) checkMultiAgentResponse(f *flow.Flow) bool {
	if f == nil || f.Request == nil || f.Response == nil || f.Request.URL == nil {
		return false
	}
	if !isMultiAgentPath(f.Request.URL.Path) {
		return false
	}
	status := f.Response.StatusCode
	if status < http.StatusBadRequest {
		return false
	}

	f.Response.ReplaceToDecodedBody()
	content := strings.ToLower(responseMessage(f.Response.Body))
	if content == "" {
		content = strings.ToLower(string(f.Response.Body))
	}

	// 检测 banned 账号
	if containsAnyPattern(content, bannedPatterns) {
		w.handleMultiAgentSwitch("banned", status, content)
		w.retryWithNewAccount(f, 10)
		return true
	}

	// 检测通用 403 错误 (WAF 拦截等)
	if status == http.StatusForbidden {
		if w.app != nil && w.app.log != nil {
			w.app.log.Warn("multi-agent 403 detected, switching account: " + truncateForLog(content, 200))
		}
		w.handleMultiAgentSwitch("error", status, content)
		w.retryWithNewAccount(f, 10)
		return true
	}

	return false
}

func (w *WarpAddon) handleMultiAgentSwitch(action string, status int, reason string) {
	if w == nil || w.accounts == nil {
		return
	}

	prevEmail := ""
	prevID := 0
	prevUsed := 0
	prevQuota := 0
	if current := w.accounts.Current(); current != nil {
		prevEmail = current.Email
		prevID = current.ID
		prevUsed = current.Used
		prevQuota = current.Quota
	}

	var next *Account
	switch action {
	case "limited":
		next = w.accounts.MarkCurrentLimited()
		// 同步状态到远程服务器（limited 表示用完，used=quota）
		w.syncAccountStatusToRemote(prevID, "limited", prevQuota, prevQuota)
	case "banned":
		next = w.accounts.MarkCurrentBanned()
		// 同步状态到远程服务器
		w.syncAccountStatusToRemote(prevID, "banned", prevUsed, prevQuota)
	default:
		w.accounts.MarkCurrentError()
		next = w.accounts.Current()
		if next != nil {
			_ = updateWarpCredentialsWithLog(*next, w.app.log, "error")
		}
	}

	if w.app != nil && w.app.log != nil {
		msg := "multi-agent switch: " + action + " status=" + strconv.Itoa(status)
		if prevEmail != "" && next != nil && !strings.EqualFold(prevEmail, next.Email) {
			msg += " " + prevEmail + " -> " + next.Email
		}
		if trimmed := strings.TrimSpace(reason); trimmed != "" {
			msg += " reason=" + truncateForLog(trimmed, 240)
		}
		w.app.log.Warn(msg)
	}

	if prevEmail != "" && next != nil && !strings.EqualFold(prevEmail, next.Email) {
		w.incrementSwitch()
	}
}

// syncAccountStatusToRemote 同步账号状态到远程服务器
// used 和 quota 可仠为 -1 表示不同步
func (w *WarpAddon) syncAccountStatusToRemote(accountID int, status string, used int, quota int) {
	if w.app == nil || w.app.remote == nil || accountID <= 0 {
		return
	}

	token, deviceID := w.app.getRemoteCredentials()
	if token == "" || deviceID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 同时同步状态、使用量和额度
	payload := map[string]any{"status": status}
	if used >= 0 && quota >= 0 {
		payload["used"] = used
		payload["quota"] = quota
	}
	err := w.app.remote.UpdateAccount(ctx, token, deviceID, accountID, payload)
	if err != nil {
		if w.app.log != nil {
			w.app.log.Warn("sync account status to remote failed: " + err.Error())
		}
	} else {
		if w.app.log != nil {
			w.app.log.Info("sync account status to remote: id=" + strconv.Itoa(accountID) + " status=" + status)
		}
	}
}

// syncCurrentUsageToRemote 在后台异步同步当前账号的使用量
func (w *WarpAddon) syncCurrentUsageToRemote() {
	if w.app == nil || w.app.remote == nil {
		return
	}

	current := w.accounts.Current()
	if current == nil || current.ID <= 0 {
		return
	}

	token, deviceID := w.app.getRemoteCredentials()
	if token == "" || deviceID == "" {
		return
	}

	// 后台异步同步，避免阻塞响应
	go func(accountID, used, quota int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		payload := map[string]any{
			"used":  used,
			"quota": quota,
		}
		_ = w.app.remote.UpdateAccount(ctx, token, deviceID, accountID, payload)
	}(current.ID, current.Used, current.Quota)
}

func (w *WarpAddon) checkLimitedResponse(f *flow.Flow) bool {
	if w == nil || f == nil || f.Response == nil {
		return false
	}
	status := f.Response.StatusCode
	// 与 Python 保持一致：只处理 401, 403, 429 状态码
	if status != http.StatusTooManyRequests && status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}

	f.Response.ReplaceToDecodedBody()
	content := strings.ToLower(responseMessage(f.Response.Body))
	if content == "" {
		content = strings.ToLower(string(f.Response.Body))
	}

	// 检测 429 (Too Many Requests)
	if status == http.StatusTooManyRequests {
		if w.app != nil && w.app.log != nil {
			w.app.log.Warn("429 too many requests, switching account: " + truncateForLog(content, 200))
		}
		w.handleMultiAgentSwitch("limited", status, content)
		w.retryWithNewAccount(f, 10)
		return true
	}

	// 检测 401 (Unauthorized)
	if status == http.StatusUnauthorized {
		if w.app != nil && w.app.log != nil {
			w.app.log.Warn("401 unauthorized, switching account: " + truncateForLog(content, 200))
		}
		w.handleMultiAgentSwitch("limited", status, content)
		w.retryWithNewAccount(f, 10)
		return true
	}

	return false
}

func (w *WarpAddon) handleLimitedRetry(f *flow.Flow, reason string) bool {
	if w == nil || w.accounts == nil {
		return false
	}
	prevID := 0
	prevQuota := 0
	if current := w.accounts.Current(); current != nil {
		prevID = current.ID
		prevQuota = current.Quota
	}
	next := w.accounts.MarkCurrentLimited()
	if next == nil {
		return false
	}
	// 同步状态到远程服务器（limited 表示用完，used=quota）
	w.syncAccountStatusToRemote(prevID, "limited", prevQuota, prevQuota)
	if w.app != nil && w.app.log != nil {
		msg := "account limited detected"
		if strings.TrimSpace(reason) != "" {
			msg += ": " + reason
		}
		w.app.log.Warn(msg)
	}
	w.incrementSwitch()
	w.retryWithNewAccount(f, 10)
	return true
}

func (w *WarpAddon) retryWithNewAccount(f *flow.Flow, maxRetries int) {
	if f == nil || f.Request == nil {
		return
	}
	tried := map[string]struct{}{}
	retries := 0
	for retries < maxRetries {
		current := w.accounts.Current()
		if current == nil {
			return
		}
		if current.Email != "" {
			if _, ok := tried[current.Email]; ok {
				return
			}
			tried[current.Email] = struct{}{}
		}

		retries++
		if w.app != nil && w.app.log != nil {
			w.app.log.Info("retry account: " + current.Email)
		}

		req, err := w.buildRetryRequest(f, current)
		if err != nil {
			w.accounts.MarkCurrentError()
			continue
		}

		resp, err := w.directClient().Do(req)
		if err != nil {
			w.accounts.MarkCurrentError()
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		textLower := strings.ToLower(string(body))

		// 检测 banned 账号 (403 + banned patterns)
		if resp.StatusCode == http.StatusForbidden {
			if containsAnyPattern(textLower, bannedPatterns) {
				if w.app != nil && w.app.log != nil {
					w.app.log.Warn("retry: account banned detected: " + truncateForLog(textLower, 200))
				}
				curID := 0
				curUsed := 0
				curQuota := 0
				if cur := w.accounts.Current(); cur != nil {
					curID = cur.ID
					curUsed = cur.Used
					curQuota = cur.Quota
				}
				w.incrementSwitch()
				w.accounts.MarkCurrentBanned()
				w.syncAccountStatusToRemote(curID, "banned", curUsed, curQuota)
				continue
			}
		}

		// 检测 429 (Too Many Requests) - 注意：429 是限流，不是额度用完，但客户端仍传递完整数据由后端判断
		if resp.StatusCode == http.StatusTooManyRequests {
			if w.app != nil && w.app.log != nil {
				w.app.log.Warn("retry: 429 too many requests: " + truncateForLog(textLower, 200))
			}
			curID := 0
			curUsed := 0
			curQuota := 0
			if cur := w.accounts.Current(); cur != nil {
				curID = cur.ID
				curUsed = cur.Used
				curQuota = cur.Quota
			}
			w.incrementSwitch()
			w.accounts.MarkCurrentLimited()
			w.syncAccountStatusToRemote(curID, "limited", curUsed, curQuota)
			continue
		}

		// 检测 401 (Unauthorized)
		if resp.StatusCode == http.StatusUnauthorized {
			if w.app != nil && w.app.log != nil {
				w.app.log.Warn("retry: 401 unauthorized: " + truncateForLog(textLower, 200))
			}
			curID := 0
			curUsed := 0
			curQuota := 0
			if cur := w.accounts.Current(); cur != nil {
				curID = cur.ID
				curUsed = cur.Used
				curQuota = cur.Quota
			}
			w.incrementSwitch()
			w.accounts.MarkCurrentLimited()
			w.syncAccountStatusToRemote(curID, "limited", curUsed, curQuota)
			continue
		}

		// 检测通用 403 错误 (即使没有 banned pattern 也切换)
		if resp.StatusCode == http.StatusForbidden {
			if w.app != nil && w.app.log != nil {
				w.app.log.Warn("retry: generic 403 detected: " + truncateForLog(textLower, 200))
			}
			w.incrementSwitch()
			w.accounts.MarkCurrentError()
			continue
		}

		f.Response = &flow.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       body,
		}
		return
	}
}

func (w *WarpAddon) buildRetryRequest(f *flow.Flow, acc *Account) (*http.Request, error) {
	if f == nil || f.Request == nil || f.Request.URL == nil {
		return nil, errors.New("invalid flow")
	}
	body := f.Request.Body
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(f.Request.Method, f.Request.URL.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header = cloneHeader(f.Request.Header)
	if isAgentEndpoint(f.Request.URL.Path) {
		ensureNoDefaultUserAgent(req.Header)
	}
	if acc != nil {
		if isMultiAgentPath(f.Request.URL.Path) {
			applyExperimentIDHeader(req.Header, acc.ExperimentID, w.app.log)
			applyAPIKeyHeaders(req.Header, acc.APIKey, false)
		} else if acc.APIKey != "" {
			applyAPIKeyHeaders(req.Header, acc.APIKey, false)
			applyExperimentIDHeader(req.Header, acc.ExperimentID, w.app.log)
		}
	}
	if len(body) > 0 {
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	return req, nil
}

func (w *WarpAddon) modifyQuotaResponse(f *flow.Flow) {
	if f == nil || f.Response == nil || len(f.Response.Body) == 0 {
		return
	}

	f.Response.ReplaceToDecodedBody()
	var payload map[string]any
	if json.Unmarshal(f.Response.Body, &payload) != nil {
		return
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		return
	}

	var limitInfo map[string]any
	if info, ok := data["getRequestLimitInfo"].(map[string]any); ok {
		limitInfo = info
	} else if info, ok := data["requestLimitInfo"].(map[string]any); ok {
		limitInfo = info
	} else if userObj, ok := data["user"].(map[string]any); ok {
		if inner, ok := userObj["user"].(map[string]any); ok {
			if info, ok := inner["requestLimitInfo"].(map[string]any); ok {
				limitInfo = info
			}
		}
	}

	if limitInfo == nil {
		return
	}

	modified := false
	if _, ok := limitInfo["requestLimit"]; ok {
		realUsed := asInt(limitInfo["requestsUsedSinceLastRefresh"], 0)
		realQuota := asInt(limitInfo["requestLimit"], 150)
		// 在切换前保存旧账号信息，用于同步到后端
		oldAccount := w.accounts.Current()
		virtualUsed, virtualQuota, needSwitch := w.accounts.SyncUsage(realUsed, realQuota)
		limitInfo["requestsUsedSinceLastRefresh"] = virtualUsed
		limitInfo["requestLimit"] = virtualQuota
		modified = true
		w.printStatus(virtualUsed, virtualQuota)
		// 账号额度用完时，同步旧账号状态到后台（触发后端二次检测）
		if needSwitch && oldAccount != nil && oldAccount.ID > 0 {
			w.syncAccountStatusToRemote(oldAccount.ID, "limited", oldAccount.Quota, oldAccount.Quota)
		}
	} else if _, ok := limitInfo["used"]; ok {
		if _, ok := limitInfo["quota"]; ok {
			realUsed := asInt(limitInfo["used"], 0)
			realQuota := asInt(limitInfo["quota"], 150)
			// 在切换前保存旧账号信息，用于同步到后端
			oldAccount := w.accounts.Current()
			virtualUsed, virtualQuota, needSwitch := w.accounts.SyncUsage(realUsed, realQuota)
			limitInfo["used"] = virtualUsed
			limitInfo["quota"] = virtualQuota
			modified = true
			w.printStatus(virtualUsed, virtualQuota)
			// 账号额度用完时，同步旧账号状态到后台（触发后端二次检测）
			if needSwitch && oldAccount != nil && oldAccount.ID > 0 {
				w.syncAccountStatusToRemote(oldAccount.ID, "limited", oldAccount.Quota, oldAccount.Quota)
			}
		}
	}

	if !modified {
		return
	}

	newBody, err := marshalJSON(payload)
	if err != nil {
		return
	}
	f.Response.Body = newBody
	f.Response.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
}

func (w *WarpAddon) printStatus(virtualUsed, virtualQuota int) {
	current := w.accounts.Current()
	currentEmail := ""
	currentUsed := 0
	currentQuota := 0
	if current != nil {
		currentEmail = current.Email
		currentUsed = current.Used
		currentQuota = current.Quota
	}

	w.mu.Lock()
	consumption := 0
	if w.lastVirtualUsedSet {
		if virtualUsed > w.lastVirtualUsed {
			consumption = virtualUsed - w.lastVirtualUsed
		}
	} else {
		w.lastVirtualUsedSet = true
	}
	w.lastVirtualUsed = virtualUsed

	switchIndicator := ""
	if w.lastCurrentEmail != "" && currentEmail != "" && !strings.EqualFold(w.lastCurrentEmail, currentEmail) {
		switchIndicator = " [switch #" + strconv.Itoa(w.accountsSwitched) + "]"
		if w.app != nil && w.app.log != nil {
			w.app.log.Info("account switched: " + w.lastCurrentEmail + " -> " + currentEmail)
		}
	}
	w.lastCurrentEmail = currentEmail
	w.mu.Unlock()

	totalRemaining := virtualQuota - virtualUsed
	if totalRemaining < 0 {
		totalRemaining = 0
	}
	currentRemaining := currentQuota - currentUsed
	if currentRemaining < 0 {
		currentRemaining = 0
	}
	usageRate := 0.0
	if virtualQuota > 0 {
		usageRate = float64(virtualUsed) / float64(virtualQuota) * 100.0
	}

	if w.app != nil && w.app.log != nil {
		msg := "usage: current=" + currentEmail + " (" + strconv.Itoa(currentUsed) + "/" + strconv.Itoa(currentQuota) +
			", remaining=" + strconv.Itoa(currentRemaining) + ") total=" + strconv.Itoa(virtualUsed) + "/" + strconv.Itoa(virtualQuota) +
			", rate=" + strconv.FormatFloat(usageRate, 'f', 2, 64) + "%, remaining_total=" + strconv.Itoa(totalRemaining) +
			", consumption=" + strconv.Itoa(consumption) + switchIndicator
		w.app.log.Info(msg)
	}
}

func (w *WarpAddon) directClient() *http.Client {
	dialer := newWarpDialer()
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   true,
		DisableCompression:  true,
		TLSHandshakeTimeout: 10 * 1000000000,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	return &http.Client{
		Transport: newWarpRoundTripper(transport, dialer, nil, w.app.log),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * 1000000000,
	}
}

func (w *WarpAddon) incrementSwitch() {
	w.mu.Lock()
	w.accountsSwitched += 1
	w.mu.Unlock()
}

type multipartPart struct {
	Name    string
	Header  textproto.MIMEHeader
	Content []byte
}

func parseMultipartJSON(body []byte, boundary string) ([]multipartPart, map[string]any, error) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	parts := make([]multipartPart, 0)
	var parsed map[string]any

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		data, _ := io.ReadAll(part)
		name := part.FormName()
		parts = append(parts, multipartPart{
			Name:    name,
			Header:  part.Header,
			Content: data,
		})
		if parsed == nil && isJSONPartName(name) {
			var payload map[string]any
			if json.Unmarshal(data, &payload) == nil {
				parsed = payload
			}
		}
	}

	if parsed == nil {
		return parts, nil, errors.New("no json part")
	}
	return parts, parsed, nil
}

func buildMultipart(parts []multipartPart, boundary string) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}
	for _, part := range parts {
		pw, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, err
		}
		if len(part.Content) > 0 {
			if _, err := pw.Write(part.Content); err != nil {
				return nil, err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isJSONPartName(name string) bool {
	switch name {
	case "request", "input", "data", "payload", "variables":
		return true
	default:
		return false
	}
}

type messagesRef struct {
	parent map[string]any
	key    string
	msgs   []any
}

func findMessagesRef(data map[string]any) *messagesRef {
	candidates := [][]string{
		{"messages"},
		{"input", "messages"},
		{"variables", "messages"},
		{"variables", "input", "messages"},
		{"variables", "payload", "messages"},
		{"variables", "request", "messages"},
		{"variables", "input", "request", "messages"},
		{"data", "messages"},
		{"request", "messages"},
		{"payload", "messages"},
	}

	for _, path := range candidates {
		if ref := getMessagesByPath(data, path); ref != nil {
			return ref
		}
	}
	return findMessagesRecursive(data, 0, 8, nil, "")
}

func getMessagesByPath(data map[string]any, path []string) *messagesRef {
	cur := data
	for i, key := range path {
		if i == len(path)-1 {
			val, ok := cur[key]
			if !ok {
				return nil
			}
			msgs, ok := val.([]any)
			if !ok || !looksLikeMessages(msgs) {
				return nil
			}
			return &messagesRef{parent: cur, key: key, msgs: msgs}
		}
		next, ok := cur[key].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return nil
}

func findMessagesRecursive(obj any, depth, maxDepth int, parent map[string]any, key string) *messagesRef {
	if depth > maxDepth {
		return nil
	}
	switch v := obj.(type) {
	case map[string]any:
		if msgs, ok := v["messages"].([]any); ok && looksLikeMessages(msgs) {
			return &messagesRef{parent: v, key: "messages", msgs: msgs}
		}
		for k, child := range v {
			if ref := findMessagesRecursive(child, depth+1, maxDepth, v, k); ref != nil {
				return ref
			}
		}
	case []any:
		if looksLikeMessages(v) && parent != nil {
			return &messagesRef{parent: parent, key: key, msgs: v}
		}
		limit := len(v)
		if limit > 50 {
			limit = 50
		}
		for i := 0; i < limit; i++ {
			if ref := findMessagesRecursive(v[i], depth+1, maxDepth, parent, key); ref != nil {
				return ref
			}
		}
	}
	return nil
}

func looksLikeMessages(msgs []any) bool {
	if len(msgs) == 0 {
		return false
	}
	hasFields := false
	for _, item := range msgs {
		msgMap, ok := item.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := msgMap["role"]; ok {
			hasFields = true
		}
		if _, ok := msgMap["content"]; ok {
			hasFields = true
		}
		if _, ok := msgMap["text"]; ok {
			hasFields = true
		}
	}
	return hasFields
}

func inferMsgFormat(msg map[string]any) string {
	if msg == nil {
		return "content_str"
	}
	if content, ok := msg["content"]; ok {
		switch c := content.(type) {
		case []any:
			if len(c) > 0 {
				if part, ok := c[0].(map[string]any); ok {
					if _, ok := part["text"].(string); ok {
						return "content_parts"
					}
				}
			}
			return "content_str"
		case string:
			return "content_str"
		}
	}
	if _, ok := msg["text"].(string); ok {
		return "text_field"
	}
	return "content_str"
}

func getMsgText(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	switch inferMsgFormat(msg) {
	case "text_field":
		if value, ok := msg["text"].(string); ok {
			return value
		}
	case "content_parts":
		parts, ok := msg["content"].([]any)
		if !ok {
			return ""
		}
		var sb strings.Builder
		for _, part := range parts {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := partMap["text"].(string); ok {
				sb.WriteString(text)
			}
		}
		return sb.String()
	default:
		if value, ok := msg["content"].(string); ok {
			return value
		}
	}
	return ""
}

func setMsgText(msg map[string]any, text string, preferFormat string) {
	if msg == nil {
		return
	}
	format := preferFormat
	if format == "" {
		format = inferMsgFormat(msg)
	}
	switch format {
	case "text_field":
		msg["text"] = text
	case "content_parts":
		partType := "text"
		if parts, ok := msg["content"].([]any); ok && len(parts) > 0 {
			if partMap, ok := parts[0].(map[string]any); ok {
				if t, ok := partMap["type"].(string); ok && t != "" {
					partType = t
				}
			}
		}
		msg["content"] = []any{
			map[string]any{
				"type": partType,
				"text": text,
			},
		}
	default:
		msg["content"] = text
	}
}

func containsReadableText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
		switch r {
		case ' ', ',', '.', '!', '?', ':', ';':
			return true
		case '\u3001', '\u3002', '\uff0c', '\uff01', '\uff1f':
			return true
		}
	}
	return false
}

func hostBlocked(host string) bool {
	for _, blocked := range blockedHosts {
		if strings.Contains(host, blocked) {
			return true
		}
	}
	return false
}

func shouldInjectAPIKey(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "/ai/") {
		return true
	}
	if strings.Contains(lower, "multi-agent") {
		return true
	}
	if strings.Contains(lower, "graphql") {
		return true
	}
	return false
}

func rewriteSecureTokenRequest(f *flow.Flow) bool {
	if f == nil || f.Request == nil || f.Request.URL == nil {
		return false
	}
	if !strings.EqualFold(requestHostname(f.Request), "securetoken.googleapis.com") {
		return false
	}
	path := strings.ToLower(f.Request.URL.Path)
	if !strings.HasPrefix(path, "/v1/token") {
		return false
	}

	f.Request.URL.Scheme = "https"
	f.Request.URL.Host = warpProxyHost
	f.Request.URL.Path = warpProxyTokenPath
	f.Request.Header.Set("Host", warpProxyHost)

	setHeaderIfMissing(f.Request.Header, "Content-Type", "application/x-www-form-urlencoded")
	setHeaderIfMissing(f.Request.Header, "Accept", "*/*")
	setHeaderIfMissing(f.Request.Header, "x-warp-client-version", defaultWarpClientVersion)
	setHeaderIfMissing(f.Request.Header, "x-warp-os-category", "desktop")
	setHeaderIfMissing(f.Request.Header, "x-warp-os-name", warpOSName())
	setHeaderIfMissing(f.Request.Header, "x-warp-os-version", warpOSVersion())
	return true
}

func isWarpProxyTokenRequest(req *flow.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	if !strings.EqualFold(requestHostname(req), warpProxyHost) {
		return false
	}
	return strings.HasPrefix(strings.ToLower(req.URL.Path), warpProxyTokenPath)
}

func (w *WarpAddon) replaceRefreshToken(f *flow.Flow) (bool, string) {
	if w == nil || w.accounts == nil || f == nil || f.Request == nil {
		return false, "missing-request"
	}
	req := f.Request
	if req.Method != http.MethodPost {
		return false, "non-post"
	}
	if len(req.Body) == 0 {
		return false, "empty-body"
	}

	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return false, "unsupported-content-type"
	}

	values, err := url.ParseQuery(string(req.Body))
	if err != nil {
		return false, "parse-error"
	}
	if strings.ToLower(values.Get("grant_type")) != "refresh_token" {
		return false, "non-refresh-grant"
	}

	current := w.ensureAvailableAccount()
	if current == nil {
		return false, "no-account"
	}
	refresh := strings.TrimSpace(current.RefreshToken)
	if refresh == "" {
		return false, "empty-refresh"
	}

	if strings.TrimSpace(values.Get("refresh_token")) == refresh {
		return false, "already-current"
	}
	values.Set("refresh_token", refresh)
	newBody := values.Encode()
	req.Body = []byte(newBody)
	req.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w.app != nil && w.app.log != nil && current.Email != "" {
		w.app.log.Info("proxy token refresh replaced: " + current.Email)
	}
	return true, ""
}

func isAgentEndpoint(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "/ai/")
}

func isGatewaySkipRequest(req *flow.Request) bool {
	if req == nil || req.Header == nil {
		return false
	}
	if strings.TrimSpace(req.Header.Get("X-Warp-Gateway-Skip")) == "1" {
		return true
	}
	if strings.TrimSpace(req.Header.Get("x-warp-gateway-skip")) == "1" {
		return true
	}
	return false
}

func isClientAuthEndpoint(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "/client/login") || strings.HasPrefix(lower, "/client/token")
}

func requestHostname(req *flow.Request) string {
	if req == nil {
		return ""
	}
	if req.URL != nil {
		if host := strings.TrimSpace(req.URL.Hostname()); host != "" {
			return host
		}
		if host := strings.TrimSpace(req.URL.Host); host != "" {
			return normalizeHost(host)
		}
	}
	if req.Header == nil {
		return ""
	}
	return normalizeHost(req.Header.Get("Host"))
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") {
		if idx := strings.Index(host, "]"); idx > 0 {
			return host[1:idx]
		}
	}
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return h
		}
	}
	return host
}

func isMultiAgentPath(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "/ai/multi-agent")
}

func applyAPIKeyHeaders(header http.Header, apiKey string, inject bool) {
	if header == nil || apiKey == "" {
		return
	}
	auth := header.Get("Authorization")
	if _, ok := bearerToken(auth); ok {
		header.Set("Authorization", "Bearer "+apiKey)
	} else if inject {
		header.Set("Authorization", "Bearer "+apiKey)
	}

	setKeyHeader := func(name string) {
		if value := header.Get(name); value != "" {
			header.Set(name, apiKey)
			return
		}
		if inject {
			header.Set(name, apiKey)
		}
	}
	setKeyHeader("X-API-Key")
	setKeyHeader("x-api-key")
}

func applyAPIKeyKeyHeaders(header http.Header, apiKey string, inject bool) {
	if header == nil || apiKey == "" {
		return
	}
	setKeyHeader := func(name string) {
		if value := header.Get(name); value != "" {
			header.Set(name, apiKey)
			return
		}
		if inject {
			header.Set(name, apiKey)
		}
	}
	setKeyHeader("X-API-Key")
	setKeyHeader("x-api-key")
}

func applyExperimentIDHeader(header http.Header, experimentID string, log *Logger) {
	if header == nil {
		return
	}
	experimentID = strings.TrimSpace(experimentID)
	prev := strings.TrimSpace(header.Get("x-warp-experiment-id"))
	if prev == "" {
		prev = strings.TrimSpace(header.Get("X-Warp-Experiment-Id"))
	}
	if experimentID == "" {
		if prev != "" {
			header.Del("x-warp-experiment-id")
			header.Del("X-Warp-Experiment-Id")
			if log != nil {
				log.Info("[DEBUG] experiment id cleared")
			}
		}
		return
	}
	if prev != experimentID {
		header.Set("x-warp-experiment-id", experimentID)
		if log != nil {
			log.Info("[DEBUG] experiment id set")
		}
	}
}

func setHeaderIfMissing(header http.Header, name, value string) {
	if header == nil || value == "" {
		return
	}
	if header.Get(name) != "" {
		return
	}
	header.Set(name, value)
}

func ensureNoDefaultUserAgent(header http.Header) bool {
	if header == nil {
		return false
	}
	if strings.TrimSpace(header.Get("User-Agent")) != "" {
		return false
	}
	header.Set("User-Agent", "")
	return true
}

func (w *WarpAddon) normalizeAIHeaders(f *flow.Flow) {
	if w == nil || f == nil || f.Request == nil || f.Request.Header == nil || f.Request.URL == nil {
		return
	}
	if !isAgentEndpoint(f.Request.URL.Path) {
		return
	}
	if ensureNoDefaultUserAgent(f.Request.Header) {
		w.logDebug("user-agent cleared for ai request")
	}
}

func warpOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func warpOSVersion() string {
	switch runtime.GOOS {
	case "windows":
		return "10"
	case "darwin":
		return "14"
	default:
		return "0"
	}
}

func bearerToken(auth string) (string, bool) {
	fields := strings.Fields(auth)
	if len(fields) != 2 {
		return "", false
	}
	if !strings.EqualFold(fields[0], "bearer") {
		return "", false
	}
	return fields[1], true
}

func isJWTBearer(auth string) bool {
	token, ok := bearerToken(auth)
	if !ok {
		return false
	}
	return strings.HasPrefix(token, "eyJ")
}

func isWarpAPIKey(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "wk-1.")
}


func containsAnyPattern(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func responseMessage(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "{") {
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			if msg := extractErrorMessage(payload); msg != "" {
				return msg
			}
		}
	}
	return text
}

func truncateForLog(text string, max int) string {
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func extractErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if errs, ok := payload["errors"]; ok {
		return collectErrorMessages(errs)
	}
	if errMsg, ok := payload["error"].(string); ok {
		return errMsg
	}
	if errObj, ok := payload["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok {
			return msg
		}
	}
	if data, ok := payload["data"].(map[string]any); ok {
		if user, ok := data["user"].(map[string]any); ok {
			if errObj, ok := user["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok {
					return msg
				}
			}
		}
	}
	return ""
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return http.Header{}
	}
	out := make(http.Header, len(header))
	for k, v := range header {
		copied := make([]string, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

func marshalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

func decodeRequestBody(encoding string, body []byte) ([]byte, error) {
	switch encoding {
	case "", "identity":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	case "br":
		reader := brotli.NewReader(bytes.NewReader(body))
		return io.ReadAll(reader)
	case "deflate":
		reader := flate.NewReader(bytes.NewReader(body))
		defer reader.Close()
		return io.ReadAll(reader)
	default:
		return nil, errors.New("unsupported content-encoding")
	}
}

func encodeRequestBody(encoding string, body []byte) ([]byte, error) {
	switch encoding {
	case "", "identity":
		return body, nil
	case "gzip":
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		if _, err := writer.Write(body); err != nil {
			_ = writer.Close()
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "br":
		var buf bytes.Buffer
		writer := brotli.NewWriter(&buf)
		if _, err := writer.Write(body); err != nil {
			_ = writer.Close()
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "deflate":
		var buf bytes.Buffer
		writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(body); err != nil {
			_ = writer.Close()
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, errors.New("unsupported content-encoding")
	}
}
