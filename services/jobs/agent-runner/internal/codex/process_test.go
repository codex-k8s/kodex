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
	case "rollout-nonzero", "rollout-clean":
		path := os.Getenv("KODEX_APP_SERVER_ROLLOUT_PATH")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			os.Exit(6)
		}
		_, writeErr := file.WriteString("{\"type\":\"event_msg\"}\n")
		syncErr, closeErr := file.Sync(), file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			os.Exit(7)
		}
		if mode == "rollout-nonzero" {
			os.Exit(3)
		}
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

func TestFailedProcessCanCaptureOnlyAfterBoundedJoin(t *testing.T) {
	for _, mode := range []string{"rollout-nonzero", "rollout-clean"} {
		t.Run(mode, func(t *testing.T) {
			input, before := capturedRolloutFixture(t, model.Input{})
			command := appServerPipeFixtureCommand(t, mode)
			command.Env = append(command.Env, "KODEX_APP_SERVER_ROLLOUT_PATH="+before.ArchivePath)
			gate := make(chan struct{})
			server, err := startAppServerCommand(command, gate)
			if err != nil {
				t.Fatal(err)
			}
			state := newProtocolState(before.SessionID)
			state.threadID, state.threadPath = before.SessionID, before.ArchivePath
			if server.captureReady() || captureFailedRollout(input, server, state, Result{}).SessionID != "" {
				t.Fatal("unjoined writer issued capture")
			}
			close(gate)
			if mode == "rollout-nonzero" {
				if err := server.stop(state); !errors.Is(err, errAppServerExited) {
					t.Fatal("process failure was not retained")
				}
			} else {
				// thread/read failure после append использует предыдущую проверенную identity.
				server.waitErr = <-server.wait
				server.waited = true
				cause := errors.New("synthetic thread read failure")
				if err := server.abort(t.Context(), state, cause); !errors.Is(err, cause) {
					t.Fatal("protocol cause was not retained")
				}
			}
			if !server.captureReady() {
				t.Fatal("bounded join did not close writer and readers")
			}
			captured, err := CaptureStoppedRollout(input, state.threadID, state.threadPath)
			if err != nil || !captured.HasVerifiedRollout(input) || captured.ArchiveSHA256 == before.ArchiveSHA256 || captured.ArchiveSizeBytes <= before.ArchiveSizeBytes {
				t.Fatal("stopped failed writer lost appended source bytes")
			}
			// Обычный UID не может назначить deployment group; production helper
			// не выдаёт pins при этом отказе вместо небезопасного fallback.
			failed := captureFailedRollout(input, server, state, Result{Usage: before.Usage})
			if failed.Usage != before.Usage {
				t.Fatal("capture failure lost measured usage")
			}
			if failed.SessionID != "" && !failed.HasVerifiedRollout(input) {
				t.Fatal("capture failure issued unverified pins")
			}
		})
	}
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
		if _, err := executeLocal(context.Background(), input, []byte("task"), "", nil); !errors.Is(err, ErrRuntimeProfile) {
			t.Fatalf("selection reached credential/process boundary: %v", err)
		}
	}
}

func TestProviderStageErrorKeepsClassificationAndHidesDiagnostic(t *testing.T) {
	secret := errors.New("provider response with secret diagnostic")
	err := atProviderStage(providerStageThreadCall, secret)
	if !errors.Is(err, secret) {
		t.Fatal("wrapped failure lost its original classification")
	}
	if got := providerStageOf(err); got != providerStageThreadCall {
		t.Fatalf("providerStageOf() = %q, want %q", got, providerStageThreadCall)
	}
	if strings.Contains(err.Error(), "secret") || err.Error() != "Codex provider execution stage failed" {
		t.Fatalf("provider stage error exposed diagnostic: %q", err.Error())
	}
	if got := providerStageOf(errors.New("unclassified")); got != providerStageUnknown {
		t.Fatalf("unclassified providerStageOf() = %q, want %q", got, providerStageUnknown)
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

func TestWaitTerminalPreservesClosedFailureDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method, detail, category string
		payload                        json.RawMessage
		kind                           messageKind
		streamError                    error
	}{
		{name: "unknown method", method: "private-sentinel", payload: json.RawMessage(`{"private":"private-sentinel"}`), kind: messageNotification, detail: "NOTIFICATION_INVALID", category: "METHOD"},
		{name: "known SDK unsupported method", method: "thread/attachment/updated", payload: json.RawMessage(`{}`), kind: messageNotification, detail: "NOTIFICATION_INVALID", category: "METHOD"},
		{name: "envelope", method: "item/started", payload: json.RawMessage(`{"private":"private-sentinel"}`), kind: messageNotification, detail: "NOTIFICATION_INVALID", category: "ENVELOPE"},
		{name: "tuple", method: "item/started", payload: json.RawMessage(`{"threadId":"other","turnId":"turn-1","startedAtMs":1,"item":{"id":"item-1","type":"unknown"}}`), kind: messageNotification, detail: "NOTIFICATION_INVALID", category: "TUPLE"},
		{name: "item", method: "item/started", payload: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","startedAtMs":1,"item":{"id":"item-1","type":"private-sentinel"}}`), kind: messageNotification, detail: "NOTIFICATION_INVALID", category: "ITEM"},
		{name: "stream", streamError: errors.New("private-sentinel"), detail: "STREAM_INVALID"},
		{name: "uncorrelated", kind: messageResponse, detail: "RESPONSE_CORRELATION"},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages := make(chan streamEvent, 1)
			messages <- streamEvent{message: wireMessage{kind: test.kind, method: test.method, payload: test.payload}, err: test.streamError}
			close(messages)
			state := newProtocolState("")
			state.threadID, state.turnID = "thread-1", "turn-1"
			err := (&appServer{messages: messages}).waitTerminal(t.Context(), state)
			var failure *appServerCallFailure
			if !errors.As(err, &failure) || failure.detail != test.detail || failure.notificationError != test.category || state.terminals != 0 {
				t.Fatal("terminal wait discarded or changed closed failure diagnostics")
			}
		})
	}
}

