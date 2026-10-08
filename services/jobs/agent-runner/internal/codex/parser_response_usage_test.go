package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func responseUsageNotification(responseID, usage string) json.RawMessage {
	return raw(`{"responseId":"` + responseID + `","threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","usage":` + usage + `,"usageMetadata":null}`)
}

func measuredUsageState(t *testing.T) *protocolState {
	t.Helper()
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	if err := state.captureUsageBaseline(); err != nil {
		t.Fatal(err)
	}
	state.turnID = testTurnID
	return state
}

func TestResponseUsageNotCompactionDisplayIsMeasured(t *testing.T) {
	state := measuredUsageState(t)
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-1", codex160UsageBreakdownFixture)); err != nil {
		t.Fatal(err)
	}
	for _, display := range []string{
		`{"total":{"totalTokens":258400,"inputTokens":0,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0},"last":{"totalTokens":258300,"inputTokens":0,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0},"modelContextWindow":258400}`,
		`{"total":{"totalTokens":258500,"inputTokens":80,"cachedInputTokens":20,"outputTokens":20,"reasoningOutputTokens":5},"last":{"totalTokens":300000,"inputTokens":0,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0},"modelContextWindow":258400}`,
	} {
		if err := state.notification("thread/tokenUsage/updated", raw(`{"threadId":"`+testThreadID+`","turnId":"`+testTurnID+`","tokenUsage":`+display+`}`)); err != nil {
			t.Fatalf("source-defined display estimate rejected: %v", err)
		}
	}
	want := runtimecontract.TokenUsage{TotalTokens: 100, InputTokens: 80, CachedInputTokens: 20, OutputTokens: 20, ReasoningOutputTokens: 5, ModelContextWindow: 258400}
	if got := state.measuredResult().Usage; got != want {
		t.Fatalf("display changed measured provider usage: %#v", got)
	}
}

func TestResponseUsageDeduplicatesAndRejectsChangedReceipt(t *testing.T) {
	state := measuredUsageState(t)
	observation := responseUsageNotification("response-1", codex160UsageBreakdownFixture)
	for range 2 {
		if err := state.notification("rawResponse/completed", observation); err != nil {
			t.Fatal(err)
		}
	}
	before := state.measuredResult()
	if before.Usage.TotalTokens != 100 {
		t.Fatal("response receipt was ignored or charged twice")
	}
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-1", `{"totalTokens":101,"inputTokens":81,"cachedInputTokens":20,"outputTokens":20,"reasoningOutputTokens":5}`)); err == nil {
		t.Fatal("changed response receipt accepted")
	}
	if state.measuredResult().Usage != before.Usage {
		t.Fatal("failed receipt changed measured subtotal")
	}
}

func TestResponseUsageOverflowPreservesPriorMeasurements(t *testing.T) {
	state := measuredUsageState(t)
	maximum := fmt.Sprintf(`{"totalTokens":%d,"inputTokens":%d,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0}`, int64(1<<63-1), int64(1<<63-1))
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-1", maximum)); err != nil {
		t.Fatal(err)
	}
	before := state.measuredResult().Usage
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-2", codex160UsageBreakdownFixture)); err == nil {
		t.Fatal("aggregate overflow accepted")
	}
	if state.measuredResult().Usage != before {
		t.Fatal("overflow changed prior measurements")
	}
}

func TestResponseUsageNullIsUnknownOrPartialWithoutFailingLifecycle(t *testing.T) {
	for _, numeric := range []bool{false, true} {
		t.Run(fmt.Sprint(numeric), func(t *testing.T) {
			state := measuredUsageState(t)
			if numeric {
				if err := state.notification("rawResponse/completed", responseUsageNotification("response-numeric", codex160UsageBreakdownFixture)); err != nil {
					t.Fatal(err)
				}
			}
			if err := state.notification("rawResponse/completed", responseUsageNotification("response-null", "null")); err != nil {
				t.Fatal("valid upstream Option null prevented lifecycle")
			}
			state.terminals, state.result.SessionID, state.result.Outcome = 1, testThreadID, "SUCCEEDED"
			state.threadPath = "/workspace/rollout-fixture.jsonl"
			result, err := state.terminalResult()
			if err != nil {
				t.Fatal(err)
			}
			wantQuality, wantTotal := UsageUnknown, int64(0)
			if numeric {
				wantQuality, wantTotal = UsagePartial, 100
			}
			if result.UsageCompleteness != wantQuality || result.Usage.TotalTokens != wantTotal {
				t.Fatal("null fabricated a complete measurement or lost numeric subtotal")
			}
			if failed := failedProviderResult(result); failed.UsageCompleteness != wantQuality || failed.Usage != result.Usage {
				t.Fatal("failure envelope lost accounting observations")
			}
		})
	}
}

