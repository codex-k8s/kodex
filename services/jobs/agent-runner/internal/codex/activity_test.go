package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestProtocolStreamsPublishedActivitiesImmediatelyAndExactlyOnce verifies immediate completed-message
// and two-revision tool projection, snapshot replay suppression and exclusion of raw commands/reasoning.
// Exactly-once here concerns the local projection, not a distributed delivery guarantee.
// TestProtocolStreamsPublishedActivitiesImmediatelyAndExactlyOnce проверяет немедленную публикацию готовых
// сообщений и двух ревизий инструмента, подавление повторного снимка и сокрытие команд/рассуждений.
// Однократность относится к локальной проекции, а не распределённой гарантии доставки.
func TestProtocolStreamsPublishedActivitiesImmediatelyAndExactlyOnce(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.workspaceRoot = testThreadID, testTurnID, "/workspace"
	var activities []runtimecontract.RuntimeActivity
	state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
		activities = append(activities, activity)
		return nil
	}
	commentary := raw(`{"id":"commentary-one","text":"Проверяю состояние","phase":"commentary","type":"agentMessage"}`)
	if err := state.consumeItem(commentary, false, 10); err != nil || len(activities) != 0 {
		t.Fatal("started message was published")
	}
	if err := state.consumeItem(commentary, true, 20); err != nil || len(activities) != 1 || activities[0].Message.Phase != runtimecontract.RuntimeMessageCommentary || state.terminals != 0 {
		t.Fatal("commentary did not stream before turn completion")
	}
	started := raw(`{"command":"SECRET_COMMAND","commandActions":[],"cwd":"/workspace","exitCode":null,"id":"native-one","status":"inProgress","type":"commandExecution"}`)
	if err := state.consumeItem(started, false, 30); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 2 || activities[1].ToolCall.State != runtimecontract.NativeToolStateRunning || activities[1].ToolCall.Revision != 1 || activities[1].ToolCall.DurationMS != 0 || activities[1].ToolCall.SafeResult != "" {
		t.Fatal("native start did not stream")
	}
	completed := raw(`{"command":"SECRET_COMMAND","commandActions":[],"cwd":"/workspace","exitCode":0,"id":"native-one","status":"completed","type":"commandExecution"}`)
	if err := state.consumeItem(completed, true, 80); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 3 || activities[2].ToolCall.Revision != 2 || activities[2].ToolCall.State != runtimecontract.NativeToolStateSucceeded || activities[2].ToolCall.DurationMS != 50 {
		t.Fatal("native completion did not stream")
	}
	final := raw(`{"id":"final-one","text":"Готово","phase":"final_answer","type":"agentMessage"}`)
	if err := state.consumeItem(final, true, 90); err != nil || len(activities) != 4 || activities[3].Message.Phase != runtimecontract.RuntimeMessageFinal {
		t.Fatal("final did not stream")
	}
	for _, item := range []json.RawMessage{commentary, completed, final} {
		if err := state.consumeItem(item, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(activities) != 4 {
		t.Fatal("authoritative snapshot duplicated activity")
	}
	encoded, _ := json.Marshal(activities)
	if bytes.Contains(encoded, []byte("SECRET_COMMAND")) {
		t.Fatal("native raw command escaped projection")
	}
	if err := state.consumeItem(raw(`{"id":"reasoning-one","summary":["SECRET_REASONING"],"content":[],"type":"reasoning"}`), true, 0); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 4 {
		t.Fatal("reasoning was published")
	}
}

// TestProtocolKeepsMultipleFinalItemsUntilTurnCompletion keeps distinct final-phase messages in history
// without treating them as turn completion; only the authoritative terminal selects or discards the final text.
// TestProtocolKeepsMultipleFinalItemsUntilTurnCompletion сохраняет разные сообщения финальной фазы в истории
// без завершения хода; только авторитетный итог выбирает либо отбрасывает окончательный текст.
func TestProtocolKeepsMultipleFinalItemsUntilTurnCompletion(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			state := newProtocolState(testThreadID)
			state.threadID, state.turnID, state.workspaceRoot = testThreadID, testTurnID, "/workspace/work"
			state.turnStarted = 1
			var activities []runtimecontract.RuntimeActivity
			state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
				activities = append(activities, activity)
				return nil
			}
			question := raw(`{"id":"final-question","text":"Какой профиль используется?","phase":"final_answer","type":"agentMessage"}`)
			commentary := raw(`{"id":"commentary-after-question","text":"Сохраняю проверенный результат","phase":"commentary","type":"agentMessage"}`)
			answer := raw(`{"id":"final-result","text":"Итоговый результат","phase":"final_answer","type":"agentMessage"}`)
			for _, item := range []json.RawMessage{question, commentary, answer, question, answer} {
				if err := state.consumeItem(item, true, 10); err != nil {
					t.Fatalf("valid distinct message or exact replay rejected: %v", err)
				}
			}
			if len(activities) != 3 || state.terminals != 0 || state.result.Outcome != "" || state.result.FinalMessage != "" || state.finalID != "final-result" {
				t.Fatal("message phase completed the turn, replay replaced the result, or history was lost")
			}
			if activities[0].Message.Phase != runtimecontract.RuntimeMessageFinal || activities[1].Message.Phase != runtimecontract.RuntimeMessageCommentary || activities[2].Message.Phase != runtimecontract.RuntimeMessageFinal {
				t.Fatal("native message phases changed")
			}
			mutated := raw(`{"id":"final-question","text":"Изменённый текст","phase":"final_answer","type":"agentMessage"}`)
			if err := state.consumeItem(mutated, true, 20); err == nil || len(activities) != 3 || state.finalID != "final-result" {
				t.Fatal("changed completed item accepted")
			}
			errorJSON := "null"
			if status == "failed" {
				errorJSON = `{"message":"diagnostic","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":null}}}`
			}
			terminal := raw(`{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[` + string(question) + `,` + string(commentary) + `,` + string(answer) + `],"status":"` + status + `","error":` + errorJSON + `}}`)
			if err := state.notification("turn/completed", terminal); err != nil || len(activities) != 3 || state.terminals != 1 {
				t.Fatalf("authoritative turn completion rejected or replayed: %v", err)
			}
			if status == "completed" && (state.result.Outcome != "SUCCEEDED" || state.result.FinalMessage != "Итоговый результат") {
				t.Fatal("last final answer was not selected")
			}
			if status == "failed" && (state.result.Outcome != "FAILED" || state.result.FinalMessage != "" || state.result.FailureCode != "provider_transport_failure") {
				t.Fatal("message converted failed provider turn to success")
			}
			if status == "interrupted" && (state.result.Outcome != "FAILED" || state.result.FinalMessage != "" || state.result.FailureCode != "provider_interrupted") {
				t.Fatal("message converted interrupted provider turn to success")
			}
		})
	}
}

