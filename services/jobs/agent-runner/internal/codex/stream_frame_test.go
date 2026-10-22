package codex

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// syntheticTerminalFrame pads synthetic command output to the requested encoded frame size so tests
// cross the actual JSONL reader/parser without starting Codex, networking or a model.
// syntheticTerminalFrame дополняет синтетический вывод команды до запрошенного размера кодированного кадра
// для проверки настоящего JSONL reader/parser без запуска Codex, сети или модели.
func syntheticTerminalFrame(t *testing.T, size int) []byte {
	t.Helper()
	command := map[string]any{"type": "commandExecution", "id": "synthetic-command", "command": "printf synthetic", "cwd": "/workspace/work", "status": "completed", "commandActions": []any{}, "aggregatedOutput": "", "exitCode": 0, "durationMs": 1}
	final := map[string]any{"type": "agentMessage", "id": "synthetic-final", "text": "Synthetic final", "phase": "final_answer"}
	envelope := map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "completed", "items": []any{command, final}}}}
	frame := marshalProtocolFixture(t, envelope)
	if size < len(frame) {
		t.Fatal("synthetic frame budget too small")
	}
	command["aggregatedOutput"] = strings.Repeat("x", size-len(frame))
	return marshalProtocolFixture(t, envelope)
}

// TestTerminalAggregateAboveSemanticLineBudget accepts a large transport aggregate whose individual
// semantic projections remain valid, retaining safe final text and one tool call without its raw output.
// TestTerminalAggregateAboveSemanticLineBudget принимает большой транспортный итог с допустимыми отдельными
// смысловыми проекциями, сохраняя безопасный ответ и один вызов инструмента без исходного вывода.
func TestTerminalAggregateAboveSemanticLineBudget(t *testing.T) {
	frame := syntheticTerminalFrame(t, 2_297_142)
	events := make(chan streamEvent, 2)
	readAppServerMessagesUntil(bytes.NewReader(append(frame, '\n')), events, nil, nil)
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.workspaceRoot, state.turnStarted = testThreadID, testTurnID, "/workspace/work", 1
	server := &appServer{messages: events}
	if err := server.waitTerminal(t.Context(), state); err != nil {
		t.Fatalf("synthetic terminal frame rejected: %v", err)
	}
	if state.result.Outcome != "SUCCEEDED" || state.result.FinalMessage != "Synthetic final" || len(state.toolCalls) != 1 {
		t.Fatal("terminal aggregate changed safe result projection")
	}
	t.Logf("synthetic terminal frame accepted: bytes=%d", len(frame))
}

// syntheticResponseFrame builds a JSON response of the requested encoded size for boundary fixtures;
// callers supply a size at least as large as the empty envelope.
// syntheticResponseFrame формирует JSON-ответ запрошенного размера для граничных fixtures;
// вызывающий код обязан передать размер не меньше пустого конверта.
func syntheticResponseFrame(t *testing.T, size int) []byte {
	t.Helper()
	frame := marshalProtocolFixture(t, map[string]any{"id": 1, "result": ""})
	return marshalProtocolFixture(t, map[string]any{"id": 1, "result": strings.Repeat("x", size-len(frame))})
}

// TestAppServerFrameExactTransportBoundary checks exact and one-byte-over limits for EOF, LF and CRLF
// and requires exactly one resulting event followed by channel closure.
// TestAppServerFrameExactTransportBoundary проверяет точный лимит и превышение на байт для EOF, LF и CRLF,
// требуя ровно одно результирующее событие с последующим закрытием канала.
func TestAppServerFrameExactTransportBoundary(t *testing.T) {
	for _, ending := range []struct{ name, value string }{{"EOF", ""}, {"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, delta := range []int{0, 1} {
			t.Run(ending.name+"/overflow_"+strconv.Itoa(delta), func(t *testing.T) {
				frame := syntheticResponseFrame(t, maximumAppServerFrameBytes+delta)
				events := make(chan streamEvent, 2)
				readAppServerMessagesUntil(io.MultiReader(bytes.NewReader(frame), strings.NewReader(ending.value)), events, nil, nil)
				event, ok := <-events
				if !ok || delta == 0 && event.err != nil || delta == 1 && safeAppServerStreamFailureReason(event.err) != "FRAME_TOO_LARGE" {
					t.Fatal("transport frame boundary changed")
				}
				if _, open := <-events; open {
					t.Fatal("reader emitted extra boundary event")
				}
			})
		}
	}
}

// syntheticReadError injects a private underlying reader failure without depending on real I/O faults.
// syntheticReadError подставляет приватный отказ reader без зависимости от реальных ошибок ввода-вывода.
type syntheticReadError struct{}

// Read returns no bytes and a synthetic private error to exercise downstream diagnostic redaction.
// Read возвращает ноль байтов и синтетическую приватную ошибку для проверки сокрытия downstream-диагностики.
func (syntheticReadError) Read([]byte) (int, error) {
	return 0, errors.New("private-upstream-detail")
}

