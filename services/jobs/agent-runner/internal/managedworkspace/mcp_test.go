package managedworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// handlerFixture supplies a synthetic current-owner read and validates every redacted native
// activity. It grants no real platform permission and does not contact any external service.
// handlerFixture подставляет synthetic current-owner read и проверяет каждую redacted native
// activity. Реальных platform permissions не выдаёт, внешних сервисов не вызывает.
func handlerFixture(t *testing.T) (*Handler, *[]runtimecontract.NativeToolCall) {
	t.Helper()
	store := fileStoreFixture(t)
	calls := []runtimecontract.NativeToolCall{}
	handler := &Handler{store: store, gate: make(chan struct{}, 1), owner: func(_ context.Context, _, name string, _ map[string]any) (map[string]any, error) {
		if name != runtimecontract.WorkspacePolicyTool {
			t.Fatal("unexpected owner tool", name)
		}
		return map[string]any{"profile": "managed-workspace-v1", "input_digest": store.input.InputDigest, "tools": runtimecontract.RuntimeWorkspaceToolNames(store.input)}, nil
	}, record: func(_ context.Context, call runtimecontract.NativeToolCall) error {
		if err := call.Validate(); err != nil {
			t.Fatal("invalid native activity", err)
		}
		calls = append(calls, call)
		return nil
	}}
	return handler, &calls
}

// TestLocalMCPUsesFreshOwnerBeforeEffectAndBeforeReturningBytes verifies denial before a file
// write, a later revoked read, and exact redacted RUNNING/terminal activity without raw contents.
// TestLocalMCPUsesFreshOwnerBeforeEffectAndBeforeReturningBytes проверяет отказ до file write,
// поздний revoked read и exact redacted RUNNING/terminal activity без raw contents.
func TestLocalMCPUsesFreshOwnerBeforeEffectAndBeforeReturningBytes(t *testing.T) {
	handler, calls := handlerFixture(t)
	ctx := context.Background()
	owner := handler.owner
	handler.owner = func(context.Context, string, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("denied")
	}
	args := json.RawMessage(`{"path":"work/file","text":"synthetic source","expected_sha256":"ABSENT"}`)
	if _, err := handler.call(ctx, "workspace-write-denied", runtimecontract.WorkspaceToolWrite, args); err == nil {
		t.Fatal("missing owner admission accepted")
	}
	if _, err := os.Stat(filepath.Join(handler.store.root, "work/file")); !os.IsNotExist(err) {
		t.Fatal("denied write changed files")
	}
	handler.owner = owner
	if _, err := handler.call(ctx, "workspace-write-allowed", runtimecontract.WorkspaceToolWrite, args); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[0].State != "RUNNING" || (*calls)[1].State != "SUCCEEDED" {
		t.Fatal("missing native lifecycle", *calls)
	}
	read := json.RawMessage(`{"path":"work/file"}`)
	count := 0
	handler.owner = func(ctx context.Context, id, name string, args map[string]any) (map[string]any, error) {
		count++
		if count == 2 {
			return nil, errors.New("revoked")
		}
		return owner(ctx, id, name, args)
	}
	value, err := handler.call(ctx, "workspace-read-revoked", runtimecontract.WorkspaceToolRead, read)
	if err == nil || value != nil {
		t.Fatal("revoked owner released bytes", value, err)
	}
	raw, _ := json.Marshal(calls)
	if strings.Contains(string(raw), "synthetic source") {
		t.Fatal("native audit contains file bytes")
	}
}

// TestWorkspaceMCPCatalogAndClosedArguments checks actual stateless JSON-RPC and rejects duplicate
// keys and attempted credential/endpoint fields before any file effect.
// TestWorkspaceMCPCatalogAndClosedArguments проверяет actual stateless JSON-RPC и отклоняет
// duplicate keys и credential/endpoint fields до какого-либо file effect.
func TestWorkspaceMCPCatalogAndClosedArguments(t *testing.T) {
	handler, _ := handlerFixture(t)
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write_workspace_file","arguments":{"path":"work/file","text":"ok","expected_sha256":"ABSENT"}}}`} {
		writer := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "http://workspace/workspace/mcp", strings.NewReader(body))
		handler.ServeHTTP(writer, request)
		if writer.Code != 200 || strings.Contains(writer.Body.String(), `"isError":true`) {
			t.Fatal(writer.Code, writer.Body.String())
		}
	}
	for _, args := range []string{`{"path":"work/file","Path":"work/private"}`, `{"path":null}`, `{"path":"work/file","path":"work/private"}`, `{"path":"work/file","authorization":"synthetic"}`, `{"path":"work/file","endpoint":"https://example.invalid"}`} {
		if _, err := handler.call(context.Background(), "workspace-invalid-args", runtimecontract.WorkspaceToolRead, json.RawMessage(args)); err == nil {
			t.Fatal("unknown or duplicate argument accepted")
		}
	}
	if err := handler.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.call(context.Background(), "workspace-after-close", runtimecontract.WorkspaceToolRead, json.RawMessage(`{"path":"work/file"}`)); err == nil {
		t.Fatal("closed handler admitted read")
	}
}

