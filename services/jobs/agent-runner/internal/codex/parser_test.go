package codex

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	testThreadID = "01980000-0000-7000-8000-000000000001"
	testTurnID   = "01980000-0000-7000-8000-000000000002"
)

func TestProtocolAcceptsStructuredSuccess(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	state.turnID = testTurnID
	state.result.SessionID = testThreadID
	if err := state.notification("turn/started", raw(`{
		"threadId":"`+testThreadID+`","turn":{"id":"`+testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
		t.Fatalf("turn start rejected: %v", err)
	}
	item := `{"id":"message-1","text":"готово","phase":"final_answer","type":"agentMessage"}`
	if err := state.notification("item/completed", raw(`{"completedAtMs":1,"item":`+item+`,"threadId":"`+
		testThreadID+`","turnId":"`+testTurnID+`"}`)); err != nil {
		t.Fatalf("item completion rejected: %v", err)
	}
	if err := state.notification("turn/completed", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+
		testTurnID+`","items":[`+item+`],"status":"completed"}}`)); err != nil {
		t.Fatalf("turn completion rejected: %v", err)
	}
	if state.result.Outcome != "SUCCEEDED" || state.result.FinalMessage != "готово" {
		t.Fatalf("unexpected result: %#v", state.result)
	}
}

func TestNativeToolItemsProduceBoundedRedactedTimelineWithoutMCPDuplicates(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.workspaceRoot = "/workspace"
	items := []struct {
		kind string
		raw  string
	}{
		{runtimecontract.NativeToolKindShell, `{"aggregatedOutput":"SECRET_OUTPUT","command":"printf SECRET_COMMAND","commandActions":[{"command":"cat SECRET_FILE","name":"cat","path":"/workspace/private.txt","type":"read"}],"cwd":"/workspace","durationMs":25,"exitCode":0,"id":"call-shell","source":"agent","status":"completed","type":"commandExecution"}`},
		{runtimecontract.NativeToolKindFileChange, `{"changes":[{"diff":"SECRET_DIFF","kind":{"type":"update"},"path":"/workspace/internal/file.go"},{"diff":"SECRET_DIFF_2","kind":{"type":"add"},"path":"/etc/outside"}],"id":"call-file","status":"completed","type":"fileChange"}`},
		{runtimecontract.NativeToolKindWebSearch, `{"action":{"queries":["SECRET_QUERY"],"type":"search"},"id":"call-web","query":"SECRET_QUERY","results":[{"body":"SECRET_RESULT"}],"type":"webSearch"}`},
		{runtimecontract.NativeToolKindDynamicTool, `{"arguments":{"password":"SECRET_ARGUMENT","nested":{"token":"SECRET_TOKEN"}},"contentItems":[{"text":"SECRET_RESULT"}],"durationMs":18,"id":"call-dynamic","namespace":"workspace.tools","status":"completed","success":true,"tool":"inspect","type":"dynamicToolCall"}`},
		{runtimecontract.NativeToolKindImageView, `{"id":"call-image-view","path":"/workspace/assets/example.png","type":"imageView"}`},
		{runtimecontract.NativeToolKindSleep, `{"durationMs":50,"id":"call-sleep","type":"sleep"}`},
		{runtimecontract.NativeToolKindImageGeneration, `{"id":"call-image-generation","result":"SECRET_IMAGE_RESULT","revisedPrompt":"SECRET_PROMPT","savedPath":"/workspace/out/generated.png","status":"completed","type":"imageGeneration"}`},
	}
	for index, item := range items {
		startedAt := int64(100 + index*100)
		if err := state.consumeItem(raw(item.raw), false, startedAt); err != nil {
			t.Fatalf("start %s: %v", item.kind, err)
		}
		if err := state.consumeItem(raw(item.raw), true, startedAt+40); err != nil {
			t.Fatalf("complete %s: %v", item.kind, err)
		}
		if err := state.consumeItem(raw(item.raw), true, 0); err != nil {
			t.Fatalf("authoritative duplicate %s: %v", item.kind, err)
		}
	}
	mcp := raw(`{"arguments":{"token":"SECRET_MCP"},"durationMs":10,"id":"call-mcp","result":{"content":"SECRET_MCP_RESULT"},"server":"kodex-runtime-tools","status":"completed","tool":"delegate_agent","type":"mcpToolCall"}`)
	if err := state.consumeItem(mcp, true, 999); err != nil {
		t.Fatalf("MCP item was not safely ignored: %v", err)
	}
	if len(state.toolCallOrder) != len(items) {
		t.Fatalf("native calls = %d, want %d", len(state.toolCallOrder), len(items))
	}
	for index, expected := range items {
		call := state.toolCalls[state.toolCallOrder[index]]
		if call.Kind != expected.kind || call.State != runtimecontract.NativeToolStateSucceeded || call.SafeResult != runtimecontract.NativeToolResultCompleted {
			t.Fatalf("unexpected native projection: %#v", call)
		}
		if call.DurationMS <= 0 {
			t.Fatalf("duration was not retained for %s: %#v", expected.kind, call)
		}
	}
	encoded, err := json.Marshal(state.toolCalls)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{
		[]byte("SECRET_OUTPUT"), []byte("SECRET_COMMAND"), []byte("SECRET_FILE"), []byte("SECRET_DIFF"),
		[]byte("SECRET_QUERY"), []byte("SECRET_RESULT"), []byte("SECRET_ARGUMENT"), []byte("SECRET_TOKEN"),
		[]byte("SECRET_IMAGE_RESULT"), []byte("SECRET_PROMPT"), []byte("SECRET_MCP"),
	} {
		if bytes.Contains(encoded, forbidden) {
			t.Fatalf("sensitive native tool data escaped projection: %s", forbidden)
		}
	}
	fileCall := state.toolCalls["call-file"]
	if got := fileCall.SafeParameters["paths"]; !reflectStringSlice(got, []string{"internal/file.go"}) {
		t.Fatalf("file paths = %#v", got)
	}
	state.result.SessionID = testThreadID
	state.result.Outcome = "SUCCEEDED"
	state.threadPath = "/workspace/.kodex/state/codex-home/sessions/test.jsonl"
	state.terminals = 1
	state.baselineCaptured = true
	result, err := state.terminalResult()
	if err != nil || len(result.ToolCalls) != len(items) || result.ToolCalls[0].CallID != "call-shell" {
		t.Fatalf("terminal native tool timeline = %#v, err=%v", result.ToolCalls, err)
	}
}

