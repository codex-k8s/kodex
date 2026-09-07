package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestAppServerPipeProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_APP_SERVER_PIPE_FIXTURE")
	if mode == "" {
		return
	}
	switch mode {
	case "start-failure":
		// Изолированный процесс исключает позднее закрытие FD предыдущих fixtures.
		_, _ = startAppServerCommand(exec.Command("/definitely/missing/kodex"), nil)
		before, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			os.Exit(7)
		}
		for attempt := 0; attempt < 50; attempt++ {
			if _, err := startAppServerCommand(exec.Command("/definitely/missing/kodex"), nil); err == nil {
				os.Exit(8)
			}
		}
		after, err := os.ReadDir("/proc/self/fd")
		if err != nil || len(before) != len(after) {
			os.Exit(9)
		}
	case "buffered":
		_, _ = os.Stdout.WriteString("{\"id\":1,\"result\":{}}\n")
		_, _ = os.Stderr.WriteString("bounded diagnostic")
	case "clean":
		_, _ = os.Stderr.WriteString("bounded diagnostic")
	case "overflow":
		_, _ = os.Stderr.Write(make([]byte, maximumDiagnosticSize+1))
	case "nonzero":
		os.Exit(3)
	case "term-resistant":
		signal.Ignore(syscall.SIGTERM)
		pidPath := os.Getenv("KODEX_APP_SERVER_PIPE_PID_PATH")
		if pidPath == "" || os.WriteFile(pidPath, []byte("ready"), 0o600) != nil {
			os.Exit(6)
		}
		time.Sleep(30 * time.Second)
	case "holding-descendant":
		child := exec.Command("/usr/bin/sleep", "30")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(5)
		}
		pidPath := os.Getenv("KODEX_APP_SERVER_PIPE_PID_PATH")
		if pidPath == "" || os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0o600) != nil {
			_ = child.Process.Kill()
			os.Exit(6)
		}
	default:
		os.Exit(4)
	}
	os.Exit(0)
}

func TestAppServerOwnsPipesUntilReadersDrain(t *testing.T) {
	command := appServerPipeFixtureCommand(t, "buffered")
	readerGate := make(chan struct{})
	server, err := startAppServerCommand(command, readerGate)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case server.waitErr = <-server.wait:
		server.waited = true
	case <-time.After(5 * time.Second):
		t.Fatal("fixture process did not exit")
	}
	if server.waitErr != nil {
		t.Fatal("fixture process failed")
	}
	close(readerGate)
	select {
	case event, open := <-server.messages:
		if !open || event.err != nil || event.message.kind != messageResponse {
			t.Fatal("buffered response was not preserved after process exit")
		}
	case <-time.After(time.Second):
		t.Fatal("buffered response reader did not finish")
	}
	select {
	case _, open := <-server.messages:
		if open {
			t.Fatal("unexpected second response")
		}
	case <-time.After(time.Second):
		t.Fatal("response stream did not close")
	}
	server.readDiagnostic(time.Second)
	if !server.diagnosticRead || server.diagnosticErr != nil {
		t.Fatal("bounded diagnostic stream did not drain cleanly")
	}
	server.closeStreams()
}

func TestAppServerStopPreservesProcessAndDiagnosticFailures(t *testing.T) {
	for _, test := range []struct {
		mode      string
		wantError string
	}{
		{mode: "clean"},
		{mode: "overflow", wantError: "Codex app-server diagnostic stream exceeded its bound"},
		{mode: "nonzero", wantError: "Codex app-server exited unsuccessfully"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			server, err := startAppServerCommand(appServerPipeFixtureCommand(t, test.mode), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = server.stop(newProtocolState(""))
			if test.wantError == "" && err != nil {
				t.Fatal("clean process shutdown failed")
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("shutdown error category changed: %v", err)
			}
		})
	}
}

