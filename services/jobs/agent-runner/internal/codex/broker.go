package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	"golang.org/x/sys/unix"
)

const (
	ProviderSocketPath = "/run/kodex/provider/provider.sock"
	maximumBrokerBytes = 4 << 20
)

var ErrProviderAuthentication = errors.New("Codex provider authentication is unavailable")

type brokerRequest struct {
	Input         model.Input `json:"input"`
	Prompt        []byte      `json:"prompt"`
	MCPSocket     string      `json:"mcp_socket"`
	MCPProxyToken string      `json:"mcp_proxy_token"`
}

type brokerResponse struct {
	Result Result `json:"result"`
	OK     bool   `json:"ok"`
}

// ExecuteViaBroker передаёт provider-only данные отдельному UID по UDS.
// Ни app-server, ни запускаемый им shell не получают authority mounts runner.
func ExecuteViaBroker(ctx context.Context, input model.Input, prompt []byte, mcpSocket, mcpProxyToken string) (Result, error) {
	return executeViaBroker(ctx, input, prompt, mcpSocket, mcpProxyToken)
}

func readProviderAuthentication(input model.Input) ([]byte, error) {
	if err := security.VerifyProtectedRegular(input.ProviderAuthFile, false); err != nil {
		return nil, ErrProviderAuthentication
	}
	auth, err := os.ReadFile(input.ProviderAuthFile)
	expectedDigest, digestErr := pinnedProviderDigest(input)
	if err != nil || digestErr != nil || validateProviderAuthenticationPayload(auth, expectedDigest) != nil {
		return nil, ErrProviderAuthentication
	}
	return auth, nil
}

func validateProviderAuthenticationPayload(auth []byte, expectedSHA256 string) error {
	trimmed := bytes.TrimSpace(auth)
	if len(auth) == 0 || len(auth) > 1<<20 || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return ErrProviderAuthentication
	}
	var snapshot struct {
		AuthMode     string          `json:"auth_mode"`
		OpenAIAPIKey *string         `json:"OPENAI_API_KEY"`
		Tokens       json.RawMessage `json:"tokens"`
	}
	if json.Unmarshal(trimmed, &snapshot) != nil || !supportedProviderAuthentication(snapshot.AuthMode, snapshot.OpenAIAPIKey, snapshot.Tokens) {
		return ErrProviderAuthentication
	}
	digest := sha256.Sum256(auth)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return ErrProviderAuthentication
	}
	return nil
}

func supportedProviderAuthentication(mode string, apiKey *string, tokens json.RawMessage) bool {
	switch mode {
	case "chatgpt", "chatgptAuthTokens":
		var value map[string]json.RawMessage
		return len(tokens) > 0 && json.Unmarshal(tokens, &value) == nil && len(value) > 0
	case "apikey":
		return apiKey != nil && *apiKey != ""
	default:
		return false
	}
}

func executeViaBroker(ctx context.Context, input model.Input, prompt []byte, mcpSocket, mcpProxyToken string) (Result, error) {
	dialer := net.Dialer{}
	var connection net.Conn
	var err error
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for connection == nil {
		connection, err = dialer.DialContext(ctx, "unix", ProviderSocketPath)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, context.Canceled
		case <-deadline.C:
			return Result{}, errors.New("connect isolated Codex provider broker")
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer connection.Close()
	encoder := json.NewEncoder(connection)
	if err := encoder.Encode(brokerRequest{Input: input, Prompt: prompt,
		MCPSocket: mcpSocket, MCPProxyToken: mcpProxyToken}); err != nil {
		return Result{}, errors.New("send isolated Codex provider request")
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok || unixConnection.CloseWrite() != nil {
		return Result{}, errors.New("seal isolated Codex provider request")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(connection, 64<<10))
	decoder.DisallowUnknownFields()
	var response brokerResponse
	if err := decoder.Decode(&response); err != nil || !decodeEOF(decoder) || !response.OK {
		return Result{}, errors.New("isolated Codex provider failed")
	}
	return response.Result, nil
}

// ServeProviderBroker запускается только в container UID 10002 без Kubernetes
// token, application grants, mTLS keys, MCP bearer и handoff signing key.
func ServeProviderBroker(ctx context.Context) error {
	if os.Geteuid() != 10002 {
		return errors.New("Codex provider broker UID is invalid")
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return errors.New("disable provider broker process inspection")
	}
	if err := os.MkdirAll(filepath.Dir(ProviderSocketPath), 0o770); err != nil {
		return errors.New("create provider broker socket directory")
	}
	_ = os.Remove(ProviderSocketPath)
	listener, err := net.Listen("unix", ProviderSocketPath)
	if err != nil {
		return errors.New("listen isolated Codex provider broker")
	}
	defer listener.Close()
	if err := os.Chown(ProviderSocketPath, -1, 29000); err != nil || os.Chmod(ProviderSocketPath, 0o660) != nil {
		return errors.New("protect provider broker socket")
	}
	go func() { <-ctx.Done(); _ = listener.Close() }()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("accept isolated Codex provider request")
		}
		if err := serveBrokerRequest(ctx, connection); err != nil {
			log.Printf("Codex provider request failed: %v", err)
			_ = connection.Close()
			continue
		}
		_ = connection.Close()
	}
}

