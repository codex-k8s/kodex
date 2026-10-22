package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestTerminalConsumedByFinancialReadDoesNotWaitForAnotherNotification feeds turn completion during
// a financial RPC and requires waitTerminal to finish without waiting for a redundant notification.
// TestTerminalConsumedByFinancialReadDoesNotWaitForAnotherNotification подаёт завершение хода во время
// финансового RPC и требует выхода waitTerminal без ожидания повторного уведомления.
func TestTerminalConsumedByFinancialReadDoesNotWaitForAnotherNotification(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID = testThreadID, testTurnID
	state.result.SessionID = testThreadID
	state.financialEnabled, state.financialWake = true, true
	state.financialExpectedAccount = "fixture-private-account"
	state.financialEpoch, state.financialBoundEpoch = 1, 1
	state.onActivity = func(runtimecontract.RuntimeActivity) error { return nil }
	if err := state.notification("turn/started", raw(`{"threadId":"`+testThreadID+`","turn":{"id":"`+testTurnID+`","items":[],"status":"inProgress"}}`)); err != nil {
		t.Fatal(err)
	}
	messages := make(chan streamEvent, 2)
	item := `{"id":"financial-terminal-final","text":"готово","phase":"final_answer","questions":null,"type":"agentMessage"}`
	messages <- streamEvent{message: wireMessage{kind: messageNotification, method: "turn/completed", payload: raw(`{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[` + item + `],"status":"completed"}}`)}}
	messages <- streamEvent{message: wireMessage{kind: messageResponse, id: raw(`1`), payload: raw(`{"accountId":"fixture-private-account","rateLimits":null}`)}}
	server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() {}}, messages: messages}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if err := server.waitTerminal(ctx, state); err != nil || state.terminals != 1 {
		t.Fatalf("terminal consumed by financial RPC did not finish execution: %v", err)
	}
}

// TestRateLimitNotificationOnlyWakesAuthoritativeRead verifies even invented credit notification data
// only requests a read and cannot publish a financial measurement by itself.
// TestRateLimitNotificationOnlyWakesAuthoritativeRead проверяет, что даже выдуманные данные о кредитах
// в уведомлении лишь пробуждают чтение и сами не публикуют финансовое измерение.
func TestRateLimitNotificationOnlyWakesAuthoritativeRead(t *testing.T) {
	state := newProtocolState("")
	calls := 0
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return nil }
	for _, raw := range []json.RawMessage{json.RawMessage(`{"rateLimits":null}`), json.RawMessage(`{"rateLimits":{"credits":{"balance":"invented"}}}`)} {
		if err := state.notification("account/rateLimits/updated", raw); err != nil {
			t.Fatal(err)
		}
	}
	if !state.financialWake || calls != 0 {
		t.Fatal("notification became an account measurement")
	}
}