func TestAppServerTerminateKillsDescriptorHoldingDescendant(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	command := appServerPipeFixtureCommand(t, "holding-descendant")
	command.Env = append(command.Env, "KODEX_APP_SERVER_PIPE_PID_PATH="+pidPath)
	server, err := startAppServerCommand(command, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var pid int
	for time.Now().Before(deadline) {
		raw, readErr := os.ReadFile(pidPath)
		if readErr == nil {
			pid, err = strconv.Atoi(string(raw))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || pid <= 0 {
		t.Fatal("descriptor holding descendant was not observed")
	}
	select {
	case server.waitErr = <-server.wait:
		server.waited = true
	case <-time.After(processGrace):
		t.Fatal("fixture leader did not exit")
	}
	if server.waitErr != nil {
		t.Fatal("fixture leader failed")
	}
	cause := errors.New("synthetic abort")
	started := time.Now()
	if err := server.terminate(cause); !errors.Is(err, cause) {
		t.Fatal("abort cause was replaced")
	}
	if time.Since(started) > 3*terminationGrace {
		t.Fatal("descriptor drain exceeded its bounded shutdown budget")
	}
	for attempt := 0; attempt < 100 && syscall.Kill(pid, 0) == nil; attempt++ {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("descriptor holding descendant survived process-group shutdown")
	}
}

func TestAppServerStartFailureClosesOwnedPipes(t *testing.T) {
	if err := appServerPipeFixtureCommand(t, "start-failure").Run(); err != nil {
		t.Fatal("isolated start failure descriptor fixture failed")
	}
}

func appServerPipeFixtureCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestAppServerPipeProcessFixture$")
	command.Env = []string{"KODEX_APP_SERVER_PIPE_FIXTURE=" + mode}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	return command
}

func TestAppServerAbortPreservesRealReadError(t *testing.T) {
	gate := make(chan struct{})
	server, err := startAppServerCommand(appServerPipeFixtureCommand(t, "clean"), gate)
	if err != nil {
		t.Fatal("fixture start failed")
	}
	server.waitErr = <-server.wait
	server.waited = true
	// Настоящий read error до первого Read, не имитация текста diagnostic.
	_ = server.stderr.Close()
	close(gate)
	cause := errors.New("synthetic abort")
	err = server.abort(t.Context(), newProtocolState(""), cause)
	if !errors.Is(err, cause) || !server.diagnosticRead || server.diagnosticErr == nil || !errors.Is(err, server.diagnosticErr) || !server.readerRead {
		t.Fatal("abort lost read error or reader join")
	}
}

func TestAppServerTermResistantProcessIsKilledAndJoined(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready")
	command := appServerPipeFixtureCommand(t, "term-resistant")
	command.Env = append(command.Env, "KODEX_APP_SERVER_PIPE_PID_PATH="+readyPath)
	server, err := startAppServerCommand(command, nil)
	if err != nil {
		t.Fatal("fixture start failed")
	}
	t.Cleanup(func() { _ = server.terminate(errors.New("fixture cleanup")) })
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("fixture readiness deadline")
	}
	cause := errors.New("synthetic abort")
	start := time.Now()
	err = server.terminate(cause)
	if !errors.Is(err, cause) || !server.waited || server.waitErr == nil || !server.readerRead || !server.diagnosticRead {
		t.Fatal("TERM resistant process was not joined")
	}
	if time.Since(start) > 4*terminationGrace {
		t.Fatal("termination budget exceeded")
	}
	if syscall.Kill(command.Process.Pid, 0) == nil {
		t.Fatal("process survived KILL")
	}
}

func TestAppServerStopCloseFailureStillJoins(t *testing.T) {
	server, err := startAppServerCommand(appServerPipeFixtureCommand(t, "clean"), nil)
	if err != nil {
		t.Fatal("fixture start failed")
	}
	_ = server.stdin.Close()
	if server.stop(newProtocolState("")) == nil || !server.waited || !server.readerRead || !server.diagnosticRead {
		t.Fatal("close failure bypassed cleanup")
	}
}

func TestTurnStartPinsModelReasoningAndPersonalityOnEveryAttempt(t *testing.T) {
	for _, session := range []string{"", "existing-thread"} {
		input := model.Input{ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high", Model: "gpt-6-astra", CodexSessionID: session, WorkspaceRoot: "/workspace",
			CodexApprovalPolicy: "never", ConfigOverlay: "model_reasoning_effort = \"high\"\npersonality = \"pragmatic\"\n"}
		input.OrganizationRef, input.ProjectRef, input.AgentRef = "org_abcdefgh", "proj_abcdefgh", "agt_abcdefgh"
		snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef}
		snapshot.Digest, _ = snapshot.ComputeDigest()
		input.ContextSnapshot = &snapshot
		params, err := turnStartParams(input, "exact-thread", []byte("exact server prompt"))
		if err != nil || params["model"] != "gpt-6-astra" || params["effort"] != "high" ||
			params["personality"] != "pragmatic" || params["threadId"] != "exact-thread" {
			t.Fatalf("turn parameters = %#v, error = %v", params, err)
		}
		input.ConfigOverlay, input.EffectiveReasoningEffort = "", "medium"
		params, err = turnStartParams(input, "exact-thread", []byte("prompt"))
		if err != nil || params["effort"] != "medium" {
			t.Fatalf("server default not sent: %+v, %v", params, err)
		}
		input.EffectiveReasoningEffort = ""
		if _, err := turnStartParams(input, "exact-thread", []byte("prompt")); err == nil {
			t.Fatal("missing owner effort reached turn/start")
		}
		input.ReasoningMode = runtimecontract.ReasoningUnsupported
		params, err = turnStartParams(input, "exact-thread", []byte("prompt"))
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := params["effort"]; exists {
			t.Fatal("non-reasoning model received invented effort")
		}
	}
}