func serveBrokerRequest(ctx context.Context, connection net.Conn) error {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("provider broker transport is invalid")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return errors.New("inspect provider broker peer")
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credential == nil || credential.Uid != 10001 {
		return errors.New("provider broker peer is unauthorized")
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(&boundedReader{reader: connection, remaining: maximumBrokerBytes}, 64<<10))
	decoder.DisallowUnknownFields()
	var request brokerRequest
	if decoder.Decode(&request) != nil || !decodeEOF(decoder) || request.Input.Validate() != nil ||
		len(request.Prompt) == 0 || len(request.Prompt) > 1<<20 {
		return errors.New("provider broker request is invalid")
	}
	auth, err := readProviderAuthentication(request.Input)
	if err != nil {
		return err
	}
	expectedDigest, err := pinnedProviderDigest(request.Input)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(auth)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return errors.New("provider broker account pin mismatch")
	}
	if request.MCPSocket != "/run/kodex/provider/mcp-authority.sock" || len(request.MCPProxyToken) != 64 {
		return errors.New("provider broker MCP binding is invalid")
	}
	if _, err := hex.DecodeString(request.MCPProxyToken); err != nil {
		return errors.New("provider broker MCP capability is invalid")
	}
	bridge, err := startProviderMCPBridge(ctx, request.MCPSocket, request.MCPProxyToken)
	if err != nil {
		return err
	}
	defer bridge.Close()
	if err := PrepareHomeWithAuth(request.Input, bridge.URL(), auth); err != nil {
		return err
	}
	defer os.Remove(filepath.Join(request.Input.CodexHome, "auth.json"))
	result, err := executeLocal(ctx, request.Input, request.Prompt, request.MCPProxyToken)
	if err != nil {
		return err
	}
	if result.Outcome != "SUCCEEDED" {
		log.Printf("Codex provider turn completed with safe failure code: %s", result.FailureCode)
	}
	return json.NewEncoder(connection).Encode(brokerResponse{Result: result, OK: true})
}

type providerMCPBridge struct {
	server    *http.Server
	transport *http.Transport
	done      chan error
	url       string
}

func startProviderMCPBridge(ctx context.Context, socketPath, localToken string) (*providerMCPBridge, error) {
	if os.Geteuid() != 10002 || socketPath != "/run/kodex/provider/mcp-authority.sock" || len(localToken) != 64 {
		return nil, errors.New("provider MCP bridge binding is invalid")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("listen provider MCP bridge")
	}
	transport := &http.Transport{DisableCompression: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	target, _ := url.Parse("http://kodex-mcp-authority/mcp")
	reverse := &httputil.ReverseProxy{Transport: transport, ErrorLog: log.New(io.Discard, "", 0),
		Director: func(request *http.Request) {
			request.URL.Scheme, request.URL.Host, request.URL.Path = target.Scheme, target.Host, target.Path
			request.URL.RawPath, request.URL.RawQuery, request.Host = "", "", target.Host
			request.Header.Del("Cookie")
			request.Header.Del("Forwarded")
			request.Header.Del("X-Forwarded-For")
			request.Header.Del("X-Forwarded-Host")
			request.Header.Del("X-Forwarded-Proto")
		}, ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "required MCP authority is unavailable", http.StatusBadGateway)
		}, FlushInterval: -1}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp" || request.URL.RawQuery != "" ||
			(request.Method != http.MethodPost && request.Method != http.MethodGet && request.Method != http.MethodDelete) ||
			subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) != 1 {
			http.Error(writer, "invalid provider MCP request", http.StatusNotFound)
			return
		}
		reverse.ServeHTTP(writer, request)
	})
	done := make(chan error, 1)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	bridge := &providerMCPBridge{server: server, transport: transport, done: done,
		url: "http://" + listener.Addr().String() + "/mcp"}
	go func() { done <- server.Serve(listener) }()
	return bridge, nil
}

func (bridge *providerMCPBridge) URL() string { return bridge.url }

func (bridge *providerMCPBridge) Close() {
	_ = bridge.server.Close()
	bridge.transport.CloseIdleConnections()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-bridge.done:
	case <-timer.C:
	}
}

func decodeEOF(decoder *json.Decoder) bool {
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

type boundedReader struct {
	reader    net.Conn
	remaining int64
}

func (reader *boundedReader) Read(value []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, errors.New("provider broker request exceeded its bound")
	}
	if int64(len(value)) > reader.remaining {
		value = value[:reader.remaining]
	}
	count, err := reader.reader.Read(value)
	reader.remaining -= int64(count)
	return count, err
}