func TestNativeToolTerminalStateIsClosedAndCorrelatedByItemID(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.workspaceRoot = "/workspace"
	failed := raw(`{"command":"false","commandActions":[{"command":"false","type":"unknown"}],"cwd":"/workspace","exitCode":1,"id":"call-failed","status":"completed","type":"commandExecution"}`)
	if err := state.consumeItem(failed, true, 10); err != nil {
		t.Fatal(err)
	}
	call := state.toolCalls["call-failed"]
	if call.CallID != "call-failed" || call.State != runtimecontract.NativeToolStateFailed || call.SafeResult != runtimecontract.NativeToolResultFailed {
		t.Fatalf("failed command projection = %#v", call)
	}
	unknown := raw(`{"command":"true","commandActions":[],"cwd":"/workspace","id":"call-unknown","status":"future","type":"commandExecution"}`)
	if err := state.consumeItem(unknown, true, 20); err == nil {
		t.Fatal("unknown native tool state was accepted")
	}
}

func reflectStringSlice(value any, expected []string) bool {
	items, ok := value.([]string)
	if !ok || len(items) != len(expected) {
		return false
	}
	for index := range items {
		if items[index] != expected[index] {
			return false
		}
	}
	return true
}

func TestThreadBindingAcceptsCurrentAppServerOptionalFields(t *testing.T) {
	state := newProtocolState("")
	response := raw(`{
		"activePermissionProfile":{"id":"kodex-runtime"},
		"approvalPolicy":"never","approvalsReviewer":"user","cwd":"/workspace",
		"instructionSources":[],"model":"codex","modelProvider":"openai",
		"multiAgentMode":"explicitRequestOnly","reasoningEffort":null,
		"runtimeWorkspaceRoots":["/workspace"],"sandbox":{"type":"readOnly"},"serviceTier":null,
		"thread":{"cliVersion":"0.144.1","createdAt":1,"cwd":"/workspace","ephemeral":false,
		"extra":null,"historyMode":"save-all","id":"` + testThreadID + `","modelProvider":"openai",
		"preview":"","sessionId":"` + testThreadID + `","source":"startup",
		"status":{"type":"idle"},"turns":[],"updatedAt":1}}`)
	if err := state.bindThread(response, "codex", "/workspace", "never"); err != nil {
		t.Fatalf("current app-server thread response was rejected: %v", err)
	}
}

