package managedworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// errReadRange identifies a rejected read range before any filesystem access. It contains
// no caller data and permits correcting arguments without implying an uncertain command effect.
// errReadRange обозначает отклонённый диапазон до доступа к файлам. Данных caller нет;
// аргументы можно исправить без предположения о неопределённом результате команды.
var errReadRange = errors.New("Workspace read range is invalid: offset_bytes must be 0..1048576 and maximum_bytes must be 4..32768, or omitted for the default 32768. No file was read or changed.")

// OwnerCall uses the same authenticated controller MCP boundary as the primary provider tools.
// It must not resolve endpoints, accept credentials or automatically repeat a lost write response.
// OwnerCall использует ту же authenticated controller MCP boundary, что основные provider tools.
// Endpoints и credentials не принимает; потерянный write response автоматически не повторяет.
type OwnerCall func(context.Context, string, string, map[string]any) (map[string]any, error)

// ExecutorCall is the trusted role's fixed UDS client, not a model-selectable HTTP transport.
// ExecutorCall — fixed UDS client доверенной роли, не выбираемый моделью HTTP transport.
type ExecutorCall func(context.Context, ExecutorRequest) (CommandResult, error)

// RecordCall records only the existing redacted native-tool lifecycle, not raw file/command data.
// RecordCall сохраняет лишь существующий redacted native-tool lifecycle, не file/command data.
type RecordCall func(context.Context, runtimecontract.NativeToolCall) error

// Handler serves the separate explicit local MCP catalog and serializes all admitted filesystem
// and command operations. A fresh owner policy read precedes and follows every operation.
// Handler обслуживает отдельный явный local MCP catalog и сериализует допущенные filesystem
// и command operations. Свежая owner policy проверяется до и после каждой операции.
type Handler struct {
	store     *fileStore
	owner     OwnerCall
	execute   ExecutorCall
	record    RecordCall
	gate      chan struct{}
	closed    atomic.Bool
	quiescent atomic.Bool
	calls     sync.Mutex
	pending   map[string]context.CancelFunc
}

// NewHandler requires the trusted role UID and complete opted-in immutable turn. No directory
// is created, credential read or command executed by this constructor.
// NewHandler требует trusted role UID и полный opted-in immutable turn. Каталогов не создаёт,
// credentials не читает и команд не запускает.
func NewHandler(input runtimecontract.RunnerInput, owner OwnerCall, execute ExecutorCall, record RecordCall) (*Handler, error) {
	if os.Geteuid() != 10001 || !runtimecontract.RuntimeWorkspaceToolsAvailable(input) || input.WorkspaceRoot != runtimecontract.RuntimeWorkspaceRoot || owner == nil || record == nil || (len(input.EnvironmentTools) > 0 && execute == nil) {
		return nil, errFiles
	}
	return &Handler{store: &fileStore{input: input, root: input.WorkspaceRoot, runnerUID: 10001, writerUID: 10002, group: 29000}, owner: owner, execute: execute, record: record, gate: make(chan struct{}, 1)}, nil
}

// authorize verifies the current own policy through the owner, including exact input binding
// and tool catalog. Immutable input alone never authorizes a current local effect.
// authorize проверяет текущую own policy через owner, включая exact input binding и tool catalog.
// Одного immutable input недостаточно для допуска текущего локального effect.
func (handler *Handler) authorize(ctx context.Context, id string) error {
	result, err := handler.owner(ctx, id, runtimecontract.WorkspacePolicyTool, map[string]any{})
	if err != nil || result["profile"] != "managed-workspace-v1" || result["input_digest"] != handler.store.input.InputDigest {
		return errFiles
	}
	raw, err := json.Marshal(result["tools"])
	var tools []string
	if err != nil || json.Unmarshal(raw, &tools) != nil || !slices.Equal(tools, runtimecontract.RuntimeWorkspaceToolNames(handler.store.input)) {
		return errFiles
	}
	return nil
}

// Close closes admission, drains the serialized operation under the caller budget and obtains
// an executor quiescence receipt before file capture. Unknown cleanup remains a closed failure.
// Close закрывает admission, дожидается сериализованной операции в caller budget и получает
// executor quiescence receipt до file capture. Неизвестный cleanup остаётся закрытым отказом.
func (handler *Handler) Close(ctx context.Context) error {
	if handler.quiescent.Load() {
		return nil
	}
	handler.closed.Store(true)
	handler.calls.Lock()
	for _, cancel := range handler.pending {
		cancel()
	}
	handler.calls.Unlock()
	select {
	case handler.gate <- struct{}{}:
		defer func() { <-handler.gate }()
	case <-ctx.Done():
		return errExecutor
	}
	if len(handler.store.input.EnvironmentTools) == 0 {
		handler.quiescent.Store(true)
		return nil
	}
	result, err := handler.execute(ctx, ExecutorRequest{Action: "QUIESCE", Binding: handler.store.input.InputDigest})
	if err != nil || result.Status != "QUIESCENT" {
		return errExecutor
	}
	handler.quiescent.Store(true)
	return nil
}