func TestExecuteLocalRejectsUnknownSelectionBeforeProcessOrCredentialAccess(t *testing.T) {
	for _, input := range []model.Input{
		{Provider: "openai", Model: "unknown"},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "unknown = true"},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "model_reasoning_effort = \"none\""},
		{Provider: "openai", Model: "gpt-6-astra", EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{Command: "missing-kodex-tool"}}},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "[mcp_servers.foreign]\nurl = \"https://example.invalid\""},
	} {
		if _, err := executeLocal(context.Background(), input, []byte("task"), ""); !errors.Is(err, ErrRuntimeProfile) {
			t.Fatalf("selection reached credential/process boundary: %v", err)
		}
	}
}

func TestProtocolErrorReportsOnlyMethodAndCode(t *testing.T) {
	t.Parallel()
	err := protocolError("turn/start", json.RawMessage(`{"code":-32602,"message":"secret diagnostic"}`))
	if err == nil || err.Error() != "Codex app-server returned a protocol error for turn/start (code -32602)" {
		t.Fatalf("protocol error = %v", err)
	}
	if strings.Contains(err.Error(), "secret diagnostic") {
		t.Fatal("protocol error exposed the upstream diagnostic")
	}
}

func TestClassifyAccountReadResponse(t *testing.T) {
	t.Parallel()

	availabilityErr := errors.New("Codex app-server is unavailable")
	protocolErr := protocolError("account/read", json.RawMessage(`{"code":-32603,"message":"internal"}`))
	tests := []struct {
		name     string
		raw      json.RawMessage
		callErr  error
		wantErr  bool
		wantAuth bool
	}{
		{name: "explicit authentication required", raw: json.RawMessage(`{"account":null,"requiresOpenaiAuth":true}`), wantErr: true, wantAuth: true},
		{name: "API key account", raw: json.RawMessage(`{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`)},
		{name: "ChatGPT account", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null,"planType":"pro"},"requiresOpenaiAuth":true}`)},
		{name: "external Bedrock account", raw: json.RawMessage(`{"account":{"type":"amazonBedrock","usesCodexManagedCredentials":false},"requiresOpenaiAuth":false}`)},
		{name: "provider without OpenAI account", raw: json.RawMessage(`{"requiresOpenaiAuth":false}`)},
		{name: "transport unavailable", callErr: availabilityErr, wantErr: true},
		{name: "protocol failure", callErr: protocolErr, wantErr: true},
		{name: "invalid top-level schema", raw: json.RawMessage(`{"account":null,"requiresOpenaiAuth":"true"}`), wantErr: true},
		{name: "invalid account schema", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null},"requiresOpenaiAuth":false}`), wantErr: true},
		{name: "unknown account field", raw: json.RawMessage(`{"account":{"type":"apiKey","token":"hidden"},"requiresOpenaiAuth":false}`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := classifyAccountReadResponse(test.raw, test.callErr)
			if (err != nil) != test.wantErr {
				t.Fatalf("classifyAccountReadResponse() error = %v, wantErr %v", err, test.wantErr)
			}
			if got := errors.Is(err, ErrProviderAuthentication); got != test.wantAuth {
				t.Fatalf("errors.Is(error, ErrProviderAuthentication) = %v, want %v; error = %v", got, test.wantAuth, err)
			}
		})
	}
}

