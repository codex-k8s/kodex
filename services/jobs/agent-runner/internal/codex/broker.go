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
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/credentialrelay"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	"golang.org/x/sys/unix"
)

const (
	ProviderSocketPath           = "/run/kodex/provider/provider.sock"
	maximumBrokerBytes           = 4 << 20
	providerRefreshCommitTimeout = 40 * time.Second
	providerSandboxProbeTimeout  = 5 * time.Second
	providerResultDeliveryGrace  = processGrace + terminationGrace + providerRefreshCommitTimeout + 5*time.Second
)

var (
	ErrProviderAuthentication        = errors.New("Codex provider authentication is unavailable")
	errProviderBrokerResponseInvalid = errors.New("isolated Codex provider response is invalid")
	errProviderBrokerFailed          = errors.New("isolated Codex provider failed")
)

type brokerRequest struct {
	Input         model.Input `json:"input"`
	Prompt        []byte      `json:"prompt"`
	MCPSocket     string      `json:"mcp_socket"`
	MCPProxyToken string      `json:"mcp_proxy_token"`
}

type brokerResponse struct {
	Result  Result                `json:"result"`
	Failure providerBrokerFailure `json:"failure,omitempty"`
	OK      bool                  `json:"ok"`
}

type providerBrokerFailure string

const (
	providerBrokerFailureAuthentication providerBrokerFailure = "AUTHENTICATION"
	providerBrokerFailureAuthority      providerBrokerFailure = "AUTHORITY"
	providerBrokerFailureMCP            providerBrokerFailure = "MCP"
	providerBrokerFailureConfiguration  providerBrokerFailure = "CONFIGURATION"
	providerBrokerFailureProvider       providerBrokerFailure = "PROVIDER"
)

type providerAuthenticationSnapshot struct {
	AuthMode     string          `json:"auth_mode"`
	OpenAIAPIKey *string         `json:"OPENAI_API_KEY"`
	Tokens       json.RawMessage `json:"tokens"`
}

type providerExecutor func(context.Context, model.Input, []byte, string) (Result, error)
type providerCredentialRefreshCommitter func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error

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
	if validateProviderAuthentication(auth) != nil {
		return ErrProviderAuthentication
	}
	digest := sha256.Sum256(auth)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return ErrProviderAuthentication
	}
	return nil
}

func validateProviderAuthentication(auth []byte) error {
	_, err := providerAuthenticationMode(auth)
	return err
}

func providerAuthenticationMode(auth []byte) (string, error) {
	trimmed := bytes.TrimSpace(auth)
	if len(auth) == 0 || len(auth) > 1<<20 || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return "", ErrProviderAuthentication
	}
	var snapshot providerAuthenticationSnapshot
	if json.Unmarshal(trimmed, &snapshot) != nil || !supportedProviderAuthentication(snapshot.AuthMode, snapshot.OpenAIAPIKey, snapshot.Tokens) {
		return "", ErrProviderAuthentication
	}
	return snapshot.AuthMode, nil
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
	stopContext := bindBrokerConnectionContext(ctx, connection)
	defer stopContext()
	encoder := json.NewEncoder(connection)
	if err := encoder.Encode(brokerRequest{Input: input, Prompt: prompt,
		MCPSocket: mcpSocket, MCPProxyToken: mcpProxyToken}); err != nil {
		return Result{}, errors.New("send isolated Codex provider request")
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok || unixConnection.CloseWrite() != nil {
		return Result{}, errors.New("seal isolated Codex provider request")
	}
	return readProviderBrokerResponse(connection)
}

// После отмены больше не посылаем request, но даём изолированному процессу
// ограниченное время остановиться, завершить credential callback и вернуть Usage.
func bindBrokerConnectionContext(ctx context.Context, connection net.Conn) func() {
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetWriteDeadline(deadline)
		_ = connection.SetReadDeadline(deadline.Add(providerResultDeliveryGrace))
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = connection.SetWriteDeadline(time.Now())
		_ = connection.SetReadDeadline(time.Now().Add(providerResultDeliveryGrace))
	})
	return func() {
		if !stop() {
			<-done
		}
	}
}