// TestBrokerFinancialSequenceRejectsReplayAndMixedActivity counts callbacks before the intentionally
// missing terminal to distinguish accepted financial sequence prefixes from replay/gap rejection;
// a financial observation mixed with a model message must fail the activity shape check.
// TestBrokerFinancialSequenceRejectsReplayAndMixedActivity считает callback до намеренно отсутствующего
// итога, отличая допустимые префиксы последовательности от отказа повтору или пропуску;
// смешанное финансовое событие и сообщение модели должны отклоняться проверкой структуры.
func TestBrokerFinancialSequenceRejectsReplayAndMixedActivity(t *testing.T) {
	for _, sequences := range [][]int64{{1, 2}, {1, 1}, {2}} {
		var stream bytes.Buffer
		for index, sequence := range sequences {
			frame := brokerFrame{Version: providerBrokerVersion, Sequence: uint64(index + 1), Kind: brokerFrameActivity, Activity: &runtimecontract.RuntimeActivity{Financial: &runtimecontract.ProviderFinancialObservation{Sequence: sequence, State: "UNAVAILABLE"}}}
			if err := json.NewEncoder(&stream).Encode(frame); err != nil {
				t.Fatal(err)
			}
		}
		// Отсутствующий terminal остаётся invalid независимо от корректности prefix;
		// до EOF callback показывает, был ли replay отвергнут до доставки.
		calls := 0
		_, err := readProviderBrokerResponse(&stream, func(runtimecontract.RuntimeActivity) error { calls++; return nil })
		if err == nil {
			t.Fatal("unterminated broker accepted")
		}
		want := 2
		if len(sequences) == 1 {
			want = 0
		} else if sequences[1] == 1 {
			want = 1
		}
		if calls != want {
			t.Fatalf("sequences %v delivered %d observations, want %d", sequences, calls, want)
		}
	}
	activity := runtimecontract.RuntimeActivity{Financial: &runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE"}, Message: &runtimecontract.RuntimeAgentMessage{ItemID: "fixture", Phase: "FINAL", Revision: 1, Text: "safe"}}
	if activity.Validate() == nil {
		t.Fatal("mixed financial/model activity accepted")
	}
}

// TestFinancialAccountBindingAndCurrentProcessEpoch distinguishes exact, null, foreign and epoch-raced
// account identities and verifies published observations never disclose the private account identifier.
// TestFinancialAccountBindingAndCurrentProcessEpoch различает точную, null-, чужую и устаревшую из-за epoch
// идентичности и проверяет отсутствие приватного идентификатора в опубликованном наблюдении.
func TestFinancialAccountBindingAndCurrentProcessEpoch(t *testing.T) {
	for _, row := range []struct {
		id        string
		changed   bool
		available bool
		rejected  bool
	}{
		{`"fixture-private-account"`, false, true, false},
		{`null`, false, false, false},
		{`"other-private-account"`, false, false, true},
		{`"fixture-private-account"`, true, false, false},
	} {
		state := newProtocolState("")
		state.financialEnabled = true
		state.financialExpectedAccount = "fixture-private-account"
		state.financialEpoch = 1
		state.financialBoundEpoch = 1
		var delivered *runtimecontract.ProviderFinancialObservation
		state.onActivity = func(activity runtimecontract.RuntimeActivity) error { delivered = activity.Financial; return nil }
		messages := make(chan streamEvent, 2)
		if row.changed {
			messages <- streamEvent{message: wireMessage{kind: messageNotification, method: "account/updated", payload: json.RawMessage(`{"authMode":null,"planType":null}`)}}
		}
		messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"accountId":` + row.id + `,"rateLimits":{"primary":{"usedPercent":100},"credits":{"hasCredits":true,"unlimited":false,"balance":"59999.9"}}}`)}}
		server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() {}}, messages: messages}
		err := server.observeFinancial(t.Context(), state)
		if (err != nil) != row.rejected {
			t.Fatal("identity mismatch outcome is invalid")
		}
		if row.rejected {
			if delivered != nil {
				t.Fatal("foreign measurement was published")
			}
			continue
		}
		if delivered == nil || (delivered.State == "AVAILABLE") != row.available {
			t.Fatal("nullable identity or epoch guard failed")
		}
		safe, err := json.Marshal(delivered)
		if err != nil || bytes.Contains(safe, []byte("private-account")) || bytes.Contains(safe, []byte("accountId")) {
			t.Fatal("private identity escaped the observation")
		}
	}
}

// TestFinancialMethodAbsenceDoesNotSuppressOtherFailures accepts the correlated method-not-found code
// as unavailable data, while unrelated internal/provider errors cannot be silently downgraded.
// TestFinancialMethodAbsenceDoesNotSuppressOtherFailures принимает коррелированный код отсутствия метода
// как недоступность данных, но не позволяет молча скрыть другие ошибки провайдера.
func TestFinancialMethodAbsenceDoesNotSuppressOtherFailures(t *testing.T) {
	for _, code := range []int64{-32601, -32603, -32000} {
		state := newProtocolState("")
		state.financialEnabled = true
		var delivered *runtimecontract.ProviderFinancialObservation
		state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
			delivered = activity.Financial
			return nil
		}
		payload, err := json.Marshal(map[string]any{"code": code, "message": "synthetic upstream error"})
		if err != nil {
			t.Fatal(err)
		}
		messages := make(chan streamEvent, 1)
		messages <- streamEvent{message: wireMessage{kind: messageError, id: json.RawMessage(`1`), payload: payload}}
		server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() {}}, messages: messages}
		err = server.observeFinancial(t.Context(), state)
		if code == -32601 {
			if err != nil || delivered == nil || delivered.State != "UNAVAILABLE" || delivered.Validate() != nil {
				t.Fatal("supported method absence blocked the execution")
			}
		} else if err == nil || delivered != nil {
			t.Fatal("unknown provider failure was suppressed")
		}
	}
}

// TestFinancialCollectorRequiresOrdinaryManagedIdentity permits only ordinary chatgpt credential identity,
// not externally supplied tokens, API keys, missing auth mode or a null account identifier.
// TestFinancialCollectorRequiresOrdinaryManagedIdentity допускает идентичность обычных chatgpt credentials,
// но не внешние токены, API key, отсутствие режима авторизации или null-идентификатор.
func TestFinancialCollectorRequiresOrdinaryManagedIdentity(t *testing.T) {
	for _, row := range []struct {
		raw  string
		want string
	}{
		{`{"auth_mode":"chatgpt","tokens":{"account_id":"synthetic-private-account"}}`, "synthetic-private-account"},
		{`{"auth_mode":"chatgptAuthTokens","tokens":{"account_id":"synthetic-private-account"}}`, ""},
		{`{"auth_mode":"apikey"}`, ""},
		{`{"tokens":{"account_id":"synthetic-private-account"}}`, ""},
		{`{"auth_mode":"chatgpt","tokens":{"account_id":null}}`, ""},
	} {
		got, err := managedFinancialIdentity([]byte(row.raw))
		if err != nil || got != row.want {
			t.Fatal("financial identity crossed the managed auth boundary")
		}
	}
}

// TestFinancialBudgetPreservesFinalRead verifies the ordinary budget cannot consume slot64 and the
// final read uses it once, with later wake requests cleared without extra RPCs or observations.
// TestFinancialBudgetPreservesFinalRead проверяет сохранение слота64 обычным сбором и его однократное
// использование итоговым чтением; следующие пробуждения снимаются без лишних RPC и наблюдений.
func TestFinancialBudgetPreservesFinalRead(t *testing.T) {
	state := newProtocolState("")
	state.financialEnabled = true
	state.financialSequence = 63
	state.financialWake = true
	delivered := 0
	state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
		if activity.Financial == nil || activity.Financial.Sequence != 64 {
			t.Fatal("final observation sequence is invalid")
		}
		delivered++
		return nil
	}
	writes := 0
	messages := make(chan streamEvent, 1)
	messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"accountId":null,"rateLimits":{}}`)}}
	server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() { writes++ }}, messages: messages}
	if err := server.observeFinancial(t.Context(), state); err != nil || writes != 0 || delivered != 0 || state.financialWake {
		t.Fatal("wake consumed the reserved final read")
	}
	if err := server.observeFinancialFinal(t.Context(), state); err != nil || writes != 1 || delivered != 1 {
		t.Fatal("final read was not delivered before process stop")
	}
	state.financialWake = true
	if err := server.observeFinancialFinal(t.Context(), state); err != nil || writes != 1 || delivered != 1 || state.financialWake {
		t.Fatal("collector exceeded the bounded observation budget")
	}
}