func TestTokenUsageNotificationProducesCurrentTurnDelta(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	state.threadPath = "/workspace/.kodex/state/codex-home/sessions/2026/08/27/rollout-test.jsonl"
	state.result.SessionID = testThreadID
	if err := state.notification("thread/tokenUsage/updated", tokenUsageNotification(
		"01980000-0000-7000-8000-000000000099", 100, 80, 20, 10, 20, 5,
	)); err != nil {
		t.Fatalf("baseline usage rejected: %v", err)
	}
	if err := state.captureUsageBaseline(); err != nil {
		t.Fatalf("capture baseline: %v", err)
	}
	state.turnID = testTurnID
	if err := state.notification("thread/tokenUsage/updated", tokenUsageNotification(
		testTurnID, 170, 140, 60, 20, 30, 8,
	)); err != nil {
		t.Fatalf("final usage rejected: %v", err)
	}
	state.terminals = 1
	state.result.Outcome = "SUCCEEDED"
	result, err := state.terminalResult()
	if err != nil {
		t.Fatalf("terminal usage: %v", err)
	}
	want := struct {
		total, input, cached, cacheWrite, output, reasoning, window int64
	}{70, 60, 40, 0, 10, 3, 200000}
	if result.Usage.TotalTokens != want.total || result.Usage.InputTokens != want.input ||
		result.Usage.CachedInputTokens != want.cached || result.Usage.CacheWriteInputTokens != want.cacheWrite ||
		result.Usage.OutputTokens != want.output || result.Usage.ReasoningOutputTokens != want.reasoning ||
		result.Usage.ModelContextWindow != want.window {
		t.Fatalf("unexpected turn usage: %#v", result.Usage)
	}
}

func TestTokenUsageDeltaNeverBecomesNegative(t *testing.T) {
	baseline, err := parseTokenUsage(raw(`{"total":{"totalTokens":100,"inputTokens":80,"cachedInputTokens":20,"outputTokens":20,"reasoningOutputTokens":5},"last":{"totalTokens":50,"inputTokens":40,"cachedInputTokens":10,"outputTokens":10,"reasoningOutputTokens":2},"modelContextWindow":200000}`))
	if err != nil {
		t.Fatal(err)
	}
	final, err := parseTokenUsage(raw(`{"total":{"totalTokens":90,"inputTokens":70,"cachedInputTokens":15,"outputTokens":20,"reasoningOutputTokens":4},"last":{"totalTokens":40,"inputTokens":30,"cachedInputTokens":5,"outputTokens":10,"reasoningOutputTokens":2},"modelContextWindow":200000}`))
	if err != nil {
		t.Fatal(err)
	}
	delta, err := tokenUsageDelta(final, baseline)
	if err != nil || delta.TotalTokens != 0 || delta.InputTokens != 0 || delta.CachedInputTokens != 0 ||
		delta.CacheWriteInputTokens != 0 || delta.OutputTokens != 0 || delta.ReasoningOutputTokens != 0 {
		t.Fatalf("usage reset was not clamped: usage=%#v err=%v", delta, err)
	}
}

