package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const codex160UsageBreakdownFixture = `{"totalTokens":100,"inputTokens":80,"cachedInputTokens":20,"outputTokens":20,"reasoningOutputTokens":5}`

func TestTokenUsageClosedFailureReasons(t *testing.T) {
	envelope := func(total, last string) string { return `{"total":` + total + `,"last":` + last + `}` }
	changed := func(field, value string) string {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(codex160UsageBreakdownFixture), &fields); err != nil {
			t.Fatal(err)
		}
		if value == "" {
			delete(fields, field)
		} else {
			fields[field] = json.RawMessage(value)
		}
		return string(marshalProtocolFixture(t, fields))
	}
	for _, test := range []struct {
		name, input string
		reason      tokenUsageFailureReason
	}{
		{"unknown field", envelope(changed("PRIVATE_SECRET_BODY_SENTINEL", `0`), codex160UsageBreakdownFixture), tokenUsageStructure},
		{"duplicate", envelope(strings.TrimSuffix(codex160UsageBreakdownFixture, `}`)+`,"totalTokens":100}`, codex160UsageBreakdownFixture), tokenUsageStructure},
		{"nonobject", `[]`, tokenUsageStructure},
		{"missing envelope", `{"last":` + codex160UsageBreakdownFixture + `}`, tokenUsageRequiredMissing},
		{"null envelope", envelope(`null`, codex160UsageBreakdownFixture), tokenUsageRequiredNull},
		{"missing required", envelope(changed("inputTokens", ""), codex160UsageBreakdownFixture), tokenUsageRequiredMissing},
		{"null required", envelope(changed("inputTokens", `null`), codex160UsageBreakdownFixture), tokenUsageRequiredNull},
		{"type required", envelope(changed("inputTokens", `"PRIVATE_SECRET_BODY_SENTINEL"`), codex160UsageBreakdownFixture), tokenUsageRequiredType},
		{"overflow required", envelope(changed("inputTokens", `9223372036854775808`), codex160UsageBreakdownFixture), tokenUsageRequiredType},
		{"null optional", envelope(changed("cacheWriteInputTokens", `null`), codex160UsageBreakdownFixture), tokenUsageOptionalNull},
		{"type optional", envelope(changed("cacheWriteInputTokens", `"PRIVATE_SECRET_BODY_SENTINEL"`), codex160UsageBreakdownFixture), tokenUsageOptionalType},
		{"context type", `{"total":` + codex160UsageBreakdownFixture + `,"last":` + codex160UsageBreakdownFixture + `,"modelContextWindow":true}`, tokenUsageOptionalType},
		{"negative", envelope(changed("inputTokens", `-1`), codex160UsageBreakdownFixture), tokenUsageNegative},
		{"arithmetic", envelope(changed("totalTokens", `101`), codex160UsageBreakdownFixture), tokenUsageTotalArithmetic},
		{"cache bound", envelope(changed("cachedInputTokens", `81`), codex160UsageBreakdownFixture), tokenUsageCacheInputBound},
		{"write bound", envelope(changed("cacheWriteInputTokens", `81`), codex160UsageBreakdownFixture), tokenUsageCacheInputBound},
		{"reasoning bound", envelope(changed("reasoningOutputTokens", `21`), codex160UsageBreakdownFixture), tokenUsageReasoningOutputBound},
		{"last bound", envelope(codex160UsageBreakdownFixture, changed("cachedInputTokens", `21`)), tokenUsageLastExceedsTotal},
		{"last total", envelope(codex160UsageBreakdownFixture, strings.Replace(strings.Replace(codex160UsageBreakdownFixture, `"totalTokens":100`, `"totalTokens":101`, 1), `"inputTokens":80`, `"inputTokens":81`, 1)), tokenUsageLastExceedsTotal},
		{"last input", envelope(codex160UsageBreakdownFixture, strings.Replace(strings.Replace(codex160UsageBreakdownFixture, `"inputTokens":80`, `"inputTokens":81`, 1), `"outputTokens":20`, `"outputTokens":19`, 1)), tokenUsageLastExceedsTotal},
		{"last output", envelope(codex160UsageBreakdownFixture, strings.Replace(strings.Replace(codex160UsageBreakdownFixture, `"inputTokens":80`, `"inputTokens":79`, 1), `"outputTokens":20`, `"outputTokens":21`, 1)), tokenUsageLastExceedsTotal},
		{"last cache write", envelope(codex160UsageBreakdownFixture, changed("cacheWriteInputTokens", `1`)), tokenUsageLastExceedsTotal},
		{"last reasoning", envelope(codex160UsageBreakdownFixture, changed("reasoningOutputTokens", `6`)), tokenUsageLastExceedsTotal},
		{"invalid last", envelope(codex160UsageBreakdownFixture, changed("outputTokens", `null`)), tokenUsageRequiredNull},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage, err := parseTokenUsage(raw(test.input))
			var failure *tokenUsageFailure
			if !errors.As(err, &failure) || failure.reason != test.reason || usage != (runtimecontract.TokenUsage{}) {
				t.Fatalf("unexpected closed failure: %v", err)
			}
			if err.Error() != "Codex app-server token usage is invalid" {
				t.Fatal("diagnostic reflected input")
			}
			notification := notificationFailure("thread/tokenUsage/updated", fmt.Errorf("PRIVATE_SECRET_BODY_SENTINEL: %w", err)).(*appServerCallFailure)
			if notification.notificationError != string(test.reason) {
				t.Fatalf("reason lost: %s", notification.notificationError)
			}
		})
	}
	if safeTokenUsageFailureReason(tokenUsageFailureReason("PRIVATE_SECRET_BODY_SENTINEL")) != "UNKNOWN" {
		t.Fatal("unknown diagnostic accepted")
	}
}

