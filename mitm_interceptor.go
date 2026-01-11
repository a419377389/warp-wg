package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lqqyt2423/go-mitmproxy/cert"
	mitmproxy "github.com/lqqyt2423/go-mitmproxy/proxy"
	"golang.org/x/net/http2"
)

type mitmListener struct {
	connChan chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

func newMitmListener() *mitmListener {
	return &mitmListener{
		connChan: make(chan net.Conn),
		closed:   make(chan struct{}),
	}
}

func (l *mitmListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case conn, ok := <-l.connChan:
		if !ok || conn == nil {
			return nil, net.ErrClosed
		}
		return conn, nil
	}
}

func (l *mitmListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
		close(l.connChan)
	})
	return nil
}

func (l *mitmListener) Addr() net.Addr {
	return mitmAddr("mitm")
}

func (l *mitmListener) send(conn net.Conn) bool {
	select {
	case <-l.closed:
		return false
	default:
	}
	select {
	case l.connChan <- conn:
		return true
	case <-l.closed:
		return false
	}
}

type mitmAddr string

func (a mitmAddr) Network() string { return "mitm" }
func (a mitmAddr) String() string  { return string(a) }

type connBuf struct {
	net.Conn
	r          *bufio.Reader
	host       string
	remoteAddr string
}

func newConnBuf(c net.Conn, req *http.Request) *connBuf {
	return &connBuf{
		Conn:       c,
		r:          bufio.NewReader(c),
		host:       req.Host,
		remoteAddr: req.RemoteAddr,
	}
}

func (b *connBuf) Peek(n int) ([]byte, error) {
	return b.r.Peek(n)
}

func (b *connBuf) Read(data []byte) (int, error) {
	return b.r.Read(data)
}

func (b *connBuf) RemoteAddr() net.Addr {
	return mitmAddr(b.remoteAddr)
}

func newPipes(req *http.Request) (net.Conn, *connBuf) {
	client, server := net.Pipe()
	return client, newConnBuf(server, req)
}

type mitmInterceptor struct {
	proxy    *mitmproxy.Proxy
	ca       *cert.CA
	listener *mitmListener
	server   *http.Server
	closed   chan struct{}
	once     sync.Once
}

func newMitmInterceptor(p *mitmproxy.Proxy, caPath string) (*mitmInterceptor, error) {
	ca, err := cert.NewCA(caPath)
	if err != nil {
		return nil, err
	}

	interceptor := &mitmInterceptor{
		proxy:    p,
		ca:       ca,
		listener: newMitmListener(),
		closed:   make(chan struct{}),
	}

	server := &http.Server{
		Handler:     interceptor,
		IdleTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
				if chi == nil {
					return nil, errors.New("missing client hello")
				}
				return ca.GetCert(chi.ServerName)
			},
		},
	}
	_ = http2.ConfigureServer(server, &http2.Server{})

	interceptor.server = server
	return interceptor, nil
}

func (m *mitmInterceptor) Start() error {
	return m.server.ServeTLS(m.listener, "", "")
}

func (m *mitmInterceptor) Close() error {
	m.once.Do(func() {
		close(m.closed)
		if m.listener != nil {
			_ = m.listener.Close()
		}
		if m.server != nil {
			_ = m.server.Close()
		}
	})
	return nil
}

func (m *mitmInterceptor) Dial(req *http.Request) (net.Conn, error) {
	select {
	case <-m.closed:
		return nil, net.ErrClosed
	default:
	}
	clientConn, serverConn := newPipes(req)
	go m.intercept(serverConn)
	return clientConn, nil
}

func (m *mitmInterceptor) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	if strings.EqualFold(req.Header.Get("Connection"), "Upgrade") && strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		mitmproxy.DefaultWebSocket.WSS(res, req)
		return
	}

	if req.URL.Scheme == "" {
		req.URL.Scheme = "https"
	}
	if req.URL.Host == "" {
		req.URL.Host = req.Host
	}
	m.proxy.ServeHTTP(res, req)
}

func (m *mitmInterceptor) intercept(serverConn *connBuf) {
	if serverConn == nil {
		return
	}

	buf, err := serverConn.Peek(3)
	if err != nil || len(buf) < 3 {
		_ = serverConn.Close()
		return
	}

	isTLS := buf[0] == 0x16 && buf[1] == 0x03 && buf[2] <= 0x03
	if isTLS {
		if !m.listener.send(serverConn) {
			_ = serverConn.Close()
			return
		}
		return
	}

	mitmproxy.DefaultWebSocket.WS(serverConn, serverConn.host)
}
