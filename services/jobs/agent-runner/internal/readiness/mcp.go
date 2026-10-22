// Package readiness prepares and checks the required MCP path before provider startup.
// Пакет readiness готовит и проверяет обязательный MCP-путь до запуска провайдера.
package readiness

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// maximumMCPBodyBytes caps each buffered readiness response at one MiB, using one extra byte to detect overflow.
	// maximumMCPBodyBytes ограничивает каждый буферизуемый ответ проверки готовности одним МиБ с дополнительным байтом для обнаружения переполнения.
	maximumMCPBodyBytes = 1 << 20
	// mcpAuthoritySocket fixes the local provider UDS endpoint protected by group permissions, peer credentials and a separate random local capability.
	// mcpAuthoritySocket задаёт фиксированный локальный UDS-endpoint провайдера, защищённый групповыми правами, идентичностью peer и отдельным случайным локальным билетом.
	mcpAuthoritySocket = "/run/kodex/provider/mcp-authority.sock"
	// mcpAuthorityHostName provides the synthetic HTTP host used over the local UDS transport, not a DNS-resolved upstream destination.
	// mcpAuthorityHostName задаёт синтетический HTTP-host локального UDS-транспорта, а не разрешаемое через DNS направление upstream.
	mcpAuthorityHostName = "kodex-mcp-authority"
)

// MCPProxy owns the local peer-checked UDS server, local capability, callback/file clients and upstream transport through the startup/close lifecycle.
// Upstream uses the selected callback profile; default mTLS and explicitly trusted-cluster transport remain distinct.
// MCPProxy владеет локальным UDS-сервером с проверкой peer, локальным билетом, клиентами callback и файлов, транспортом upstream в жизненном цикле подготовки и закрытия.
// Upstream использует выбранный callback-профиль; стандартный mTLS и явно доверенный кластерный транспорт остаются различными.
type MCPProxy struct {
	server     *http.Server
	transport  *http.Transport
	local      *http.Transport
	done       chan error
	socketPath string
	localToken string
	files      *callback.Client
}

// StartMCPProxy validates managed readiness proof and tool bounds, prepares the selected upstream transport, then creates a group-protected peer-checked UDS and fresh local bearer.
// Forwards only the local MCP route or pinned file reads, replacing upstream identity/binding headers, and verifies the required initialize/catalog path before returning.
// Uses the caller lifecycle context and cleans partial startup resources on errors; successful ownership transfers to MCPProxy.Close.
// StartMCPProxy проверяет управляемое доказательство готовности и предел инструментов, готовит выбранный upstream-транспорт, затем создаёт защищённый группой UDS с проверкой peer и новый локальный bearer.
// Передаёт только локальный маршрут MCP либо закреплённое чтение файлов, заменяя заголовки идентичности и привязок upstream, проверяет обязательный путь initialize и каталога до возврата.
// Использует контекст жизненного цикла вызывающего, очищает частично созданные ресурсы при ошибке; после успеха владение передаётся MCPProxy.Close.
func StartMCPProxy(ctx context.Context, input model.Input, token string, requiredTools []string) (_ *MCPProxy, resultErr error) {
	stage := mcpStageConfiguration
	defer func() { resultErr = withMCPStage(resultErr, stage) }()
	if err := runtimecontract.ValidateManagedMCPReadiness(input, time.Now()); err != nil {
		return nil, err
	}
	if len(requiredTools) == 0 || len(requiredTools) > 256 {
		return nil, errors.New("required MCP tool catalog is invalid")
	}
	upstream, err := url.Parse(input.CallbackURL)
	if err != nil || upstream.Host == "" ||
		(input.CallbackTLS.Profile == "" && upstream.Scheme != "https") ||
		(input.CallbackTLS.Profile != "" && input.ValidateCallbackTransport() != nil) {
		return nil, errors.New("required MCP endpoint is invalid")
	}
	upstream.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/mcp"
	stage = mcpStageTLSIdentity
	var transport *http.Transport
	if input.CallbackTLS.Profile == "" {
		transport, err = callback.MCPTransport(input.CallbackTLS)
	} else {
		transport, err = callback.TransportForInput(input)
		if err == nil {
			transport.ResponseHeaderTimeout = time.Duration(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds+5) * time.Second
		}
	}
	if err != nil {
		return nil, err
	}
	stage = mcpStageSocket
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
	stage = mcpStageCallbackAuthority
	fileClient, err := callback.New(input)
	if err != nil || fileClient.Token() != token {
		if fileClient != nil {
			fileClient.Close()
		}
		_ = listener.Close()
		_ = os.Remove(mcpAuthoritySocket)
		transport.CloseIdleConnections()
		return nil, errors.New("runtime file callback authority is invalid")
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
			request.Header.Set("X-Kodex-Organization-Ref", input.OrganizationRef)
			request.Header.Set("X-Kodex-Project-Ref", input.ProjectRef)
			request.Header.Set("X-Kodex-Run-Ref", input.RunRef)
			request.Header.Set("X-Kodex-Node-Ref", input.NodeRef)
			request.Header.Set("X-Kodex-Session-Ref", input.SessionRef)
			request.Header.Set("X-Kodex-Turn-Ref", input.TurnRef)
			request.Header.Set("X-Kodex-Attempt", strconv.FormatUint(uint64(input.Attempt), 10))
			request.Header.Set("X-Kodex-MCP-Binding-Version", strconv.FormatInt(input.RuntimeRevisionVersion, 10))
			request.Header.Set("X-Kodex-Runtime-Revision-Digest", input.RuntimeRevisionDigest)
			request.Header.Set("X-Kodex-Input-Digest", input.InputDigest)
			request.Header.Set("X-Kodex-Execution-Binding-Digest", input.ExecutionBindingDigest)
			request.Header.Set("X-Kodex-MCP-Binding-Digest", input.MCPBindingDigest)
			request.Header.Set("X-Kodex-Callback-Method", "mcp")
		},
		Transport: transport,
		ErrorLog:  log.New(io.Discard, "", 0),
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "required MCP upstream is unavailable", http.StatusBadGateway)
		},
		FlushInterval: -1,
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path != "/mcp" && subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) == 1 {
			fileClient.ServeCatalogFile(writer, request, input)
			return
		}
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
		socketPath: mcpAuthoritySocket, localToken: localToken, files: fileClient}
	go func() { done <- server.Serve(secured) }()
	localEndpoint, _ := url.Parse("http://" + mcpAuthorityHostName + "/mcp")
	if err := checkMCP(ctx, &http.Client{Transport: localTransport, Timeout: 15 * time.Second}, localEndpoint, localToken, requiredTools, input); err != nil {
		fileClient.Close()
		_ = server.Close()
		localTransport.CloseIdleConnections()
		transport.CloseIdleConnections()
		_ = os.Remove(mcpAuthoritySocket)
		return nil, err
	}
	return proxy, nil
}

