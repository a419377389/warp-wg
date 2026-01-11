package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type warpRoundTripper struct {
	defaultH1 *http.Transport
	warpH1    *http.Transport
	warpH2    *http2.Transport
	log       *Logger
}

func newWarpDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}
}

func newWarpRoundTripper(h1 *http.Transport, dialer *net.Dialer, keyLogWriter io.Writer, log *Logger) http.RoundTripper {
	if h1 == nil {
		h1 = &http.Transport{}
	}
	if dialer == nil {
		dialer = newWarpDialer()
	}

	warpH1 := h1.Clone()
	warpH1.ForceAttemptHTTP2 = false
	warpH1.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
	if warpH1.TLSClientConfig == nil {
		warpH1.TLSClientConfig = &tls.Config{}
	}
	warpH1.TLSClientConfig.InsecureSkipVerify = true
	warpH1.TLSClientConfig.KeyLogWriter = keyLogWriter
	warpH1.TLSClientConfig.NextProtos = []string{"http/1.1"}
	warpH1.DialContext = dialer.DialContext
	warpH1.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialWarpUTLS(ctx, network, addr, warpH1.TLSClientConfig, dialer, keyLogWriter)
	}

	warpH2 := &http2.Transport{
		DisableCompression: true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			KeyLogWriter:       keyLogWriter,
		},
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return dialWarpUTLS(ctx, network, addr, cfg, dialer, keyLogWriter)
		},
	}

	return &warpRoundTripper{
		defaultH1: h1,
		warpH1:    warpH1,
		warpH2:    warpH2,
		log:       log,
	}
}

func (w *warpRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("nil request")
	}
	host := strings.ToLower(strings.TrimSpace(req.URL.Hostname()))
	var rt http.RoundTripper
	if isWarpHost(host) {
		if w.warpH2 != nil {
			rt = w.warpH2
		} else if w.warpH1 != nil {
			rt = w.warpH1
		}
	}
	if rt == nil {
		rt = w.defaultH1
	}
	if rt == nil {
		return nil, errors.New("transport not configured")
	}
	resp, err := rt.RoundTrip(req)
	if err != nil && w.log != nil {
		w.log.Error("upstream failed: " + req.Method + " " + req.URL.Host + req.URL.Path + " err=" + err.Error())
	}
	return resp, err
}

func (w *warpRoundTripper) CloseIdleConnections() {
	if w == nil {
		return
	}
	if w.defaultH1 != nil {
		w.defaultH1.CloseIdleConnections()
	}
	if w.warpH1 != nil {
		w.warpH1.CloseIdleConnections()
	}
	if w.warpH2 != nil {
		w.warpH2.CloseIdleConnections()
	}
}

func isWarpHost(host string) bool {
	if host == "" {
		return false
	}
	return host == "warp.dev" || strings.HasSuffix(host, ".warp.dev")
}

func isMultiAgentRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	path := strings.ToLower(req.URL.Path)
	return strings.HasPrefix(path, "/ai/multi-agent")
}

func dialWarpUTLS(ctx context.Context, network, addr string, cfg *tls.Config, dialer *net.Dialer, keyLogWriter io.Writer) (net.Conn, error) {
	if dialer == nil {
		dialer = newWarpDialer()
	}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	host := hostFromAddr(addr)
	utlsCfg := buildUTLSConfig(cfg, host, keyLogWriter)
	uconn := utls.UClient(rawConn, utlsCfg, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return uconn, nil
}

func buildUTLSConfig(cfg *tls.Config, host string, keyLogWriter io.Writer) *utls.Config {
	out := &utls.Config{
		InsecureSkipVerify: true,
		ServerName:         normalizeServerName(host),
		KeyLogWriter:       keyLogWriter,
		NextProtos:         []string{"h2", "http/1.1"},
	}
	if cfg == nil {
		return out
	}
	if cfg.ServerName != "" {
		out.ServerName = normalizeServerName(cfg.ServerName)
	}
	if len(cfg.NextProtos) > 0 {
		out.NextProtos = append([]string(nil), cfg.NextProtos...)
	}
	if cfg.RootCAs != nil {
		out.RootCAs = cfg.RootCAs
	}
	if cfg.MinVersion != 0 {
		out.MinVersion = cfg.MinVersion
	}
	if cfg.MaxVersion != 0 {
		out.MaxVersion = cfg.MaxVersion
	}
	if cfg.KeyLogWriter != nil {
		out.KeyLogWriter = cfg.KeyLogWriter
	}
	if out.KeyLogWriter == nil {
		out.KeyLogWriter = keyLogWriter
	}
	return out
}

func hostFromAddr(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func normalizeServerName(name string) string {
	name = strings.TrimSpace(strings.Trim(name, "[]"))
	if name == "" {
		return ""
	}
	if net.ParseIP(name) != nil {
		return ""
	}
	return name
}