// TestProtocolActivityBoundaryAndStickyDeliveryFailure checks callback-error redaction and sticky failure,
// oversize rejection and omission of messages with an absent phase rather than guessing one.
// TestProtocolActivityBoundaryAndStickyDeliveryFailure проверяет сокрытие и сохранение отказа callback,
// отказ слишком большому сообщению и пропуск отсутствующей фазы без её угадывания.
func TestProtocolActivityBoundaryAndStickyDeliveryFailure(t *testing.T) {
	state := newProtocolState(testThreadID)
	calls := 0
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return errors.New("PRIVATE_CALLBACK_DIAGNOSTIC") }
	item := raw(`{"id":"message-one","text":"safe","phase":"commentary","type":"agentMessage"}`)
	if err := state.consumeItem(item, true, 0); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("callback failure was not safely rejected")
	}
	if err := state.publishActivity(runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "message-two", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}); err == nil || calls != 1 {
		t.Fatal("delivery continued after failure")
	}
	state = newProtocolState(testThreadID)
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return nil }
	large, _ := json.Marshal(map[string]any{"id": "large", "text": strings.Repeat("x", runtimecontract.MaximumRuntimeMessageBytes+1), "phase": "commentary", "type": "agentMessage"})
	if err := state.consumeItem(large, true, 0); err == nil {
		t.Fatal("oversized public message accepted")
	}
	if err := state.consumeItem(raw(`{"id":"missing-phase","text":"safe","type":"agentMessage"}`), true, 0); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unknown phase was guessed")
	}
}