func TestCodex160UsageCacheWriteDefaultsOnlyWhenMissing(t *testing.T) {
	for _, test := range []struct {
		name, extra string
		cacheWrite  int64
	}{
		{"missing", "", 0},
		{"zero", `,"cacheWriteInputTokens":0`, 0},
		{"usual", `,"cacheWriteInputTokens":10`, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage, err := parseTokenUsageBreakdown(raw(codex160UsageBreakdownFixture[:len(codex160UsageBreakdownFixture)-1]+test.extra+`}`), 258400)
			want := runtimecontract.TokenUsage{TotalTokens: 100, InputTokens: 80, CachedInputTokens: 20,
				CacheWriteInputTokens: test.cacheWrite, OutputTokens: 20, ReasoningOutputTokens: 5, ModelContextWindow: 258400}
			if err != nil || usage != want {
				t.Fatalf("optional cache-write accounting mismatch: usage=%#v err=%v", usage, err)
			}
		})
	}
}

func TestCodex160UsageCacheWriteInvalidPresentValuesFailClosed(t *testing.T) {
	for _, value := range []string{`null`, `-1`, `1.5`, `9223372036854775808`, `"0"`, `true`, `[]`, `{}`, `81`} {
		t.Run(value, func(t *testing.T) {
			input := codex160UsageBreakdownFixture[:len(codex160UsageBreakdownFixture)-1] + `,"cacheWriteInputTokens":` + value + `}`
			if _, err := parseTokenUsageBreakdown(raw(input), 258400); err == nil {
				t.Fatal("invalid present cache-write accounting was accepted")
			}
		})
	}
	for _, extra := range []string{`,"cacheWriteInputTokens":0,"cacheWriteInputTokens":0`, `,"cacheWriteInputTokens":0,"future":0`} {
		input := codex160UsageBreakdownFixture[:len(codex160UsageBreakdownFixture)-1] + extra + `}`
		if _, err := parseTokenUsageBreakdown(raw(input), 258400); err == nil {
			t.Fatal("duplicate or unknown accounting field was accepted")
		}
	}
}