func TestTokenUsageNotificationRejectsInconsistentBreakdown(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	invalid := raw(`{"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","tokenUsage":{"total":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":11,"outputTokens":10,"reasoningOutputTokens":0},"last":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":11,"outputTokens":10,"reasoningOutputTokens":0},"modelContextWindow":200000}}`)
	if err := state.notification("thread/tokenUsage/updated", invalid); err == nil {
		t.Fatal("inconsistent token usage was accepted")
	}
}

func tokenUsageNotification(turnID string, total, input, cached, cacheWrite, output, reasoning int64) json.RawMessage {
	_ = cacheWrite
	return raw(`{"threadId":"` + testThreadID + `","turnId":"` + turnID + `","tokenUsage":{"total":{"totalTokens":` +
		itoa(total) + `,"inputTokens":` + itoa(input) + `,"cachedInputTokens":` + itoa(cached) +
		`,"outputTokens":` + itoa(output) +
		`,"reasoningOutputTokens":` + itoa(reasoning) + `},"last":{"totalTokens":` + itoa(total) +
		`,"inputTokens":` + itoa(input) + `,"cachedInputTokens":` + itoa(cached) +
		`,"outputTokens":` + itoa(output) +
		`,"reasoningOutputTokens":` + itoa(reasoning) + `},"modelContextWindow":200000}}`)
}

func TestTokenUsageAllowsUnknownContextWindow(t *testing.T) {
	for _, input := range []json.RawMessage{
		raw(`{"total":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":5,"outputTokens":10,"reasoningOutputTokens":2},"last":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":5,"outputTokens":10,"reasoningOutputTokens":2}}`),
		raw(`{"total":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":5,"outputTokens":10,"reasoningOutputTokens":2},"last":{"totalTokens":20,"inputTokens":10,"cachedInputTokens":5,"outputTokens":10,"reasoningOutputTokens":2},"modelContextWindow":null}`),
	} {
		usage, err := parseTokenUsage(input)
		if err != nil || usage.ModelContextWindow != 0 || usage.CacheWriteInputTokens != 0 {
			t.Fatalf("unknown context window was rejected: usage=%#v err=%v", usage, err)
		}
	}
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }

func TestOnlyServerOverloadedIsCapacity(t *testing.T) {
	for _, test := range []struct {
		info     string
		expected string
		capacity bool
	}{
		{`"serverOverloaded"`, "server_overloaded", true},
		{`"usageLimitExceeded"`, "usage_limit_exceeded", false},
		{`"unauthorized"`, "unauthorized", false},
		{`"cyberPolicy"`, "cyber_policy", false},
		{`"futureVariant"`, "provider_error_info_invalid", false},
		{`null`, "provider_error_info_invalid", false},
	} {
		actual := classifyCodexErrorInfo(raw(test.info))
		if actual != test.expected || CapacityFailure(actual) != test.capacity {
			t.Fatalf("classification mismatch for %s: code=%s capacity=%t", test.info, actual, CapacityFailure(actual))
		}
	}
}

