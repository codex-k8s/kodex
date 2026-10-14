package codex

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Это синтетическая проверка существующей opaque-проекции, не новый контракт
// account RPC и не доказательство финансового допуска конкретного аккаунта.
func TestRateLimitSnapshotDoesNotAssignLocalAdmission(t *testing.T) {
	for _, snapshot := range []string{
		`{"primary":{"usedPercent":100,"windowDurationMins":10080},"credits":{"hasCredits":true,"unlimited":false,"balance":"SYNTHETIC_CREDIT_SENTINEL"}}`,
		`{"primary":{"usedPercent":100,"windowDurationMins":10080},"credits":{"hasCredits":false,"unlimited":false,"balance":null}}`,
		`{"primary":{"usedPercent":100,"windowDurationMins":10080},"credits":{"hasCredits":true,"unlimited":true,"balance":null},"ordinaryUsageAllowed":false,"spendControl":"SYNTHETIC_SPEND_SENTINEL"}`,
		`{"primary":{"usedPercent":0,"windowDurationMins":10080},"credits":null}`,
	} {
		state := newProtocolState(testThreadID)
		state.threadID, state.turnID = testThreadID, testTurnID
		state.result.SessionID = testThreadID
		before := state.result
		if err := state.notification("account/rateLimits/updated", raw(`{"rateLimits":`+snapshot+`}`)); err != nil {
			t.Fatalf("rate limit metadata rejected: %v", err)
		}
		if !reflect.DeepEqual(before, state.result) || state.terminals != 0 || state.turnStarted != 0 {
			t.Fatal("rate limit snapshot changed execution admission or result")
		}
		encoded, err := json.Marshal(state.result)
		if err != nil || strings.Contains(string(encoded), "SYNTHETIC_CREDIT_SENTINEL") || strings.Contains(string(encoded), "SYNTHETIC_SPEND_SENTINEL") {
			t.Fatal("opaque financial metadata escaped into execution result")
		}
		if err := state.notification("turn/started", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
			t.Fatal(err)
		}
		item := `{"id":"rate-limit-success","text":"готово","phase":"final_answer","questions":null,"type":"agentMessage"}`
		if err := state.notification("item/completed", raw(`{"completedAtMs":1,"item":`+item+`,"threadId":"`+testThreadID+`","turnId":"`+testTurnID+`"}`)); err != nil {
			t.Fatal(err)
		}
		if err := state.notification("turn/completed", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+testTurnID+`","items":[`+item+`],"status":"completed"}}`)); err != nil {
			t.Fatal(err)
		}
		if state.result.Outcome != "SUCCEEDED" || state.result.FailureCode != "" {
			t.Fatal("rate limit snapshot blocked an otherwise successful turn")
		}
	}
	if !slices.Contains(suppressedNotificationMethods, "account/rateLimits/updated") {
		t.Fatal("financial metadata notification is not suppressed")
	}
}

func TestRateLimitSnapshotCannotOverrideTypedProviderDenial(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID = testThreadID, testTurnID
	state.result.SessionID = testThreadID
	if err := state.notification("turn/started", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := state.notification("account/rateLimits/updated", raw(`{"rateLimits":{"primary":{"usedPercent":100},"credits":{"hasCredits":true}}}`)); err != nil {
		t.Fatal(err)
	}
	terminal := raw(`{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[],"status":"failed","error":{"message":"synthetic denial","codexErrorInfo":"usageLimitExceeded"}}}`)
	if err := state.notification("turn/completed", terminal); err != nil {
		t.Fatal(err)
	}
	if state.result.FailureCode != "usage_limit_exceeded" || !BlockedFailure(state.result.FailureCode) || CapacityFailure(state.result.FailureCode) {
		t.Fatal("typed provider denial was overridden or classified as capacity")
	}
	outcome, _, action := TerminalPresentation(state.result.FailureCode)
	if outcome != "BLOCKED" || action != "CHECK_PROVIDER_QUOTA" {
		t.Fatal("typed quota denial lost its closed presentation")
	}
	before := state.result
	if err := state.notification("account/rateLimits/updated", raw(`{"rateLimits":{"credits":{"hasCredits":true,"unlimited":true}}}`)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, state.result) || state.terminals != 1 {
		t.Fatal("financial metadata rewrote terminal execution")
	}
}

func TestRateLimitSnapshotDoesNotRelaxOuterEnvelopeOrTurnBinding(t *testing.T) {
	for _, payload := range []string{
		`{}`,
		`{"rateLimits":{},"ordinaryUsageAllowed":true}`,
		`{"rateLimits":{},"rateLimits":{}}`,
		`{"rateLimits":{}} {}`,
	} {
		state := newProtocolState(testThreadID)
		if err := state.notification("account/rateLimits/updated", raw(payload)); err == nil {
			t.Fatal("invalid financial metadata envelope accepted")
		}
	}
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID = testThreadID, testTurnID
	if err := state.notification("turn/completed", raw(`{"threadId":"01980000-0000-7000-8000-000000000099","turn":{"id":"`+testTurnID+`","items":[],"status":"failed","error":{"message":"synthetic denial","codexErrorInfo":"usageLimitExceeded"}}}`)); err == nil {
		t.Fatal("foreign quota terminal accepted")
	}
}