// TestFinancialAuthEpochRequiresFreshAccountRead requires account/read before rateLimits/read after
// an auth change and rejects availability when another account update races with that fresh read.
// TestFinancialAuthEpochRequiresFreshAccountRead требует account/read перед rateLimits/read после смены
// авторизации и запрещает доступный снимок при новом account update во время свежего чтения.
func TestFinancialAuthEpochRequiresFreshAccountRead(t *testing.T) {
	for _, changedDuringRead := range []bool{false, true} {
		state := newProtocolState("")
		state.financialEnabled = true
		state.financialExpectedAccount = "synthetic-private-account"
		state.financialEpoch, state.financialBoundEpoch = 2, 1
		var observation *runtimecontract.ProviderFinancialObservation
		state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
			observation = activity.Financial
			return nil
		}
		messages := make(chan streamEvent, 3)
		if changedDuringRead {
			messages <- streamEvent{message: wireMessage{kind: messageNotification, method: "account/updated", payload: json.RawMessage(`{"authMode":null,"planType":null}`)}}
		}
		messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"requiresOpenaiAuth":true,"account":{"type":"chatgpt","email":null,"planType":"pro"}}`)}}
		messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`2`), payload: json.RawMessage(`{"accountId":"synthetic-private-account","rateLimits":{"primary":{"usedPercent":100},"credits":{"balance":"59999.1234567890123456789","hasCredits":true}}}`)}}
		writes := 0
		server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() { writes++ }}, messages: messages}
		if err := server.observeFinancial(t.Context(), state); err != nil || writes != 2 || observation == nil {
			t.Fatal("fresh account binding was not read")
		}
		if (observation.State == "AVAILABLE") == changedDuringRead {
			t.Fatal("auth epoch race produced a stale measurement")
		}
	}
}

// TestFinancialAuthChangeStopsReadsOutsideManagedAccount checks null/API-key accounts disable further
// collection, publish UNAVAILABLE and cannot be reopened by the final-read entry point.
// TestFinancialAuthChangeStopsReadsOutsideManagedAccount проверяет отключение сбора для null/API-key
// аккаунта, публикацию UNAVAILABLE и невозможность возобновить чтение итоговым вызовом.
func TestFinancialAuthChangeStopsReadsOutsideManagedAccount(t *testing.T) {
	for _, account := range []string{`null`, `{"type":"apiKey"}`} {
		state := newProtocolState("")
		state.financialEnabled = true
		state.financialEpoch, state.financialBoundEpoch = 2, 1
		var observation *runtimecontract.ProviderFinancialObservation
		state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
			observation = activity.Financial
			return nil
		}
		messages := make(chan streamEvent, 1)
		messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"requiresOpenaiAuth":false,"account":` + account + `}`)}}
		writes := 0
		server := &appServer{stdin: &proofRequestWriter{beforeWrite: func() { writes++ }}, messages: messages}
		if err := server.observeFinancial(t.Context(), state); err != nil || state.financialEnabled || writes != 1 || observation == nil || observation.State != "UNAVAILABLE" {
			t.Fatal("finance reader continued outside the managed account")
		}
		if err := server.observeFinancialFinal(t.Context(), state); err != nil || writes != 1 {
			t.Fatal("final finance read reopened a disabled account path")
		}
	}
}
