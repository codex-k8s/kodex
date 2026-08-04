// Package readiness поднимает рабочий required MCP path до запуска Codex.
package readiness

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
)

const maximumMCPBodyBytes = 1 << 20

// MCPProxy — loopback-only adapter, который применяет к каждому рабочему
// запросу Codex тот же exact TLS 1.3/mTLS/bearer path, что и readiness.
type MCPProxy struct {
	server    *http.Server
	transport *http.Transport
	done      chan error
	url       string
}

func StartMCPProxy(ctx context.Context, input model.Input, token string) (*MCPProxy, error) {
	upstream, err := url.Parse(input.MCP.URL)
	if err != nil || upstream.Scheme != "https" || upstream.Host == "" {
		return nil, errors.New("required MCP endpoint is invalid")
	}
	transport, err := exactMCPTransport(input.MCP.TLS)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	if err := checkMCP(ctx, client, upstream, token); err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("listen on MCP loopback proxy")
	}
	reverse := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = upstream.Scheme
			request.URL.Host = upstream.Host
			request.URL.Path = upstream.Path
			request.URL.RawPath = upstream.RawPath
			request.URL.RawQuery = upstream.RawQuery
			request.Host = upstream.Host
			request.Header.Del("Cookie")
			request.Header.Del("Forwarded")
			request.Header.Del("X-Forwarded-For")
			request.Header.Del("X-Forwarded-Host")
			request.Header.Del("X-Forwarded-Proto")
			request.Header.Set("Authorization", "Bearer "+token)
		},
		Transport: transport,
		ErrorLog:  log.New(io.Discard, "", 0),
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "required MCP upstream is unavailable", http.StatusBadGateway)
		},
		FlushInterval: -1,
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp" || request.URL.RawQuery != "" ||
			(request.Method != http.MethodPost && request.Method != http.MethodGet && request.Method != http.MethodDelete) {
			http.Error(writer, "invalid MCP proxy request", http.StatusNotFound)
			return
		}
		reverse.ServeHTTP(writer, request)
	})
	done := make(chan error, 1)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	proxy := &MCPProxy{server: server, transport: transport, done: done,
		url: "http://" + listener.Addr().String() + "/mcp"}
	go func() { done <- server.Serve(listener) }()
	return proxy, nil
}

func (proxy *MCPProxy) URL() string { return proxy.url }

func (proxy *MCPProxy) Close(ctx context.Context) error {
	err := proxy.server.Shutdown(ctx)
	if err != nil {
		_ = proxy.server.Close()
	}
	proxy.transport.CloseIdleConnections()
	var serveErr error
	select {
	case serveErr = <-proxy.done:
	case <-ctx.Done():
		return errors.New("shutdown MCP loopback proxy")
	}
	if err != nil {
		return errors.New("shutdown MCP loopback proxy")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("MCP loopback proxy failed")
	}
	return nil
}

func exactMCPTransport(binding model.TLSBinding) (*http.Transport, error) {
	ca, err := os.ReadFile(binding.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read MCP CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse MCP CA")
	}
	certificate, err := tls.LoadX509KeyPair(binding.CertificateFile, binding.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load MCP client identity")
	}
	return &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13, ServerName: binding.ServerName, RootCAs: roots,
		Certificates: []tls.Certificate{certificate}}, DisableCompression: true,
		MaxIdleConns: 2, MaxIdleConnsPerHost: 2, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10}, nil
}

func checkMCP(ctx context.Context, client *http.Client, endpoint *url.URL, token string) error {
	payload := []byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mattercodex-agent-runner","version":"1"}}}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return errors.New("create MCP readiness request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return errors.New("required MCP path is unavailable")
	}
	defer response.Body.Close()
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumMCPBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumMCPBodyBytes || response.StatusCode != http.StatusOK ||
		mediaErr != nil || mediaType != "application/json" {
		return errors.New("required MCP readiness response is invalid")
	}
	var result struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		result.JSONRPC != "2.0" || result.ID != "agent-runner-readiness" ||
		len(result.Result) == 0 || len(result.Error) != 0 || strings.TrimSpace(string(result.Result)) == "null" {
		return errors.New("required MCP initialization failed")
	}
	return nil
}