// ServeHTTP implements bounded stateless MCP JSON with a closed local catalog. The outer proxy
// supplies peer/bearer checks; this handler accepts no foreign path, endpoint or credential field.
// ServeHTTP реализует bounded stateless MCP JSON с закрытым local catalog. Внешний proxy
// проверяет peer/bearer; foreign path, endpoint и credential field не принимаются.
func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/workspace/mcp" || r.URL.RawPath != "" || r.URL.ForceQuery || r.URL.RawQuery != "" || handler.closed.Load() {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err != nil || len(raw) > 2<<20 || internalrpcauth.DecodeStrictJSON(raw, &request) != nil || request.JSONRPC != "2.0" {
		http.Error(w, "invalid workspace MCP request", http.StatusBadRequest)
		return
	}
	if request.Method == "notifications/cancelled" && len(request.ID) == 0 {
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
			Reason    string          `json:"reason,omitempty"`
		}
		if internalrpcauth.DecodeStrictJSON(request.Params, &params) != nil || len(params.RequestID) == 0 || len(params.RequestID) > 128 || len(params.Reason) > 1024 {
			http.Error(w, "invalid workspace cancellation", http.StatusBadRequest)
			return
		}
		key, ok := workspaceRequestKey(params.RequestID)
		if !ok {
			http.Error(w, "invalid workspace cancellation", http.StatusBadRequest)
			return
		}
		handler.calls.Lock()
		cancel := handler.pending[key]
		handler.calls.Unlock()
		if cancel != nil {
			cancel()
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if request.Method == "notifications/initialized" && len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var id any
	if len(request.ID) == 0 || len(request.ID) > 128 || internalrpcauth.DecodeStrictJSON(request.ID, &id) != nil {
		http.Error(w, "invalid workspace MCP id", http.StatusBadRequest)
		return
	}
	switch id.(type) {
	case string, json.Number:
	default:
		http.Error(w, "invalid workspace MCP id", http.StatusBadRequest)
		return
	}
	var result any
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
			ClientInfo      map[string]any `json:"clientInfo"`
		}
		decodeErr := internalrpcauth.DecodeStrictJSON(request.Params, &params)
		_, versionErr := time.Parse("2006-01-02", params.ProtocolVersion)
		if decodeErr != nil || versionErr != nil {
			http.Error(w, "invalid workspace MCP initialize", http.StatusBadRequest)
			return
		}
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "kodex-managed-workspace", "version": "1"}}
	case "tools/list":
		var params struct{}
		if len(request.Params) > 0 && internalrpcauth.DecodeStrictJSON(request.Params, &params) != nil {
			http.Error(w, "invalid workspace MCP list", http.StatusBadRequest)
			return
		}
		result = map[string]any{"tools": Tools(handler.store.input)}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      map[string]any  `json:"_meta,omitempty"`
		}
		if internalrpcauth.DecodeStrictJSON(request.Params, &params) != nil || !slices.Contains(runtimecontract.RuntimeWorkspaceToolNames(handler.store.input), params.Name) {
			http.Error(w, "unknown workspace MCP tool", http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(append(append([]byte(handler.store.input.InputDigest+":"+params.Name+":"), request.ID...), ':'))
		callID := "workspace-" + hex.EncodeToString(sum[:])
		key, ok := workspaceRequestKey(request.ID)
		if !ok {
			http.Error(w, "invalid workspace MCP id", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds)*time.Second)
		handler.calls.Lock()
		if handler.pending == nil {
			handler.pending = map[string]context.CancelFunc{}
		}
		_, duplicate := handler.pending[key]
		if duplicate || len(handler.pending) >= 16 {
			handler.calls.Unlock()
			cancel()
			http.Error(w, "workspace request already active or busy", http.StatusConflict)
			return
		}
		handler.pending[key] = cancel
		handler.calls.Unlock()
		defer func() { cancel(); handler.calls.Lock(); delete(handler.pending, key); handler.calls.Unlock() }()
		value, callErr := handler.call(ctx, callID, params.Name, params.Arguments)
		if callErr != nil {
			message := "Managed workspace operation failed or was denied; do not retry an unknown command outcome. Inspect current files and owner state."
			if errors.Is(callErr, errReadRange) {
				message = errReadRange.Error()
			}
			result = map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": message}}}
		} else {
			encoded, e := json.Marshal(value)
			if e != nil {
				http.Error(w, "workspace result unavailable", http.StatusServiceUnavailable)
				return
			}
			result = map[string]any{"structuredContent": value, "content": []map[string]string{{"type": "text", "text": string(encoded)}}, "isError": false}
		}
	default:
		http.Error(w, "unsupported workspace MCP method", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

// call takes the one local writer gate and fresh owner admission before dispatch. The final
// read prevents a known revoked lease from receiving file bytes or a misleading success result.
// call получает единый local writer gate и свежий owner admission до dispatch. Финальное чтение
// не выдаёт file bytes либо ложный успех при известном отзыве lease.
func (handler *Handler) call(ctx context.Context, id, name string, arguments json.RawMessage) (value any, resultErr error) {
	select {
	case handler.gate <- struct{}{}:
		defer func() { <-handler.gate }()
	case <-ctx.Done():
		return nil, errFiles
	}
	argumentCount, valid := closedToolArguments(handler.store.input, name, arguments)
	if !valid {
		return nil, errFiles
	}
	if handler.closed.Load() || handler.authorize(ctx, id+"-before") != nil {
		return nil, errFiles
	}
	call := runtimecontract.NativeToolCall{CallID: id, Revision: 1, Kind: runtimecontract.NativeToolKindDynamicTool, State: runtimecontract.NativeToolStateRunning,
		SafeParameters: map[string]any{"namespace": "workspace", "tool": name, "argument_count": argumentCount, "argument_shape": "OBJECT"}}
	if handler.record(ctx, call) != nil {
		return nil, errFiles
	}
	started := time.Now()
	defer func() {
		call.Revision = 2
		call.State = runtimecontract.NativeToolStateFailed
		call.SafeResult = runtimecontract.NativeToolResultFailed
		commandSucceeded := true
		if command, ok := value.(CommandResult); ok && command.Status != "SUCCEEDED" {
			commandSucceeded = false
		}
		if resultErr == nil && commandSucceeded {
			call.State = runtimecontract.NativeToolStateSucceeded
			call.SafeResult = runtimecontract.NativeToolResultCompleted
		}
		call.DurationMS = time.Since(started).Milliseconds()
		terminal, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := handler.record(terminal, call); err != nil {
			value = nil
			resultErr = errFiles
		}
	}()
	var result any
	var err error
	switch name {
	case runtimecontract.WorkspaceToolList:
		var params struct {
			Path  string `json:"path"`
			After string `json:"after,omitempty"`
		}
		if internalrpcauth.DecodeStrictJSON(arguments, &params) != nil || len(params.After) > 255 || strings.ContainsAny(params.After, "/\\\x00\n\r") {
			return nil, errFiles
		}
		var entries []File
		var more bool
		entries, more, err = handler.store.list(ctx, params.Path, params.After)
		after := ""
		if more && len(entries) > 0 {
			after = filepath.Base(entries[len(entries)-1].Path)
		}
		result = map[string]any{"entries": entries, "has_more": more, "next_after": after}
	case runtimecontract.WorkspaceToolRead:
		var params struct {
			Path    string `json:"path"`
			Offset  int64  `json:"offset_bytes,omitempty"`
			Maximum *int64 `json:"maximum_bytes,omitempty"`
		}
		if internalrpcauth.DecodeStrictJSON(arguments, &params) != nil {
			return nil, errFiles
		}
		maximum := int64(32768)
		if params.Maximum != nil {
			maximum = *params.Maximum
		}
		if params.Offset < 0 || params.Offset > maximumFileBytes || maximum < 4 || maximum > 32768 {
			return nil, errReadRange
		}
		result, err = handler.store.read(ctx, params.Path, params.Offset, maximum)
	case runtimecontract.WorkspaceToolWrite:
		var params struct {
			Path     string `json:"path"`
			Text     string `json:"text"`
			Expected string `json:"expected_sha256"`
		}
		if internalrpcauth.DecodeStrictJSON(arguments, &params) != nil {
			return nil, errFiles
		}
		var digest string
		digest, err = handler.store.write(ctx, params.Path, params.Text, params.Expected)
		result = map[string]any{"sha256": digest}
	case runtimecontract.WorkspaceToolExecute:
		var params struct {
			Command   string   `json:"command"`
			Arguments []string `json:"arguments"`
			Directory string   `json:"directory"`
			Timeout   int      `json:"timeout_seconds"`
		}
		if internalrpcauth.DecodeStrictJSON(arguments, &params) != nil || !runtimecontract.RuntimeWorkspaceCommandAllowed(handler.store.input, params.Command) {
			return nil, errExecutor
		}
		request := ExecutorRequest{Action: "RUN", Binding: handler.store.input.InputDigest, Command: params.Command, Arguments: params.Arguments, Directory: params.Directory, TimeoutSeconds: params.Timeout}
		if !validCommandArguments(request) {
			return nil, errExecutor
		}
		commandResult, commandErr := handler.execute(ctx, request)
		if commandErr != nil || commandResult.Status == "UNKNOWN" {
			handler.closed.Store(true)
			return nil, errExecutor
		}
		if !slices.Contains([]string{"SUCCEEDED", "FAILED", "TIMED_OUT", "CANCELLED"}, commandResult.Status) {
			handler.closed.Store(true)
			return nil, errExecutor
		}
		result = commandResult
	case runtimecontract.WorkspaceToolCheckout:
		result, err = handler.checkout(ctx, id, arguments)
	default:
		return nil, errFiles
	}
	if err != nil {
		return nil, err
	}
	if handler.authorize(ctx, id+"-after") != nil {
		return nil, errFiles
	}
	return result, nil
}

// errCheckout marks a bounded repository snapshot failure, never a successful partial checkout.
// errCheckout обозначает отказ bounded repository snapshot, не успешный partial checkout.
var errCheckout = errors.New("managed workspace repository snapshot is unavailable")

// Ready verifies current owner policy and initializes only the selected secret-free command list.
// Readiness runs before model startup and does not perform a repository read or execute a command.
// Ready проверяет текущую owner policy и закрепляет лишь выбранные команды без секретов.
// Readiness выполняется до старта модели, не читая репозиторий и не запуская команд.
func (handler *Handler) Ready(ctx context.Context) error {
	if handler.authorize(ctx, "workspace-ready-"+handler.store.input.InputDigest) != nil {
		return errFiles
	}
	lock, work, _, err := handler.store.lock(ctx, "work")
	if err != nil {
		return err
	}
	work.Close()
	lock.Close()
	if len(handler.store.input.EnvironmentTools) == 0 {
		return nil
	}
	commands := make([]string, 0, len(handler.store.input.EnvironmentTools))
	for _, tool := range handler.store.input.EnvironmentTools {
		commands = append(commands, tool.Command)
	}
	result, err := handler.execute(ctx, ExecutorRequest{Action: "INITIALIZE", Binding: handler.store.input.InputDigest, Commands: commands})
	if err != nil || result.Status != "READY" {
		return errExecutor
	}
	return nil
}

// closedToolArguments enforces the producer catalog's exact case-sensitive field names,
// required selectors and non-null object values before typed decoding or owner admission.
// It composes the existing duplicate/depth decoder instead of introducing another JSON parser.
// closedToolArguments проверяет exact case-sensitive field names, required selectors и non-null
// object values каталога producer до typed decode либо owner admission. Использует существующий
// duplicate/depth decoder без нового JSON parser.
func closedToolArguments(input runtimecontract.RunnerInput, name string, raw json.RawMessage) (int, bool) {
	var fields map[string]json.RawMessage
	if internalrpcauth.DecodeStrictJSON(raw, &fields) != nil || fields == nil {
		return 0, false
	}
	for _, tool := range Tools(input) {
		if tool["name"] != name {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		for key, value := range fields {
			if _, exists := properties[key]; !exists || string(value) == "null" {
				return 0, false
			}
		}
		for _, key := range schema["required"].([]string) {
			if _, exists := fields[key]; !exists {
				return 0, false
			}
		}
		return len(fields), true
	}
	return 0, false
}

// workspaceRequestKey canonicalizes only bounded scalar JSON-RPC IDs for own-request cancellation.
// It is not an execution selector; peer/bearer and immutable Handler input already fix that scope.
// workspaceRequestKey канонизирует лишь bounded scalar JSON-RPC IDs для own-request cancellation.
// Это не execution selector: peer/bearer и immutable Handler input уже фиксируют scope.
func workspaceRequestKey(raw json.RawMessage) (string, bool) {
	var id any
	if len(raw) > 128 || internalrpcauth.DecodeStrictJSON(raw, &id) != nil {
		return "", false
	}
	switch id.(type) {
	case string, json.Number:
	default:
		return "", false
	}
	encoded, err := json.Marshal(id)
	return string(encoded), err == nil
}