func TestResponseUsageCompleteRequiresNumericObservationAndTerminal(t *testing.T) {
	state := measuredUsageState(t)
	if state.measuredResult().UsageCompleteness != UsageUnknown {
		t.Fatal("no observations declared complete")
	}
	zero := `{"totalTokens":0,"inputTokens":0,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0}`
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-zero", zero)); err != nil {
		t.Fatal(err)
	}
	if state.measuredResult().UsageCompleteness != UsagePartial {
		t.Fatal("incomplete lifecycle declared complete")
	}
	state.terminals = 1
	if state.measuredResult().UsageCompleteness != UsageComplete {
		t.Fatal("explicit measured zero was not distinguished from unknown")
	}
}

func TestResponseUsageClosedNumericValidationAndTupleIsolation(t *testing.T) {
	valid := string(responseUsageNotification("response-1", codex160UsageBreakdownFixture))
	for name, input := range map[string]string{
		"absent usage":     strings.Replace(valid, `"usage":`+codex160UsageBreakdownFixture+`,`, "", 1),
		"null counter":     strings.Replace(valid, `"inputTokens":80`, `"inputTokens":null`, 1),
		"unknown":          strings.Replace(valid, `"inputTokens":80`, `"PRIVATE_BODY_SENTINEL":80`, 1),
		"duplicate":        strings.Replace(valid, `"inputTokens":80`, `"inputTokens":80,"inputTokens":80`, 1),
		"type":             strings.Replace(valid, `"inputTokens":80`, `"inputTokens":"PRIVATE_BODY_SENTINEL"`, 1),
		"negative":         strings.Replace(valid, `"inputTokens":80`, `"inputTokens":-1`, 1),
		"arithmetic":       strings.Replace(valid, `"totalTokens":100`, `"totalTokens":101`, 1),
		"cache":            strings.Replace(valid, `"cachedInputTokens":20`, `"cachedInputTokens":81`, 1),
		"write null":       strings.Replace(valid, `"inputTokens":80`, `"inputTokens":80,"cacheWriteInputTokens":null`, 1),
		"reasoning":        strings.Replace(valid, `"reasoningOutputTokens":5`, `"reasoningOutputTokens":21`, 1),
		"numeric overflow": strings.Replace(valid, `"totalTokens":100`, `"totalTokens":9223372036854775808`, 1),
		"sum overflow":     strings.Replace(strings.Replace(valid, `"inputTokens":80`, `"inputTokens":9223372036854775807`, 1), `"outputTokens":20`, `"outputTokens":1`, 1),
		"other thread":     strings.Replace(valid, testThreadID, "01980000-0000-7000-8000-000000000099", 1),
		"other turn":       strings.Replace(valid, testTurnID, "01980000-0000-7000-8000-000000000099", 1),
		"unknown metadata": strings.Replace(valid, `"usageMetadata":null`, `"usageMetadata":{"PRIVATE_BODY_SENTINEL":0}`, 1),
		"metadata type":    strings.Replace(valid, `"usageMetadata":null`, `"usageMetadata":{"amount":0}`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			state := measuredUsageState(t)
			before := state.measuredResult()
			if err := state.notification("rawResponse/completed", raw(input)); err == nil || strings.Contains(err.Error(), "PRIVATE_BODY_SENTINEL") {
				t.Fatal("invalid response accepted or diagnostic reflected payload")
			}
			if !reflect.DeepEqual(state.measuredResult(), before) || len(state.responseReceipts) != 0 {
				t.Fatal("invalid response mutated measurements")
			}
		})
	}
	for _, stage := range []string{"restored", "terminal"} {
		state := measuredUsageState(t)
		if stage == "restored" {
			state.turnID = ""
		} else {
			state.terminals = 1
		}
		if err := state.notification("rawResponse/completed", raw(valid)); err == nil {
			t.Fatal("stale or unbound response was charged")
		}
	}
}

func TestResponseUsageMetadataIsRedactedButPinnedForDeduplication(t *testing.T) {
	state := measuredUsageState(t)
	observation := strings.Replace(string(responseUsageNotification("response-1", codex160UsageBreakdownFixture)), `"usageMetadata":null`, `"usageMetadata":{"amount":"PRIVATE_BODY_SENTINEL","metadata":{"private":"PRIVATE_BODY_SENTINEL"}}`, 1)
	if err := state.notification("rawResponse/completed", raw(observation)); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(state.measuredResult())
	receipts, _ := json.Marshal(state.responseReceipts)
	if bytes.Contains(result, []byte("PRIVATE_BODY_SENTINEL")) || bytes.Contains(receipts, []byte("PRIVATE_BODY_SENTINEL")) {
		t.Fatal("provider metadata escaped numeric projection")
	}
	if err := state.notification("rawResponse/completed", raw(strings.Replace(observation, "PRIVATE_BODY_SENTINEL", "CHANGED_PRIVATE_BODY_SENTINEL", 1))); err == nil {
		t.Fatal("same response ID changed metadata without rejection")
	}
}