func TestAppServerEnvironmentPreservesOnlyRequiredEgressProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://egress-gateway:8080")
	t.Setenv("HTTPS_PROXY", "http://egress-gateway:8080")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("UNRELATED_SECRET", "must-not-be-forwarded")

	environment := appServerEnvironment(model.Input{CodexHome: "/workspace/.kodex"}, "mcp-token")
	for _, expected := range []string{
		"HTTP_PROXY=http://egress-gateway:8080",
		"HTTPS_PROXY=http://egress-gateway:8080",
		"NO_PROXY=127.0.0.1,localhost",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("app-server environment does not contain %q: %#v", expected, environment)
		}
	}
	if slices.Contains(environment, "UNRELATED_SECRET=must-not-be-forwarded") {
		t.Fatalf("unrelated process environment was forwarded: %#v", environment)
	}
}

func TestTokenUsageNotificationRemainsEnabled(t *testing.T) {
	t.Parallel()
	for _, method := range suppressedNotificationMethods {
		if method == "thread/tokenUsage/updated" {
			t.Fatal("token usage notification is required for authoritative per-turn accounting")
		}
	}
}

func TestRequiredMCPToolNamesMatchRuntimeAuthority(t *testing.T) {
	input := model.Input{SystemAssistant: true}
	input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{})
	input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{})
	actual := RequiredMCPToolNames(input)
	expected := []string{
		"propose_run_metadata",
		"get_configuration_catalog",
		"propose_configuration_plan",
		"propose_assistant_metadata",
		"delegate_agent",
		"invoke_integration",
	}
	if !sameStringSet(actual, expected) {
		t.Fatalf("required MCP tools = %#v, want %#v", actual, expected)
	}
}

func TestTrustedMCPToolApproval(t *testing.T) {
	state := &protocolState{threadID: "thread-1", turnID: "turn-1"}
	request := map[string]any{
		"threadId":   "thread-1",
		"turnId":     "turn-1",
		"serverName": "kodex",
		"mode":       "form",
		"message":    "Allow this MCP tool call?",
		"requestedSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := trustedMCPToolApproval(state, raw)
	if err != nil {
		t.Fatalf("approve trusted MCP tool: %v", err)
	}
	if response["action"] != "accept" {
		t.Fatalf("action = %#v", response["action"])
	}
	content, ok := response["content"].(map[string]any)
	if !ok || len(content) != 0 {
		t.Fatalf("content = %#v", response["content"])
	}
}

func TestTrustedMCPToolApprovalRejectsAuthorityExpansion(t *testing.T) {
	state := &protocolState{threadID: "thread-1", turnID: "turn-1"}
	tests := map[string]map[string]any{
		"foreign server": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "external", "mode": "form",
			"message": "approve", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"foreign turn": {
			"threadId": "thread-1", "turnId": "turn-2", "serverName": "kodex", "mode": "form",
			"message": "approve", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"user input form": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "kodex", "mode": "form",
			"message": "provide input", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"ordinary elicitation": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "kodex", "mode": "form",
			"message": "provide input", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{},
		},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := trustedMCPToolApproval(state, raw); err == nil {
				t.Fatal("authority expansion was accepted")
			}
		})
	}
}