func readProviderBrokerResponse(reader io.Reader) (Result, error) {
	decoder := json.NewDecoder(bufio.NewReaderSize(&boundedReader{reader: reader, remaining: maximumBrokerBytes}, 64<<10))
	decoder.DisallowUnknownFields()
	var response brokerResponse
	if err := decoder.Decode(&response); err != nil || !decodeEOF(decoder) {
		return Result{}, errProviderBrokerFailed
	}
	if !response.OK {
		switch response.Failure {
		case providerBrokerFailureAuthentication, providerBrokerFailureAuthority, providerBrokerFailureMCP,
			providerBrokerFailureConfiguration, providerBrokerFailureProvider:
		default:
			return Result{}, errProviderBrokerResponseInvalid
		}
		if response.Result.Usage.Validate() != nil || len(response.Result.ToolCalls) > runtimecontract.MaximumNativeToolCalls {
			return Result{}, errProviderBrokerResponseInvalid
		}
		for _, call := range response.Result.ToolCalls {
			if call.Validate() != nil {
				return Result{}, errProviderBrokerResponseInvalid
			}
		}
		return failedProviderResult(response.Result), providerBrokerError(response.Failure)
	}
	if response.Failure != "" {
		return Result{}, errProviderBrokerResponseInvalid
	}
	return response.Result, nil
}

func providerBrokerError(failure providerBrokerFailure) error {
	switch failure {
	case providerBrokerFailureAuthentication:
		return ErrProviderAuthentication
	case providerBrokerFailureAuthority:
		return ErrAuthorityRequestUnsupported
	case providerBrokerFailureMCP:
		return ErrRequiredMCPUnavailable
	case providerBrokerFailureConfiguration:
		return ErrRuntimeProfile
	case providerBrokerFailureProvider:
		return errProviderBrokerFailed
	default:
		return errProviderBrokerResponseInvalid
	}
}

func classifyProviderBrokerFailure(err error) providerBrokerFailure {
	switch {
	case errors.Is(err, ErrProviderAuthentication):
		return providerBrokerFailureAuthentication
	case errors.Is(err, ErrAuthorityRequestUnsupported):
		return providerBrokerFailureAuthority
	case errors.Is(err, ErrRequiredMCPUnavailable):
		return providerBrokerFailureMCP
	case errors.Is(err, ErrRuntimeProfile):
		return providerBrokerFailureConfiguration
	default:
		return providerBrokerFailureProvider
	}
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
	if err := verifyProviderSandbox(ctx, func(command *exec.Cmd) error { return command.Run() }); err != nil {
		return err
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

func verifyProviderSandbox(ctx context.Context, execute func(*exec.Cmd) error) error {
	if execute == nil {
		return errors.New("Codex provider sandbox probe is unavailable")
	}
	probeContext, cancel := context.WithTimeout(ctx, providerSandboxProbeTimeout)
	defer cancel()
	uid := strconv.Itoa(os.Geteuid())
	command := exec.CommandContext(probeContext, "/usr/bin/bwrap", "--unshare-user", "--uid", uid, "--gid", uid,
		"--ro-bind", "/", "/", "/usr/bin/true")
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	command.Stdin, command.Stdout, command.Stderr = nil, io.Discard, io.Discard
	if err := execute(command); err != nil {
		return errors.New("Codex provider sandbox is unavailable")
	}
	return nil
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
	if err := ValidateRuntimeProfile(request.Input); err != nil {
		return writeProviderBrokerFailure(connection, err)
	}
	snapshot, err := request.Input.RequiredContextSnapshot(time.Now())
	if err != nil || verifyProviderContext(request.Input, snapshot) != nil {
		return writeProviderBrokerFailure(connection, ErrRuntimeProfile)
	}
	ctx, cancelContext := snapshot.BoundExecutionContext(ctx)
	defer cancelContext()
	auth, err := readProviderAuthentication(request.Input)
	if err != nil {
		return writeProviderBrokerFailure(connection, err)
	}
	defer clear(auth)
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
	bridge, err := startProviderMCPBridge(ctx, request.MCPSocket, request.MCPProxyToken, request.Input)
	if err != nil {
		return writeProviderBrokerFailure(connection, err)
	}
	defer bridge.Close()
	if err := PrepareHomeWithAuth(request.Input, bridge.URL(), auth); err != nil {
		return writeProviderBrokerFailure(connection, err)
	}
	result, err := executeProviderTurn(ctx, request.Input, request.Prompt, request.MCPProxyToken, executeLocal, credentialrelay.Commit)
	if err != nil {
		return writeProviderBrokerResultFailure(connection, result, err)
	}
	if result.Outcome != "SUCCEEDED" {
		log.Printf("Codex provider turn completed with safe failure code: %s", result.FailureCode)
	}
	return json.NewEncoder(connection).Encode(brokerResponse{Result: result, OK: true})
}

func writeProviderBrokerFailure(connection io.Writer, err error) error {
	return writeProviderBrokerResultFailure(connection, Result{}, err)
}

// Ошибка не подтверждает итог или credential effect. Измеренный расход и
// безопасная native timeline сохраняются независимо от этого исхода.
func failedProviderResult(result Result) Result {
	return Result{Usage: result.Usage, ToolCalls: result.ToolCalls}
}

func writeProviderBrokerResultFailure(connection io.Writer, result Result, err error) error {
	return json.NewEncoder(connection).Encode(brokerResponse{
		Result:  failedProviderResult(result),
		Failure: classifyProviderBrokerFailure(err),
		OK:      false,
	})
}

func executeProviderTurn(ctx context.Context, input model.Input, prompt []byte, mcpProxyToken string,
	execute providerExecutor, commit providerCredentialRefreshCommitter,
) (Result, error) {
	authenticationPath := filepath.Join(input.CodexHome, "auth.json")
	defer os.Remove(authenticationPath)
	result, executionErr := execute(ctx, input, prompt, mcpProxyToken)
	authentication, changed, err := readProviderCredentialRefresh(input, authenticationPath)
	if err != nil {
		return result, err
	}
	defer clear(authentication)
	if changed {
		payload := runtimecontract.RunnerProviderCredentialRefreshRequest{
			RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
			PreviousCredentialRevisionRef: input.ProviderCredentialRef,
			PreviousContentSHA256:         input.ProviderCredentialSHA256,
			Authentication:                authentication,
		}
		commitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerRefreshCommitTimeout)
		defer cancel()
		if err := commit(commitContext, input, payload); err != nil {
			return result, errors.New("commit refreshed provider authentication")
		}
	}
	return result, executionErr
}

func readProviderCredentialRefresh(input model.Input, path string) ([]byte, bool, error) {
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return nil, false, errors.New("read refreshed provider authentication")
	}
	defer file.Close()
	if info.Size() <= 0 || info.Size() > runtimecontract.MaximumProviderAuthBytes {
		return nil, false, errors.New("refreshed provider authentication metadata is invalid")
	}
	authentication, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumProviderAuthBytes+1))
	if err != nil || int64(len(authentication)) != info.Size() || len(authentication) > runtimecontract.MaximumProviderAuthBytes {
		clear(authentication)
		return nil, false, errors.New("refreshed provider authentication content is invalid")
	}
	mode, modeErr := providerAuthenticationMode(authentication)
	if modeErr != nil {
		clear(authentication)
		return nil, false, errors.New("refreshed provider authentication is invalid")
	}
	digest := sha256.Sum256(authentication)
	changed := hex.EncodeToString(digest[:]) != input.ProviderCredentialSHA256
	if changed && mode == "apikey" {
		clear(authentication)
		return nil, false, errors.New("provider API-key authentication changed unexpectedly")
	}
	return authentication, changed, nil
}