// SocketPath returns the owned local UDS pathname for the provider adapter, without opening a socket or authorizing a caller.
// SocketPath возвращает собственный локальный путь UDS для адаптера провайдера без открытия сокета и авторизации вызывающего.
func (proxy *MCPProxy) SocketPath() string { return proxy.socketPath }

// LocalBearerToken returns the secret local capability for the authorized provider adapter; it is not the owner execution ticket and must not enter logs/history.
// LocalBearerToken возвращает секретный локальный билет авторизованному адаптеру провайдера; это не билет выполнения владельца, он не должен попадать в логи и историю.
func (proxy *MCPProxy) LocalBearerToken() string { return proxy.localToken }

// Close closes file-client resources, shuts down or forces the local server, closes idle transports and removes its socket, then joins the Serve result within ctx.
// Returns fixed shutdown/serve failures rather than transport details; callers own the bounded shutdown context.
// Close закрывает ресурсы файлового клиента, штатно либо принудительно завершает локальный сервер, закрывает простаивающие транспорты и удаляет сокет, затем ожидает результат Serve в пределах ctx.
// Возвращает фиксированные ошибки завершения или Serve вместо подробностей транспорта; ограниченный контекст завершения задаёт вызывающий.
func (proxy *MCPProxy) Close(ctx context.Context) error {
	proxy.files.Close()
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

// peerCredentialListener wraps a UDS listener so only accepted runtime peer UIDs reach the HTTP handler; local bearer validation remains separate.
// peerCredentialListener оборачивает UDS-слушатель, пропуская в HTTP-обработчик только допустимые UID выполнения; проверка локального bearer остаётся отдельной.
type peerCredentialListener struct {
	net.Listener
}

// Accept closes non-Unix or disallowed-peer connections and continues accepting until an eligible connection or listener error.
// Accept закрывает не-Unix соединения и соединения недопустимых peer, продолжая приём до подходящего соединения либо ошибки слушателя.
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

// allowedMCPPeer reads SO_PEERCRED from the accepted Unix connection and accepts only UID 10001 or 10002; errors reject the peer.
// This checks operating-system identity, not application grants or the local bearer.
// allowedMCPPeer читает SO_PEERCRED принятого Unix-соединения и принимает только UID 10001 либо 10002; ошибка отклоняет peer.
// Проверяется идентичность операционной системы, а не прикладные доступы или локальный bearer.
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

// checkMCP runs initialize, empty accepted initialized notification and tools/list with closed JSON-RPC envelopes and bounded catalog descriptors.
// Optionally verifies current managed proofs, exact tool names and canonical managed input-schema digests against one immutable runner input, then rechecks freshness.
// Non-null initialization result is required but not a full protocol-capability negotiation; catalog availability grants no execution authority.
// checkMCP выполняет initialize, пустое принятое уведомление initialized и tools/list с закрытыми JSON-RPC-оболочками и ограниченными описаниями каталога.
// Необязательно сверяет текущие управляемые доказательства, точные имена и канонические дайджесты управляемых входных схем с одним неизменяемым входом runner, затем повторяет проверку свежести.
// Требуется непустой результат инициализации, но не полное согласование возможностей протокола; доступность каталога не выдаёт полномочий выполнения.
func checkMCP(ctx context.Context, client *http.Client, endpoint *url.URL, token string, requiredTools []string, inputs ...runtimecontract.RunnerInput) (resultErr error) {
	stage := mcpStageInitialize
	defer func() { resultErr = withMCPStage(resultErr, stage) }()
	if len(inputs) > 1 {
		return errors.New("required MCP profile input is invalid")
	}
	expectedSchemas := map[string]string{}
	if len(inputs) == 1 {
		if err := runtimecontract.ValidateManagedMCPReadiness(inputs[0], time.Now()); err != nil {
			return err
		}
		expectedNames := runtimecontract.RuntimeMCPToolNames(inputs[0])
		configuredNames := append([]string(nil), requiredTools...)
		slices.Sort(expectedNames)
		slices.Sort(configuredNames)
		if !slices.Equal(expectedNames, configuredNames) {
			return errors.New("required MCP tool profile does not match RuntimeRevision")
		}
		var err error
		expectedSchemas, err = runtimecontract.ManagedMCPToolSchemas(inputs[0])
		if err != nil {
			return err
		}
	}
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

	stage = mcpStageInitialized
	initialized := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	raw, _, statusCode, err = postMCP(ctx, client, endpoint, token, initialized)
	if err != nil || statusCode != http.StatusAccepted || len(raw) != 0 {
		return errors.New("required MCP initialized notification failed")
	}

	stage = mcpStageCatalogTransport
	list := []byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","method":"tools/list","params":{}}`)
	raw, mediaType, statusCode, err = postMCP(ctx, client, endpoint, token, list)
	if err != nil || len(raw) == 0 || statusCode != http.StatusOK || mediaType != "application/json" {
		return errors.New("required MCP tool catalog is unavailable")
	}
	stage = mcpStageCatalogSchema
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
	want := append([]string(nil), requiredTools...)
	actual := make([]string, 0, len(catalog.Result.Tools))
	seen := make(map[string]struct{}, len(catalog.Result.Tools))
	for _, rawTool := range catalog.Result.Tools {
		var tool struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"inputSchema"`
			OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
		}
		toolDecoder := json.NewDecoder(bytes.NewReader(rawTool))
		toolDecoder.DisallowUnknownFields()
		if toolDecoder.Decode(&tool) != nil || toolDecoder.Decode(&struct{}{}) != io.EOF ||
			strings.TrimSpace(tool.Name) != tool.Name || tool.Name == "" || len(tool.Name) > 128 ||
			strings.TrimSpace(tool.Description) == "" || len(tool.Description) > 2000 ||
			len(tool.InputSchema) == 0 || tool.InputSchema[0] != '{' || !validMCPOutputSchema(tool.OutputSchema) {
			return errors.New("required MCP tool catalog is invalid")
		}
		if _, exists := seen[tool.Name]; exists {
			return errors.New("required MCP tool catalog is invalid")
		}
		seen[tool.Name] = struct{}{}
		actual = append(actual, tool.Name)
		if expected, required := expectedSchemas[tool.Name]; required {
			wantDigest, wantErr := runtimecontract.ManagedMCPSchemaDigest([]byte(expected))
			actualDigest, actualErr := runtimecontract.ManagedMCPSchemaDigest(tool.InputSchema)
			if wantErr != nil || actualErr != nil || wantDigest != actualDigest {
				return errors.New("required MCP tool schema does not match RuntimeRevision")
			}
		}
	}
	stage = mcpStageCatalogBinding
	slices.Sort(want)
	slices.Sort(actual)
	if !slices.Equal(actual, want) {
		return errors.New("required MCP tool catalog does not match RuntimeRevision")
	}
	if len(inputs) == 1 {
		return runtimecontract.ValidateManagedMCPReadiness(inputs[0], time.Now())
	}
	return nil
}

// postMCP posts JSON with the supplied bearer and caller context, buffers one bounded response and returns parsed media type/status for the stage validator.
// Transport, redirect and timeout policy come from the supplied HTTP client; upstream error bodies are not copied into returned errors.
// postMCP отправляет JSON с переданным bearer и контекстом вызывающего, ограниченно буферизует ответ, возвращает разобранные media type и статус для проверки стадии.
// Политика транспорта, перенаправлений и таймаутов наследуется от HTTP-клиента; тела ошибок upstream не копируются в возвращаемую ошибку.
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

// validMCPOutputSchema accepts an omitted outputSchema or a bounded non-null JSON object declaring type object, while leaving schema keywords open.
// Does not resolve schema references or perform full JSON Schema validation; the surrounding tool descriptor stays closed.
// validMCPOutputSchema принимает отсутствующий outputSchema либо ограниченный непустой JSON-объект с type object, оставляя ключевые слова схемы открытыми.
// Не разрешает ссылки схемы и не выполняет полную проверку JSON Schema; внешнее описание инструмента остаётся закрытым.
func validMCPOutputSchema(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var schema map[string]json.RawMessage
	if len(raw) > maximumMCPBodyBytes || json.Unmarshal(raw, &schema) != nil || schema == nil {
		return false
	}
	var kind string
	return json.Unmarshal(schema["type"], &kind) == nil && kind == "object"
}