// TestAppServerStreamReasonsRemainClosed tests empty/malformed/duplicate/trailing frames and read errors,
// retaining internal subtypes while the public diagnostic stays STREAM_INVALID and injected reasons stay hidden.
// TestAppServerStreamReasonsRemainClosed проверяет пустые, повреждённые, повторные и лишние кадры с ошибками чтения,
// сохраняя внутренние подтипы, публичный STREAM_INVALID и сокрытие подставленных причин.
func TestAppServerStreamReasonsRemainClosed(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		reader       io.Reader
	}{
		{"empty", "EMPTY_FRAME", strings.NewReader("\n")},
		{"malformed", "WIRE_INVALID", strings.NewReader("private-upstream-detail\n")},
		{"unknown field", "WIRE_INVALID", strings.NewReader("{\"id\":1,\"result\":{},\"private-upstream-detail\":true}\n")},
		{"duplicate", "WIRE_INVALID", strings.NewReader("{\"id\":1,\"id\":1,\"result\":{}}\n")},
		{"trailing JSON", "WIRE_INVALID", strings.NewReader("{\"id\":1,\"result\":{}} {}\n")},
		{"read error", "READ_ERROR", syntheticReadError{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := make(chan streamEvent, 2)
			readAppServerMessagesUntil(test.reader, events, nil, nil)
			event := <-events
			err := atProviderStage(providerStageTerminalWait, callFailure("STREAM_INVALID", event.err))
			if safeAppServerStreamFailureReason(err) != test.reason || strings.Contains(err.Error(), "private-upstream-detail") {
				t.Fatal("unsafe or missing closed stream subtype")
			}
			diagnostic, _ := providerFailureDetails(providerStageTerminalWait, err)
			if diagnostic.Detail != "STREAM_INVALID" || diagnostic.NotificationError != "NONE" {
				t.Fatal("internal stream subtype changed public diagnostic contract")
			}
		})
	}
	output := captureProviderDiagnostic(t)
	logProviderSafeFailure(providerStageTerminalWait, callFailure("STREAM_INVALID", &appServerStreamFailure{reason: "private-upstream-detail"}))
	if strings.Contains(output.String(), "private-upstream-detail") || !strings.Contains(output.String(), "stream_error: UNKNOWN") {
		t.Fatal("injected stream subtype escaped closed log vocabulary")
	}
}

// TestAppServerStreamMessageLimitUnchanged feeds one message beyond the allowed count and requires
// the limit event at the exact boundary followed by the reader's explicit completion signal.
// TestAppServerStreamMessageLimitUnchanged подаёт на одно сообщение больше лимита и требует
// событие ограничения на точной границе с явным сигналом завершения reader.
func TestAppServerStreamMessageLimitUnchanged(t *testing.T) {
	frame := []byte("{\"id\":1,\"result\":{}}\n")
	events := make(chan streamEvent, appServerMessageQueueCapacity)
	done := make(chan struct{})
	go readAppServerMessagesUntil(bytes.NewReader(bytes.Repeat(frame, maximumJSONLMessages+1)), events, nil, done)
	count := 0
	for event := range events {
		if event.err != nil {
			if count != maximumJSONLMessages || safeAppServerStreamFailureReason(event.err) != "MESSAGE_LIMIT" {
				t.Fatal("message count limit changed")
			}
			<-done
			return
		}
		count++
	}
	t.Fatal("message count limit was not enforced")
}

// TestAppServerFrameQueuePreservesBudgetAndStopJoin pins transport/semantic budgets and fills the
// bounded event queue before cancelling, requiring reader completion without publishing an extra event.
// TestAppServerFrameQueuePreservesBudgetAndStopJoin закрепляет транспортный и смысловой бюджеты, заполняет
// ограниченную очередь перед отменой и требует завершения reader без публикации лишнего события.
func TestAppServerFrameQueuePreservesBudgetAndStopJoin(t *testing.T) {
	if appServerMessageQueueCapacity*maximumAppServerFrameBytes != 64<<20 || appServerMessageQueueCapacity != 8 || maximumJSONLLineBytes != 1<<20 {
		t.Fatal("transport queue or semantic budget changed")
	}
	events := make(chan streamEvent, appServerMessageQueueCapacity)
	for index := 0; index < cap(events); index++ {
		events <- streamEvent{}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go readAppServerMessagesUntil(bytes.NewReader(append(syntheticTerminalFrame(t, 2_297_142), '\n')), events, stop, done)
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bounded reader did not join with a full queue")
	}
	count := 0
	for range events {
		count++
	}
	if count != cap(events) {
		t.Fatal("cancelled reader published into a full queue")
	}
}

// TestAppServerLargerFrameRetainsSemanticQuotas mutates a transport-valid aggregate to exceed command,
// final-message or public-message limits or alter thread identity; semantic consumption must still reject it.
// TestAppServerLargerFrameRetainsSemanticQuotas подменяет транспортно допустимый итог с превышением лимитов
// команды, итогового/публичного сообщения или с чужим потоком; смысловой разбор всё равно обязан отказать.
func TestAppServerLargerFrameRetainsSemanticQuotas(t *testing.T) {
	for _, test := range []struct{ from, to string }{
		{"printf synthetic", strings.Repeat("x", maximumJSONLLineBytes+1)},
		{"Synthetic final", strings.Repeat("x", maximumFinalBytes+1)},
		{"Synthetic final", strings.Repeat("x", runtimecontract.MaximumRuntimeMessageBytes+1)},
		{testThreadID, "01980000-0000-7000-8000-000000000099"},
	} {
		frame := bytes.Replace(syntheticTerminalFrame(t, 2_297_142), []byte(test.from), []byte(test.to), 1)
		events := make(chan streamEvent, 2)
		readAppServerMessagesUntil(bytes.NewReader(append(frame, '\n')), events, nil, nil)
		state := newProtocolState(testThreadID)
		state.threadID, state.turnID, state.workspaceRoot, state.turnStarted = testThreadID, testTurnID, "/workspace/work", 1
		if err := (&appServer{messages: events}).waitTerminal(t.Context(), state); err == nil {
			t.Fatal("larger transport frame weakened semantic or identity checks")
		}
	}
}