type providerMCPBridge struct {
	server    *http.Server
	transport *http.Transport
	done      chan error
	url       string
}

func startProviderMCPBridge(ctx context.Context, socketPath, localToken string, input model.Input) (*providerMCPBridge, error) {
	if os.Geteuid() != 10002 || socketPath != "/run/kodex/provider/mcp-authority.sock" || len(localToken) != 64 {
		return nil, errors.New("provider MCP bridge binding is invalid")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("listen provider MCP bridge")
	}
	transport := &http.Transport{DisableCompression: true, MaxResponseHeaderBytes: 16 << 10, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	target, _ := url.Parse("http://kodex-mcp-authority/mcp")
	reverse := &httputil.ReverseProxy{Transport: transport, ErrorLog: log.New(io.Discard, "", 0),
		Director: func(request *http.Request) {
			request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
			request.Host = target.Host
			request.Header.Del("Cookie")
			request.Header.Del("Forwarded")
			request.Header.Del("X-Forwarded-For")
			request.Header.Del("X-Forwarded-Host")
			request.Header.Del("X-Forwarded-Proto")
		}, ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "required MCP authority is unavailable", http.StatusBadGateway)
		}, FlushInterval: -1}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path != "/mcp" && subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) == 1 {
			if _, err := filetransfer.CatalogRequest(input, request); err != nil {
				http.NotFound(writer, request)
				return
			}
			bounded, cancel := context.WithTimeout(request.Context(), filetransfer.TotalTimeout)
			defer cancel()
			control := http.NewResponseController(writer)
			deadline, _ := bounded.Deadline()
			if err := control.SetWriteDeadline(deadline); err != nil {
				http.Error(writer, "runtime file response deadline is unavailable", http.StatusServiceUnavailable)
				return
			}
			defer control.SetWriteDeadline(time.Time{})
			reverse.ServeHTTP(writer, request.WithContext(bounded))
			return
		}
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
	reader    io.Reader
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