func TestCodex160UsageOtherRequiredFieldsAndInvariantsRemainClosed(t *testing.T) {
	for _, field := range []string{"totalTokens", "inputTokens", "cachedInputTokens", "outputTokens", "reasoningOutputTokens"} {
		t.Run(field, func(t *testing.T) {
			var fields map[string]any
			if err := json.Unmarshal([]byte(codex160UsageBreakdownFixture), &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, field)
			if _, err := parseTokenUsageBreakdown(marshalProtocolFixture(t, fields), 258400); err == nil {
				t.Fatal("missing required accounting field was accepted")
			}
		})
	}
	for _, input := range []string{
		strings.Replace(codex160UsageBreakdownFixture, `"totalTokens":100`, `"totalTokens":101`, 1),
		strings.Replace(codex160UsageBreakdownFixture, `"cachedInputTokens":20`, `"cachedInputTokens":81`, 1),
		strings.Replace(codex160UsageBreakdownFixture, `"reasoningOutputTokens":5`, `"reasoningOutputTokens":21`, 1),
		strings.Replace(codex160UsageBreakdownFixture, `"inputTokens":80`, `"inputTokens":-1`, 1),
	} {
		if _, err := parseTokenUsageBreakdown(raw(input), 258400); err == nil {
			t.Fatal("inconsistent accounting was accepted without optional cache-write field")
		}
	}
}

func TestCodex160UsageEveryPresentCounterRejectsNull(t *testing.T) {
	const zeroCounters = `{"totalTokens":0,"inputTokens":0,"cachedInputTokens":0,"cacheWriteInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0}`
	for _, field := range []string{"totalTokens", "inputTokens", "cachedInputTokens", "cacheWriteInputTokens", "outputTokens", "reasoningOutputTokens"} {
		t.Run(field, func(t *testing.T) {
			input := strings.Replace(zeroCounters, `"`+field+`":0`, `"`+field+`":null`, 1)
			if _, err := parseTokenUsageBreakdown(raw(input), 258400); err == nil {
				t.Fatal("present null counter was silently converted to zero")
			}
		})
	}
}

func TestCodex160UsageOptionalCacheWriteInTotalAndLast(t *testing.T) {
	withWrite := strings.TrimSuffix(codex160UsageBreakdownFixture, `}`) + `,"cacheWriteInputTokens":10}`
	withZero := strings.TrimSuffix(codex160UsageBreakdownFixture, `}`) + `,"cacheWriteInputTokens":0}`
	for _, test := range []struct {
		name, total, last string
		cacheWrite        int64
	}{
		{"both_missing", codex160UsageBreakdownFixture, codex160UsageBreakdownFixture, 0},
		{"last_missing", withWrite, codex160UsageBreakdownFixture, 10},
		{"total_missing", codex160UsageBreakdownFixture, withZero, 0},
		{"both_present", withWrite, withWrite, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage, err := parseTokenUsage(raw(`{"total":` + test.total + `,"last":` + test.last + `,"modelContextWindow":258400}`))
			if err != nil || usage.CacheWriteInputTokens != test.cacheWrite || usage.InputTokens != 80 || usage.CachedInputTokens != 20 || usage.OutputTokens != 20 {
				t.Fatalf("optional total/last mismatch: usage=%#v err=%v", usage, err)
			}
		})
	}
	if _, err := parseTokenUsage(raw(`{"total":` + codex160UsageBreakdownFixture + `,"last":` + withWrite + `}`)); err == nil {
		t.Fatal("last cache-write usage exceeded total default zero")
	}
}

