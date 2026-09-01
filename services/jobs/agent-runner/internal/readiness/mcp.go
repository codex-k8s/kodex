// Package readiness поднимает рабочий required MCP path до запуска Codex.
package readiness

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	maximumMCPBodyBytes  = 1 << 20
	mcpAuthoritySocket   = "/run/kodex/provider/mcp-authority.sock"
	mcpAuthorityHostName = "kodex-mcp-authority"
)

// MCPProxy — trusted UDS adapter, который проверяет SO_PEERCRED и применяет к
// каждому рабочему запросу тот же exact TLS 1.3/mTLS/bearer path, что и readiness.
type MCPProxy struct {
	server     *http.Server
	transport  *http.Transport
	local      *http.Transport
	done       chan error
	socketPath string
	localToken string
}

func StartMCPProxy(ctx context.Context, input model.Input, token string) (*MCPProxy, error) {
	upstream, err := url.Parse(input.CallbackURL)
	if err != nil || upstream.Scheme != "https" || upstream.Host == "" {
		return nil, errors.New("required MCP endpoint is invalid")
	}
	upstream.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/mcp"
	transport, err := exactMCPTransport(input.CallbackTLS)
	if err != nil {
		return nil, err
	}
	localRaw := make([]byte, 32)
	if _, err := rand.Read(localRaw); err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("generate MCP proxy capability")
	}
	localToken := hex.EncodeToString(localRaw)
	_ = os.Remove(mcpAuthoritySocket)
	listener, err := net.Listen("unix", mcpAuthoritySocket)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("listen on MCP authority socket")
	}
	if os.Chown(mcpAuthoritySocket, -1, 29000) != nil || os.Chmod(mcpAuthoritySocket, 0o660) != nil {
		_ = listener.Close()
		_ = os.Remove(mcpAuthoritySocket)
		transport.CloseIdleConnections()
		return nil, errors.New("protect MCP authority socket")
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
			request.Header.Set("X-Kodex-Run-Ref", input.RunRef)
			request.Header.Set("X-Kodex-Turn-Ref", input.TurnRef)
			request.Header.Set("X-Kodex-Attempt", strconv.FormatUint(uint64(input.Attempt), 10))
			request.Header.Set("X-Kodex-MCP-Binding-Version", strconv.FormatInt(input.RuntimeRevisionVersion, 10))
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
			(request.Method != http.MethodPost && request.Method != http.MethodGet && request.Method != http.MethodDelete) ||
			subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) != 1 {
			http.Error(writer, "invalid MCP proxy request", http.StatusNotFound)
			return
		}
		reverse.ServeHTTP(writer, request)
	})
	done := make(chan error, 1)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	secured := &peerCredentialListener{Listener: listener}
	localTransport := &http.Transport{DisableCompression: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", mcpAuthoritySocket)
	}}
	proxy := &MCPProxy{server: server, transport: transport, local: localTransport, done: done,
		socketPath: mcpAuthoritySocket, localToken: localToken}
	go func() { done <- server.Serve(secured) }()
	localEndpoint, _ := url.Parse("http://" + mcpAuthorityHostName + "/mcp")
	if err := checkMCP(ctx, &http.Client{Transport: localTransport, Timeout: 15 * time.Second}, localEndpoint, localToken); err != nil {
		_ = server.Close()
		localTransport.CloseIdleConnections()
		transport.CloseIdleConnections()
		_ = os.Remove(mcpAuthoritySocket)
		return nil, err
	}
	return proxy, nil
}

func (proxy *MCPProxy) SocketPath() string       { return proxy.socketPath }
func (proxy *MCPProxy) LocalBearerToken() string { return proxy.localToken }

func (proxy *MCPProxy) Close(ctx context.Context) error {
	err := proxy.server.Shutdown(ctx)
	if err != nil {
		_ = proxy.server.Close()
	}
	proxy.local.CloseIdleConnections()
	proxy.transport.CloseIdleConnections()
	_ = os.Remove(proxy.socketPath)
	var serveErr error
	select {
	case serveErr = <-proxy.done:
	case <-ctx.Done():
		return errors.New("shutdown MCP authority proxy")
	}
	if err != nil {
		return errors.New("shutdown MCP authority proxy")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("MCP authority proxy failed")
	}
	return nil
}

type peerCredentialListener struct {
	net.Listener
}

func (listener *peerCredentialListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConnection, ok := connection.(*net.UnixConn)
		if !ok || !allowedMCPPeer(unixConnection) {
			_ = connection.Close()
			continue
		}
		return connection, nil
	}
}

func allowedMCPPeer(connection *net.UnixConn) bool {
	raw, err := connection.SyscallConn()
	if err != nil {
		return false
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credential == nil {
		return false
	}
	return credential.Uid == 10001 || credential.Uid == 10002
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
	initialize := []byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"kodex-agent-runner","version":"1"}}}`)
	raw, mediaType, statusCode, err := postMCP(ctx, client, endpoint, token, initialize)
	if err != nil || len(raw) == 0 || statusCode != http.StatusOK || mediaType != "application/json" {
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

	initialized := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	raw, _, statusCode, err = postMCP(ctx, client, endpoint, token, initialized)
	if err != nil || statusCode != http.StatusAccepted || len(raw) != 0 {
		return errors.New("required MCP initialized notification failed")
	}

	list := []byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","method":"tools/list","params":{}}`)
	raw, mediaType, statusCode, err = postMCP(ctx, client, endpoint, token, list)
	if err != nil || len(raw) == 0 || statusCode != http.StatusOK || mediaType != "application/json" {
		return errors.New("required MCP tool catalog is unavailable")
	}
	var catalog struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Result  struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&catalog) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		catalog.JSONRPC != "2.0" || catalog.ID != "agent-runner-tools" ||
		len(catalog.Error) != 0 || len(catalog.Result.Tools) == 0 || len(catalog.Result.Tools) > 256 {
		return errors.New("required MCP tool catalog is invalid")
	}
	return nil
}

func postMCP(ctx context.Context, client *http.Client, endpoint *url.URL, token string, payload []byte) ([]byte, string, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, "", 0, errors.New("create MCP readiness request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return nil, "", 0, errors.New("required MCP path is unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumMCPBodyBytes+1))
	if err != nil || len(raw) > maximumMCPBodyBytes {
		return nil, "", 0, errors.New("read required MCP response")
	}
	mediaType := ""
	if value := response.Header.Get("Content-Type"); value != "" {
		parsed, _, mediaErr := mime.ParseMediaType(value)
		if mediaErr != nil {
			return nil, "", 0, errors.New("required MCP response media type is invalid")
		}
		mediaType = parsed
	}
	return raw, mediaType, response.StatusCode, nil
}
