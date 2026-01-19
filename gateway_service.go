package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lqqyt2423/go-mitmproxy/addon"
	mitmproxy "github.com/lqqyt2423/go-mitmproxy/proxy"
)

const gatewayStreamLimit = 5 * 1024 * 1024

type GatewayService struct {
	app         *App
	proxy       *mitmproxy.Proxy
	interceptor *mitmInterceptor
	port        int
	startedAt   time.Time
	errCh       chan error

	mu      sync.Mutex
	running bool
}

func newGatewayService(app *App, port int) (*GatewayService, error) {
	if app == nil {
		return nil, errors.New("app not ready")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	p := &mitmproxy.Proxy{
		Version:           "0.2.0",
		StreamLargeBodies: gatewayStreamLimit,
		Addons:            make([]addon.Addon, 0),
	}

	p.Server = &http.Server{
		Addr:        addr,
		Handler:     p,
		IdleTimeout: 5 * time.Second,
	}

	dialer := newWarpDialer()
	transport := &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: dialer.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       5 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			KeyLogWriter:       mitmproxy.GetTlsKeyLogWriter(),
		},
	}

	p.Client = &http.Client{
		Transport: newWarpRoundTripper(transport, dialer, mitmproxy.GetTlsKeyLogWriter(), app.log),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	interceptor, err := newMitmInterceptor(p, mitmproxyDir())
	if err != nil {
		return nil, err
	}
	p.Interceptor = interceptor

	accountMgr := NewGatewayAccountManager(app.paths.AccountsFile, app.log, func(prev string, current *Account, reason string) {
		if current == nil {
			return
		}
		if prev == "" && current.Email == "" {
			return
		}
		if app.consumeMCPSyncSkip(current.Email) {
			return
		}
		_ = app.updateConfig(func(cfg *LocalConfig) {
			cfg.SwitchCount += 1
		})
		var prevAcc *Account
		if strings.TrimSpace(prev) != "" {
			prevAcc = &Account{Email: prev}
		}
		if err := switchAccountWithMCP(prevAcc, current, app.log); err != nil {
			if app.log != nil {
				app.log.Warn("switch account with MCP sync failed: " + err.Error())
			}
			_ = updateWarpCredentialsWithLog(*current, app.log, reason)
		}
	})
	if snapshot, ok := app.getMemorySnapshot(); ok {
		accountMgr.LoadSnapshot(snapshot, true)
	}

	warpAddon := NewWarpAddon(app, accountMgr)
	p.Addons = append(p.Addons, warpAddon)

	return &GatewayService{
		app:         app,
		proxy:       p,
		interceptor: interceptor,
		port:        port,
	}, nil
}

func (g *GatewayService) Start() error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return errors.New("gateway already running")
	}
	g.running = true
	g.startedAt = time.Now()
	g.errCh = make(chan error, 2)
	g.mu.Unlock()

	go func() {
		err := g.proxy.Server.ListenAndServe()
		g.errCh <- err
	}()

	go func() {
		err := g.interceptor.Start()
		g.errCh <- err
	}()

	if !waitForPort(g.port, 15*time.Second) {
		_ = g.Stop()
		return errors.New("gateway start timeout")
	}

	go g.watchErrors()
	return nil
}

func (g *GatewayService) watchErrors() {
	for {
		select {
		case err := <-g.errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) && g.app != nil {
				g.app.log.Error("gateway stopped: " + err.Error())
			}
			g.mu.Lock()
			g.running = false
			g.mu.Unlock()
			return
		}
	}
}

func (g *GatewayService) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = false
	g.mu.Unlock()

	if g.proxy != nil && g.proxy.Server != nil {
		_ = g.proxy.Server.Close()
	}
	if g.interceptor != nil {
		_ = g.interceptor.Close()
	}
	if g.proxy != nil && g.proxy.Client != nil {
		if closer, ok := g.proxy.Client.Transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
	return nil
}

func (g *GatewayService) Running() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

func (g *GatewayService) StartedAt() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.startedAt
}