func TestFailedTurnWithoutTypedErrorRemainsOwnerVisible(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.result.SessionID = testThreadID, testTurnID, testThreadID
	if err := state.notification("turn/started", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+
		testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := state.notification("turn/completed", raw(`{"threadId":"`+testThreadID+`","turn":{"error":{"message":"diagnostic"},"id":"`+
		testTurnID+`","items":[],"status":"failed"}}`)); err != nil {
		t.Fatalf("failed terminal rejected: %v", err)
	}
	if state.result.FailureCode != "provider_error_info_invalid" || !BlockedFailure(state.result.FailureCode) {
		t.Fatalf("missing error info was not converted to a safe terminal: %#v", state.result)
	}
}

func TestUnknownTypedErrorNotificationWaitsForSafeTerminal(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.result.SessionID = testThreadID, testTurnID, testThreadID
	if err := state.notification("error", raw(`{"error":{"codexErrorInfo":"futureVariant","message":"diagnostic"},"threadId":"`+
		testThreadID+`","turnId":"`+testTurnID+`","willRetry":false}`)); err != nil {
		t.Fatalf("unknown typed provider error must remain non-authoritative until terminal: %v", err)
	}
	if err := state.notification("turn/started", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+
		testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := state.notification("turn/completed", raw(`{"threadId":"`+testThreadID+`","turn":{"error":{"codexErrorInfo":"futureVariant","message":"diagnostic"},"id":"`+
		testTurnID+`","items":[],"status":"failed"}}`)); err != nil {
		t.Fatal(err)
	}
	if state.result.FailureCode != "provider_error_info_invalid" || !BlockedFailure(state.result.FailureCode) {
		t.Fatalf("unknown error info was not converted to a safe terminal: %#v", state.result)
	}
}

func TestWireParserRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, value := range []string{
		`{"id":1,"result":{},"authority":"payload"}`,
		`{"id":1,"id":2,"result":{}}`,
		`{"method":"turn/completed","params":{},"result":{}}`,
	} {
		if _, err := parseWireMessage([]byte(value)); err == nil {
			t.Fatalf("invalid wire value accepted: %s", value)
		}
	}
}

func TestServerRequestSetIsClosed(t *testing.T) {
	if _, ok := serverRequestMethods["item/commandExecution/requestApproval"]; !ok {
		t.Fatal("approval request method is missing")
	}
	if _, ok := serverRequestMethods["future/approval"]; ok {
		t.Fatal("unknown request method is allowed")
	}
}

func TestNotificationSchemasCoverExactClosedMethodSet(t *testing.T) {
	if len(serverNotificationMethods) != len(notificationSchemas) {
		t.Fatalf("notification schema count mismatch: methods=%d schemas=%d", len(serverNotificationMethods), len(notificationSchemas))
	}
	for method := range serverNotificationMethods {
		if _, ok := notificationSchemas[method]; !ok {
			t.Fatalf("notification method has no schema: %s", method)
		}
	}
	for method := range notificationSchemas {
		if _, ok := serverNotificationMethods[method]; !ok {
			t.Fatalf("notification schema is not in the closed method set: %s", method)
		}
	}
}

func TestTerminalPresentationKeepsCapacityQuotaAndPolicySeparate(t *testing.T) {
	tests := []struct {
		code, outcome, action string
	}{
		{"server_overloaded", "FAILED", "RETRY_LATER"},
		{"usage_limit_exceeded", "BLOCKED", "CHECK_PROVIDER_QUOTA"},
		{"unauthorized", "BLOCKED", "REAUTH_DEVICE_CODE"},
		{"cyber_policy", "BLOCKED", "REVIEW_POLICY"},
		{"provider_error_info_invalid", "FAILED", "RETRY_FRESH_TURN"},
	}
	for _, test := range tests {
		outcome, markdown, action := TerminalPresentation(test.code)
		if outcome != test.outcome || action != test.action || markdown == "" {
			t.Fatalf("unexpected terminal mapping for %q: %q %q %q", test.code, outcome, action, markdown)
		}
	}
}

func TestTerminalPresentationCoversEveryParsedProviderFailure(t *testing.T) {
	for _, code := range []string{
		"context_window_exceeded", "session_budget_exceeded", "usage_limit_exceeded",
		"server_overloaded", "cyber_policy", "provider_internal_error", "unauthorized",
		"provider_bad_request", "thread_rollback_failed", "provider_sandbox_error",
		"provider_other_error", "active_turn_not_steerable", "provider_transport_failure",
	} {
		_, markdown, action := TerminalPresentation(code)
		if markdown == "i18n:PROVIDER_RESULT_UNKNOWN" || action == "" {
			t.Fatalf("parsed provider failure %q has no explicit terminal presentation", code)
		}
	}
}

func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}