func TestResponseUsageReceiptsHaveABoundedBudget(t *testing.T) {
	state := measuredUsageState(t)
	for index := range maximumResponseReceipts {
		state.responseReceipts[fmt.Sprint(index)] = [32]byte{}
	}
	if err := state.notification("rawResponse/completed", responseUsageNotification("response-new", codex160UsageBreakdownFixture)); err == nil {
		t.Fatal("response receipt budget was unbounded")
	}
	if state.measuredResult().Usage.TotalTokens != 0 {
		t.Fatal("rejected over-budget response changed measurements")
	}
}

func TestResponseUsageAggregatesEachNumericResponseExactlyOnce(t *testing.T) {
	state := measuredUsageState(t)
	usage := strings.TrimSuffix(codex160UsageBreakdownFixture, `}`) + `,"cacheWriteInputTokens":10}`
	for _, id := range []string{"response-1", "response-2"} {
		if err := state.notification("rawResponse/completed", responseUsageNotification(id, usage)); err != nil {
			t.Fatal(err)
		}
	}
	reordered := `{"usageMetadata":null,"usage":{"cacheWriteInputTokens":10,"reasoningOutputTokens":5,"outputTokens":20,"cachedInputTokens":20,"inputTokens":80,"totalTokens":100},"turnId":"` + testTurnID + `","threadId":"` + testThreadID + `","responseId":"response-2"}`
	if err := state.notification("rawResponse/completed", raw(reordered)); err != nil {
		t.Fatal("JSON field ordering broke response deduplication")
	}
	want := runtimecontract.TokenUsage{TotalTokens: 200, InputTokens: 160, CachedInputTokens: 40, CacheWriteInputTokens: 20, OutputTokens: 40, ReasoningOutputTokens: 10}
	if state.measuredResult().Usage != want {
		t.Fatal("response totals did not preserve cache/write/reasoning counters")
	}
}

func TestResponseUsageResponseIDIsBoundedOpaqueString(t *testing.T) {
	state := measuredUsageState(t)
	if err := state.notification("rawResponse/completed", responseUsageNotification("opaque/response#1", codex160UsageBreakdownFixture)); err != nil {
		t.Fatal("response ID was incorrectly treated as a tool ID")
	}
	for _, id := range []string{"", strings.Repeat("x", 257)} {
		if err := state.notification("rawResponse/completed", responseUsageNotification(id, codex160UsageBreakdownFixture)); err == nil {
			t.Fatal("unbounded or empty response ID was accepted")
		}
	}
}

func TestResponseUsageFailureReasonStaysClosedAtNotificationBoundary(t *testing.T) {
	state := measuredUsageState(t)
	invalid := strings.Replace(codex160UsageBreakdownFixture, `"totalTokens":100`, `"totalTokens":101`, 1)
	err := state.notification("rawResponse/completed", responseUsageNotification("response-invalid", invalid))
	if err == nil {
		t.Fatal("billable arithmetic was relaxed")
	}
	failure := notificationFailure("rawResponse/completed", fmt.Errorf("PRIVATE_BODY_SENTINEL: %w", err)).(*appServerCallFailure)
	if failure.notificationError != string(tokenUsageTotalArithmetic) {
		t.Fatal("numeric failure lost its closed reason")
	}
}

func TestUsageCompletenessIsClosedAndDefaultsToUnknown(t *testing.T) {
	var empty Result
	if json.Unmarshal([]byte(`{}`), &empty) != nil || empty.UsageCompleteness != UsageUnknown {
		t.Fatal("default result claimed measured completeness")
	}
	for _, value := range []UsageCompleteness{UsageUnknown, UsagePartial, UsageComplete} {
		encoded, err := json.Marshal(value)
		var decoded UsageCompleteness
		if err != nil || json.Unmarshal(encoded, &decoded) != nil || decoded != value {
			t.Fatal("closed completeness enum did not round-trip")
		}
	}
	for _, value := range []string{`""`, `"PRIVATE_BODY_SENTINEL"`, `1`, `null`, `{}`} {
		quality := UsagePartial
		if err := json.Unmarshal([]byte(value), &quality); err == nil || strings.Contains(err.Error(), "PRIVATE_BODY_SENTINEL") || quality != UsagePartial {
			t.Fatal("invalid completeness mutated state or escaped diagnostics")
		}
	}
	if _, err := json.Marshal(UsageCompleteness(255)); err == nil {
		t.Fatal("invalid completeness escaped serialization")
	}
	if _, err := validateBrokerTerminal(brokerResponse{Result: Result{UsageCompleteness: UsageCompleteness(255)}, Failure: providerBrokerFailureProvider}); err == nil {
		t.Fatal("broker accepted invalid completeness")
	}
}