// TestBrokerStreamDeliversActivityBeforeExactTerminal uses a gated pipe to prove activity arrives before
// completion and cannot invent it; the later failure must preserve the measured token usage.
// TestBrokerStreamDeliversActivityBeforeExactTerminal через управляемый pipe проверяет доставку события
// до завершения без выдуманного итога; последующий отказ должен сохранить измеренный расход токенов.
func TestBrokerStreamDeliversActivityBeforeExactTerminal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	observed := make(chan struct{}, 1)
	finish := make(chan struct{})
	written := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		frames := &brokerFrameWriter{writer: writer}
		err := frames.activity(runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "stream-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}})
		if err == nil {
			<-finish
			err = frames.finish(brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: usage}})
		}
		_ = writer.CloseWithError(err)
		written <- err
	}()
	// outcome transfers the reader's result and error together so the test can distinguish early activity
	// from premature completion without consulting shared mutable state.
	// outcome передаёт результат читателя вместе с ошибкой, чтобы отличать раннее событие
	// от преждевременного завершения без чтения общей изменяемой переменной.
	type outcome struct {
		result Result
		err    error
	}
	read := make(chan outcome, 1)
	go func() {
		result, err := readProviderBrokerResponse(reader, func(runtimecontract.RuntimeActivity) error { observed <- struct{}{}; return nil })
		read <- outcome{result, err}
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("activity buffered until terminal")
	}
	select {
	case <-read:
		t.Fatal("activity invented terminal")
	default:
	}
	close(finish)
	select {
	case got := <-read:
		if got.err == nil || got.result.Usage != usage {
			t.Fatal("terminal lost measured failure usage")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal did not complete")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

// TestBrokerStreamRejectsLegacyMalformedAndReorderedFrames rejects malformed version-1 fixtures and
// unframed input. Because their version already differs from the current protocol, this table alone
// does not independently prove each named replay, ordering or terminal-validation branch.
// TestBrokerStreamRejectsLegacyMalformedAndReorderedFrames отклоняет повреждённые конверты версии1
// и ввод без кадров. Их версия уже отличается от текущего протокола, поэтому одна эта таблица
// не доказывает независимо каждую указанную ветвь повторов, порядка или проверки итога.
func TestBrokerStreamRejectsLegacyMalformedAndReorderedFrames(t *testing.T) {
	message := `{"message":{"item_id":"message-one","phase":"COMMENTARY","revision":1,"text":"safe"}}`
	activity := `{"version":1,"sequence":1,"kind":"ACTIVITY","activity":` + message + `}\n`
	terminal := `{"version":1,"sequence":2,"kind":"TERMINAL","terminal":{"result":{},"failure":"PROVIDER","ok":false}}` + "\n"
	for name, payload := range map[string]string{
		"legacy":            `{"result":{},"failure":"PROVIDER","ok":false}`,
		"wrong-version":     strings.ReplaceAll(activity, `"version":1`, `"version":2`),
		"wrong-sequence":    strings.ReplaceAll(activity, `"sequence":1`, `"sequence":2`),
		"unknown-field":     strings.ReplaceAll(activity, `"version":1`, `"unknown":true,"version":1`),
		"duplicate-field":   strings.ReplaceAll(activity, `"revision":1`, `"revision":1,"revision":1`),
		"missing-terminal":  activity,
		"double-terminal":   activity + terminal + terminal,
		"after-terminal":    activity + terminal + activity,
		"duplicate-message": activity + strings.ReplaceAll(activity, `"sequence":1`, `"sequence":2`) + strings.ReplaceAll(terminal, `"sequence":2`, `"sequence":3`),
		"oversized":         strings.Repeat("x", maximumBrokerFrameBytes+1),
		"terminal-batch":    strings.ReplaceAll(terminal, `"result":{}`, `"result":{"ToolCalls":[]}`),
		"terminal-null":     strings.ReplaceAll(terminal, `"result":{}`, `"result":{"ToolCalls":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			payload = strings.ReplaceAll(payload, `\n`, "\n")
			result, err := readProviderBrokerResponse(strings.NewReader(payload))
			if err == nil || result.Outcome != "" {
				t.Fatal("malformed broker stream accepted")
			}
		})
	}
}

// TestExecuteViaBrokerRequiresLiveActivityConsumer proves a missing callback is rejected before any
// broker connection can be attempted; it does not start an isolated provider.
// TestExecuteViaBrokerRequiresLiveActivityConsumer проверяет отказ отсутствующему callback до попытки
// соединения с broker; изолированный провайдер здесь не запускается.
func TestExecuteViaBrokerRequiresLiveActivityConsumer(t *testing.T) {
	if _, err := ExecuteViaBroker(context.Background(), model.Input{}, nil, "", "", nil); err == nil {
		t.Fatal("missing activity consumer accepted")
	}
}

// brokerUDSPair opens an independent Unix socket pair beneath t.TempDir and registers each resource
// for reverse-order cleanup, including partially constructed fixtures. It supplies transport only;
// peer cancellation, deadlines and lifecycle assertions remain the responsibility of each test.
// brokerUDSPair открывает независимую Unix-пару внутри t.TempDir и регистрирует обратную очистку
// каждого ресурса, в том числе при неполной подготовке. Helper предоставляет только транспорт;
// отмена peer, сроки и проверки жизненного цикла остаются в конкретном тесте.
func brokerUDSPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return client, server
}

// TestBrokerUDSCancelInterruptsPeerAndPreservesReadGrace cancels a real temporary Unix connection's
// provider context while keeping the terminal read path open to receive measured failure usage.
// TestBrokerUDSCancelInterruptsPeerAndPreservesReadGrace отменяет контекст провайдера через временное
// Unix-соединение, сохраняя путь чтения итога для получения расхода токенов при отказе.
func TestBrokerUDSCancelInterruptsPeerAndPreservesReadGrace(t *testing.T) {
	client, server := brokerUDSPair(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := bindBrokerConnectionContext(ctx, client)
	defer stop()
	peerCtx, join := bindBrokerPeerContext(t.Context(), server, bufio.NewScanner(server))
	defer join()
	select {
	case <-peerCtx.Done():
		t.Fatal("request was prematurely sealed")
	default:
	}
	cancel()
	select {
	case <-peerCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("runner cancellation did not interrupt isolated provider")
	}
	written := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		err := writeProviderBrokerResultFailure(server, Result{Usage: usage}, context.Canceled)
		_ = server.Close()
		written <- err
	}()
	result, err := readProviderBrokerResponse(client)
	if err == nil || result.Usage != usage {
		t.Fatal("cancel closed the usage delivery path")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

// TestAppServerWaitTerminalStreamsNotificationBeforeTerminal drives the in-process event channel and
// requires notification delivery before turn/completed, without launching an external app server.
// TestAppServerWaitTerminalStreamsNotificationBeforeTerminal подаёт события через канал внутри процесса
// и требует доставки сообщения до turn/completed без запуска внешнего app server.
func TestAppServerWaitTerminalStreamsNotificationBeforeTerminal(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.turnStarted = testThreadID, testTurnID, 1
	observed := make(chan struct{}, 1)
	state.onActivity = func(runtimecontract.RuntimeActivity) error { observed <- struct{}{}; return nil }
	events := make(chan streamEvent)
	server := &appServer{messages: events}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.waitTerminal(ctx, state) }()
	events <- streamEvent{message: wireMessage{kind: messageNotification, method: "item/completed", payload: raw(`{"completedAtMs":1,"item":{"id":"final-live","text":"safe","phase":"final_answer","type":"agentMessage"},"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `"}`)}}
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal("process notification did not stream")
	}
	select {
	case <-done:
		t.Fatal("message invented terminal")
	default:
	}
	events <- streamEvent{message: wireMessage{kind: messageNotification, method: "turn/completed", payload: raw(`{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[],"status":"completed"}}`)}}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestBrokerTerminalDropsPublishedCallsAndPreservesUsageOnOverflow proves terminal never batches already
// published calls and an escaped-text size overflow becomes failure without losing measured usage.
// TestBrokerTerminalDropsPublishedCallsAndPreservesUsageOnOverflow проверяет отсутствие уже переданных
// вызовов в итоге и превращение переполнения текста в отказ без потери измеренного расхода.
func TestBrokerTerminalDropsPublishedCallsAndPreservesUsageOnOverflow(t *testing.T) {
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	for _, overflow := range []bool{false, true} {
		var output bytes.Buffer
		result := Result{Outcome: "SUCCEEDED", Usage: usage, FinalMessage: "safe", ToolCalls: make([]runtimecontract.NativeToolCall, runtimecontract.MaximumNativeToolCalls)}
		for index := range result.ToolCalls {
			result.ToolCalls[index].SafeParameters = map[string]any{"oversized": strings.Repeat("x", 3000)}
		}
		if overflow {
			result.FinalMessage = strings.Repeat("<", maximumFinalBytes)
		}
		writer := &brokerFrameWriter{writer: &output}
		if err := writer.finish(brokerResponse{OK: true, Result: result}); err != nil {
			t.Fatal(err)
		}
		got, err := readProviderBrokerResponse(&output)
		if got.Usage != usage || len(got.ToolCalls) != 0 || (err != nil) != overflow {
			t.Fatal("terminal duplicated calls or lost measured usage")
		}
	}
}

// TestBrokerActivityReservesTerminalBudget exhausts byte and frame activity budgets independently,
// then verifies the reserved terminal can still be written without first emitting a rejected activity.
// TestBrokerActivityReservesTerminalBudget отдельно исчерпывает байтовый и кадровый лимиты событий
// и проверяет запись зарезервированного итога без записи отклонённого события.
func TestBrokerActivityReservesTerminalBudget(t *testing.T) {
	for _, frameLimit := range []bool{false, true} {
		var output bytes.Buffer
		writer := &brokerFrameWriter{writer: &output}
		if frameLimit {
			writer.sequence = maximumBrokerFrames - 1
		} else {
			writer.bytes = maximumBrokerStreamBytes - maximumBrokerFrameBytes
		}
		activity := runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "reserved-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}
		if writer.activity(activity) == nil || output.Len() != 0 {
			t.Fatal("activity consumed reserved terminal budget")
		}
		if err := writer.finish(brokerResponse{Failure: providerBrokerFailureProvider}); err != nil || !writer.terminal {
			t.Fatal("reserved terminal could not be written")
		}
	}
}

// TestBrokerUDSCallbackFailureCancelsPeerAndRetainsTerminalUsage uses a real local socket to require
// peer interruption on callback failure, suppression of later callbacks and private error redaction,
// while the subsequent failed terminal still delivers the exact measurements.
// TestBrokerUDSCallbackFailureCancelsPeerAndRetainsTerminalUsage через локальный сокет требует прерывания
// peer при отказе callback, подавления дальнейших callback и сокрытия приватной ошибки,
// сохраняя точные измерения в последующем итоговом отказе.
func TestBrokerUDSCallbackFailureCancelsPeerAndRetainsTerminalUsage(t *testing.T) {
	client, server := brokerUDSPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	peerCtx, join := bindBrokerPeerContext(ctx, server, bufio.NewScanner(server))
	defer join()
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	written := make(chan error, 1)
	go func() {
		frames := &brokerFrameWriter{writer: server}
		activity := runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "failed-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}
		err := frames.activity(activity)
		if err == nil {
			<-peerCtx.Done()
			if ctx.Err() != nil {
				err = errors.New("callback failure did not cancel isolated provider")
			} else {
				activity.Message.ItemID = "suppressed-two"
				err = frames.activity(activity)
			}
		}
		if err == nil {
			err = frames.finish(brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: usage}})
		}
		_ = server.Close()
		written <- err
	}()
	calls := 0
	result, err := readProviderBrokerResponse(client, func(runtimecontract.RuntimeActivity) error {
		calls++
		return errors.New("synthetic private callback error")
	})
	if err == nil || strings.Contains(err.Error(), "private") || result.Usage != usage || calls != 1 {
		t.Fatal("callback failure lost usage, repeated delivery, or exposed private error")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}
