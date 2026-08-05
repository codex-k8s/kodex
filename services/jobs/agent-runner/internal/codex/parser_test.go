package codex

import (
	"encoding/json"
	"testing"
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

func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}