func TestCodex160CompactionAndOptionalUsageReachOrdinaryTerminal(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.result.SessionID = testThreadID, testThreadID
	state.threadPath = "/workspace/.kodex/state/codex-home/sessions/2026/10/08/rollout-fixture.jsonl"
	if err := state.captureUsageBaseline(); err != nil {
		t.Fatal(err)
	}
	var activities []runtimecontract.RuntimeActivity
	state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
		activities = append(activities, activity)
		return nil
	}
	compaction := `{"id":"compact-1","type":"contextCompaction"}`
	message := `{"id":"message-1","type":"agentMessage","text":"готово","phase":"final_answer"}`
	notifications := []struct{ method, payload string }{
		{"turn/started", `{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[],"status":"inProgress"}}`},
		{"item/started", `{"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","startedAtMs":100,"item":` + compaction + `}`},
		{"item/completed", `{"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","completedAtMs":140,"item":` + compaction + `}`},
		{"thread/tokenUsage/updated", `{"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","tokenUsage":{"total":` + codex160UsageBreakdownFixture + `,"last":` + codex160UsageBreakdownFixture + `,"modelContextWindow":258400}}`},
		{"item/completed", `{"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `","completedAtMs":150,"item":` + message + `}`},
		{"turn/completed", `{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[` + compaction + `,` + message + `],"status":"completed"}}`},
	}
	events := make(chan streamEvent, len(notifications))
	for _, notification := range notifications {
		wire, err := parseWireMessage(raw(`{"method":"` + notification.method + `","params":` + notification.payload + `}`))
		if err != nil {
			t.Fatal(err)
		}
		events <- streamEvent{message: wire}
	}
	close(events)
	if err := (&appServer{messages: events}).waitTerminal(t.Context(), state); err != nil {
		t.Fatalf("compaction lifecycle or optional usage prevented terminal: %v", err)
	}
	result, err := state.terminalResult()
	if err != nil || result.Outcome != "SUCCEEDED" || result.FinalMessage != "готово" || result.Usage.CacheWriteInputTokens != 0 ||
		result.Usage.TotalTokens != 100 || result.Usage.InputTokens != 80 || result.Usage.CachedInputTokens != 20 || result.Usage.OutputTokens != 20 {
		t.Fatalf("ordinary terminal accounting changed: result=%#v err=%v", result, err)
	}
	if len(state.startedToolCalls) != 0 || len(state.toolCalls) != 0 || len(state.itemStartedAtMS) != 0 || len(result.ToolCalls) != 0 ||
		len(activities) != 1 || activities[0].Message == nil || activities[0].ToolCall != nil {
		t.Fatal("compaction invented an unfinished tool or duplicate activity")
	}
}

func TestCodex160CompactionRejectsWrongTupleAndUnknownItemFields(t *testing.T) {
	for _, method := range []string{"item/started", "item/completed"} {
		for _, test := range []struct{ name, thread, turn, item string }{
			{"thread", "other-thread", testTurnID, `{"id":"compact-1","type":"contextCompaction"}`},
			{"turn", testThreadID, "01980000-0000-7000-8000-000000000099", `{"id":"compact-1","type":"contextCompaction"}`},
			{"unknown_field", testThreadID, testTurnID, `{"id":"compact-1","type":"contextCompaction","future":true}`},
			{"other_item_field", testThreadID, testTurnID, `{"id":"compact-1","type":"contextCompaction","text":"private"}`},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				state := newProtocolState(testThreadID)
				state.threadID, state.turnID = testThreadID, testTurnID
				timestamp := "startedAtMs"
				if method == "item/completed" {
					timestamp = "completedAtMs"
				}
				payload := raw(`{"threadId":"` + test.thread + `","turnId":"` + test.turn + `","` + timestamp + `":100,"item":` + test.item + `}`)
				if err := state.notification(method, payload); err == nil {
					t.Fatal("invalid compaction item or tuple was accepted")
				}
				if state.terminals != 0 || len(state.startedToolCalls) != 0 || len(state.toolCalls) != 0 {
					t.Fatal("rejected compaction changed execution state")
				}
			})
		}
	}
}
