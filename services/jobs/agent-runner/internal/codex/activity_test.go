package codex

import (
	"bufio"
	"bytes"
	"context"
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