// TestWorkspaceReadRangeCanBeCorrected exercises the real MCP boundary after an oversized
// model request: the bounded diagnostic exposes no file bytes and a corrected read retains
// fresh owner admission and the complete file hash. Unknown effect errors stay conservative.
// TestWorkspaceReadRangeCanBeCorrected проверяет MCP после слишком большого запроса модели:
// ограниченная диагностика не раскрывает файл, исправленное чтение сохраняет свежий owner
// admission и полный hash. Ошибки с неопределённым эффектом остаются консервативными.
func TestWorkspaceReadRangeCanBeCorrected(t *testing.T) {
	handler, _ := handlerFixture(t)
	hash, err := handler.store.write(context.Background(), "work/proof.txt", "synthetic proof\n", "ABSENT")
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []string{
		`{"path":"work/proof.txt","maximum_bytes":1048576}`,
		`{"path":"work/proof.txt","maximum_bytes":3}`,
		`{"path":"work/proof.txt","maximum_bytes":0}`,
		`{"path":"work/proof.txt","offset_bytes":-1}`,
		`{"path":"work/proof.txt","offset_bytes":1048577}`,
	} {
		writer := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "http://workspace/workspace/mcp", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_workspace_file","arguments":`+arguments+`}}`))
		handler.ServeHTTP(writer, request)
		body := writer.Body.String()
		if writer.Code != 200 || !strings.Contains(body, `"isError":true`) ||
			!strings.Contains(body, "maximum_bytes must be 4..32768") || strings.Contains(body, "synthetic proof") {
			t.Fatal("invalid range diagnostic", writer.Code, body)
		}
	}
	value, err := handler.call(context.Background(), "corrected-read", runtimecontract.WorkspaceToolRead,
		json.RawMessage(`{"path":"work/proof.txt","maximum_bytes":32768}`))
	if err != nil {
		t.Fatal(err)
	}
	read, ok := value.(FileRead)
	if !ok || read.Text != "synthetic proof\n" || read.SHA256 != hash {
		t.Fatal("corrected read lost bytes or hash", value)
	}
}

// TestUnknownCommandClosesLocalFileAdmission proves a lost/unknown command reply cannot be
// followed by file access under an unproven writer-stop assumption; there is no automatic retry.
// TestUnknownCommandClosesLocalFileAdmission доказывает, что lost/unknown command reply закрывает
// file access при недоказанной остановке writer; автоматического повтора нет.
func TestUnknownCommandClosesLocalFileAdmission(t *testing.T) {
	handler, _ := handlerFixture(t)
	handler.store.input.EnvironmentTools = []runtimecontract.RuntimeEnvironmentTool{{Name: "Bash", Command: "bash"}}
	attempts := 0
	handler.execute = func(context.Context, ExecutorRequest) (CommandResult, error) {
		attempts++
		return CommandResult{Status: "UNKNOWN", ExitCode: -1}, errExecutor
	}
	args := json.RawMessage(`{"command":"bash","arguments":[],"directory":"work","timeout_seconds":1}`)
	if _, err := handler.call(context.Background(), "workspace-unknown-command", runtimecontract.WorkspaceToolExecute, args); err == nil {
		t.Fatal("unknown command accepted")
	}
	if _, err := handler.call(context.Background(), "workspace-file-after-unknown", runtimecontract.WorkspaceToolList, json.RawMessage(`{"path":"work"}`)); err == nil {
		t.Fatal("unproven writer state allowed files")
	}
	if attempts != 1 {
		t.Fatal("unknown command repeated", attempts)
	}
}

// TestMCPRequestCancellationStopsTheOwnCommand verifies the actual notification route cancels
// the active request, never repeats its command, and closes file admission after uncertain I/O.
// TestMCPRequestCancellationStopsTheOwnCommand проверяет, что actual notification route отменяет
// свой active request, не повторяет команду и закрывает file admission после uncertain I/O.
func TestMCPRequestCancellationStopsTheOwnCommand(t *testing.T) {
	handler, _ := handlerFixture(t)
	handler.store.input.EnvironmentTools = []runtimecontract.RuntimeEnvironmentTool{{Name: "Bash", Command: "bash"}}
	started := make(chan struct{})
	done := make(chan struct{})
	attempts := 0
	handler.execute = func(ctx context.Context, _ ExecutorRequest) (CommandResult, error) {
		attempts++
		close(started)
		<-ctx.Done()
		return CommandResult{}, ctx.Err()
	}
	go func() {
		defer close(done)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest("POST", "http://workspace/workspace/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"run_workspace_tool","arguments":{"command":"bash","arguments":[],"directory":"work","timeout_seconds":1}}}`)))
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command never started")
	}
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest("POST", "http://workspace/workspace/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":17}}`)))
	if writer.Code != 202 {
		t.Fatal("cancellation notification rejected", writer.Code)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled command did not join")
	}
	if attempts != 1 || !handler.closed.Load() {
		t.Fatal("cancelled uncertain command repeated or kept file admission")
	}
}

// TestWorkspaceMCPNegotiatesItsImplementedProtocol admits a newer client's valid initialization
// but advertises only the protocol actually implemented by this stateless local server.
// TestWorkspaceMCPNegotiatesItsImplementedProtocol допускает valid initialization нового клиента,
// но сообщает лишь фактически реализованный protocol stateless local server.
func TestWorkspaceMCPNegotiatesItsImplementedProtocol(t *testing.T) {
	handler, _ := handlerFixture(t)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest("POST", "http://workspace/workspace/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"synthetic","version":"1"}}}`)))
	if writer.Code != 200 || !strings.Contains(writer.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatal("protocol negotiation failed", writer.Code, writer.Body.String())
	}
}