func TestWaitTerminalClosedStreamAndCancellationDiagnostics(t *testing.T) {
	t.Parallel()
	messages := make(chan streamEvent)
	close(messages)
	err := (&appServer{messages: messages}).waitTerminal(t.Context(), newProtocolState(""))
	var failure *appServerCallFailure
	if !errors.As(err, &failure) || failure.detail != "STREAM_CLOSED" {
		t.Fatal("closed terminal stream category was discarded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = (&appServer{messages: make(chan streamEvent)}).waitTerminal(ctx, newProtocolState(""))
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.detail != "CONTEXT_CANCELLED" {
		t.Fatal("terminal cancellation category or identity was discarded")
	}
}

func TestProtocolErrorPreservesClosedAccountReadReason(t *testing.T) {
	t.Parallel()
	err := protocolError("account/read", json.RawMessage(`{"code":-32603,"message":"workspace routing discovery failed","data":{"private":"private-sentinel"}}`))
	var failure *appServerCallFailure
	if !errors.As(err, &failure) || failure.accountRead != "DISCOVERY_FAILED" || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatal("account read diagnostic did not preserve only the closed reason")
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
		{name: "ChatGPT Pro Max account", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null,"planType":"promax"},"requiresOpenaiAuth":true}`)},
		{name: "ChatGPT workspace routing", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null,"planType":"promax"},"requiresOpenaiAuth":true,"workspaceRouting":{"chatgptAccountId":"synthetic-private-account","backendOrigin":"https://chatgpt.com/backend-api","accountRoutingOverride":"NO_CONSTRAINT"}}`)},
		{name: "null workspace routing", raw: json.RawMessage(`{"account":{"type":"apiKey"},"requiresOpenaiAuth":true,"workspaceRouting":null}`)},
		{name: "unknown ChatGPT plan remains invalid", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null,"planType":"future-plan"},"requiresOpenaiAuth":true}`), wantErr: true},
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

func TestAccountReadWorkspaceRoutingClosedSchema(t *testing.T) {
	t.Parallel()
	const account = `{"account":{"type":"chatgpt","email":null,"planType":"pro"},"requiresOpenaiAuth":true,"workspaceRouting":`
	const valid = `{"chatgptAccountId":"synthetic-private-account","backendOrigin":"https://chatgpt.com/backend-api","accountRoutingOverride":"NO_CONSTRAINT"}`
	tests := []struct {
		name, routing string
		valid         bool
	}{
		{"null", `null`, true},
		{"no_constraint", valid, true},
		{"us", strings.Replace(valid, "NO_CONSTRAINT", "us", 1), true},
		{"us_cr", strings.Replace(valid, "NO_CONSTRAINT", "us_cr", 1), true},
		{"unknown_override", strings.Replace(valid, "NO_CONSTRAINT", "private-unknown", 1), false},
		{"wrong_override_case", strings.Replace(valid, "NO_CONSTRAINT", "no_constraint", 1), false},
		{"override_null", strings.Replace(valid, `"NO_CONSTRAINT"`, `null`, 1), false},
		{"override_object", strings.Replace(valid, `"NO_CONSTRAINT"`, `{}`, 1), false},
		{"override_number", strings.Replace(valid, `"NO_CONSTRAINT"`, `123`, 1), false},
		{"empty_object", `{}`, false},
		{"wrong_object_type", `[]`, false},
		{"string", `"synthetic-private-account"`, false},
		{"boolean", `false`, false},
		{"number", `123`, false},
		{"missing_account_id", `{"backendOrigin":"https://chatgpt.com/backend-api","accountRoutingOverride":"us"}`, false},
		{"missing_origin", `{"chatgptAccountId":"synthetic-private-account","accountRoutingOverride":"us"}`, false},
		{"missing_override", `{"chatgptAccountId":"synthetic-private-account","backendOrigin":"https://chatgpt.com/backend-api"}`, false},
		{"null_account_id", strings.Replace(valid, `"synthetic-private-account"`, `null`, 1), false},
		{"array_account_id", strings.Replace(valid, `"synthetic-private-account"`, `[]`, 1), false},
		{"null_origin", strings.Replace(valid, `"https://chatgpt.com/backend-api"`, `null`, 1), false},
		{"boolean_origin", strings.Replace(valid, `"https://chatgpt.com/backend-api"`, `true`, 1), false},
		{"empty_account_id", strings.Replace(valid, `"synthetic-private-account"`, `""`, 1), false},
		{"empty_origin", strings.Replace(valid, `"https://chatgpt.com/backend-api"`, `""`, 1), false},
		{"account_id_limit", strings.Replace(valid, "synthetic-private-account", strings.Repeat("p", 512), 1), true},
		{"account_id_over_limit", strings.Replace(valid, "synthetic-private-account", strings.Repeat("p", 513), 1), false},
		{"origin_limit", strings.Replace(valid, "https://chatgpt.com/backend-api", strings.Repeat("p", 4096), 1), true},
		{"origin_over_limit", strings.Replace(valid, "https://chatgpt.com/backend-api", strings.Repeat("p", 4097), 1), false},
		{"unknown_field", strings.TrimSuffix(valid, "}") + `,"synthetic-private-field":"private-email@example.invalid"}`, false},
		{"duplicate_account_id", strings.TrimSuffix(valid, "}") + `,"chatgptAccountId":"synthetic-private-account"}`, false},
		{"duplicate_override", strings.TrimSuffix(valid, "}") + `,"accountRoutingOverride":"us"}`, false},
		{"raw_over_limit", strings.Replace(valid, "{", "{"+strings.Repeat(" ", 16<<10), 1), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := classifyAccountReadResponse(json.RawMessage(account+test.routing+`}`), nil)
			if (err == nil) != test.valid {
				t.Fatalf("closed account/read validation outcome is incorrect: valid=%t", test.valid)
			}
			if err != nil && !errors.Is(err, errAccountReadResponseInvalid) {
				t.Fatal("invalid routing escaped the closed account/read schema error")
			}
			if err != nil && (strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "@")) {
				t.Fatal("account/read schema error leaked private fixture values")
			}
		})
	}
	if err := classifyAccountReadResponse(json.RawMessage(`{"account":null,"requiresOpenaiAuth":true,"workspaceRouting":null}`), nil); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatal("null workspace routing must not bypass required authentication")
	}
	if err := classifyAccountReadResponse(json.RawMessage(account+valid+`,"workspaceRouting":null}`), nil); !errors.Is(err, errAccountReadResponseInvalid) {
		t.Fatal("duplicate top-level workspace routing must fail closed")
	}
}

func TestAppServerEnvironmentPreservesOnlyRequiredEgressProxy(t *testing.T) {
	setRuntimeTransportFixture(t)
	t.Setenv("UNRELATED_SECRET", "must-not-be-forwarded")

	environment := appServerEnvironment(model.Input{CodexHome: "/workspace/.kodex"}, "mcp-token")
	for _, expected := range []string{
		"HTTP_PROXY=" + syntheticRuntimeProxy,
		"HTTPS_PROXY=" + syntheticRuntimeProxy,
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
		if method == "thread/tokenUsage/updated" || method == "rawResponse/completed" {
			t.Fatal("usage observation notification is required")
		}
	}
}

func TestRawProviderResponseItemsRemainSuppressed(t *testing.T) {
	t.Parallel()
	for _, required := range []string{"rawResponseItem/completed"} {
		if !slices.Contains(suppressedNotificationMethods, required) {
			t.Fatalf("sensitive provider notification %q is not suppressed", required)
		}
	}
}

func TestRequiredMCPToolNamesMatchRuntimeAuthority(t *testing.T) {
	input := model.Input{AssistantScope: runtimecontract.AssistantScopeSystem}
	input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{})
	input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{})
	actual := RequiredMCPToolNames(input)
	expected := []string{
		"propose_run_metadata",
		"get_configuration_catalog",
		"find_platform_resources",
		"propose_configuration_plan",
		"propose_assistant_metadata",
		"delegate_agent",
		"get_integration_catalog",
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
