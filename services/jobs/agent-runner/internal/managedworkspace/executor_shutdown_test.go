package managedworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// executorTestRequest uses the closed production HTTP shape without changing peer admission;
// direct handler tests isolate sequencing while the container fixture checks kernel identities.
// executorTestRequest использует production HTTP shape без изменения peer admission; прямые
// handler tests изолируют порядок, а container fixture проверяет kernel identities.
func executorTestRequest(t *testing.T, handler http.Handler, request ExecutorRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(raw)))
	return response
}

// TestExecutorDrainRetainsOnlySameBindingQuiesce refuses selected commands and rebinding after
// cancellation while preserving a single exact stopped-command ACK and the permanent closure.
// TestExecutorDrainRetainsOnlySameBindingQuiesce отклоняет выбранные commands и rebinding после
// отмены, сохраняя единственный exact stopped-command ACK и окончательное закрытие.
func TestExecutorDrainRetainsOnlySameBindingQuiesce(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cleaned, probes, acknowledged := 0, 0, 0
	executor := &commandExecutor{
		binding:  strings.Repeat("a", 64),
		commands: []string{"bash"},
		cleanup: func() error {
			cleaned++
			return nil
		},
		probe: func(context.Context) error {
			probes++
			return nil
		},
	}
	handler := executorHTTPHandler(ctx, executor, func() { acknowledged++ })
	cancel()
	denied := []ExecutorRequest{
		{Action: "INITIALIZE", Binding: executor.binding, Commands: []string{"bash"}},
		{Action: "RUN", Binding: executor.binding, Command: "bash", Arguments: []string{"-c", "printf denied"}, Directory: "work", TimeoutSeconds: 1},
		{Action: "QUIESCE", Binding: strings.Repeat("b", 64)},
	}
	for _, request := range denied {
		if response := executorTestRequest(t, handler, request); response.Code != http.StatusConflict {
			t.Fatal("cancelled executor admitted a command or foreign binding")
		}
	}
	if cleaned != 0 || probes != 0 || acknowledged != 0 {
		t.Fatal("rejected admission changed executor effects or acknowledgement")
	}
	request := ExecutorRequest{Action: "QUIESCE", Binding: executor.binding}
	response := executorTestRequest(t, handler, request)
	var result CommandResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "QUIESCENT" || acknowledged != 1 || cleaned != 1 {
		t.Fatal("same-binding quiescence did not produce exactly one positive ACK")
	}
	if executorTestRequest(t, handler, request).Code != http.StatusConflict || acknowledged != 1 || cleaned != 1 {
		t.Fatal("closed or unknown executor operation was replayed")
	}
}

// failedExecutorFlush simulates a disconnected reply transport after cleanup; it cannot
// authorize a client or fabricate a known received ACK.
// failedExecutorFlush имитирует разрыв reply transport после cleanup; actor и известный
// полученный ACK этим не создаются.
type failedExecutorFlush struct{ *httptest.ResponseRecorder }

// FlushError fails the exact ResponseController boundary used before server closure.
// FlushError отклоняет ту же ResponseController-границу перед server closure.
func (*failedExecutorFlush) FlushError() error { return errors.New("synthetic disconnected ACK") }

// TestExecutorFailedAckFlushDoesNotSignalQuiescence keeps cleanup separate from an accepted
// transport ACK; a second QUIESCE cannot reopen the closed instance.
// TestExecutorFailedAckFlushDoesNotSignalQuiescence отделяет cleanup от принятого transport
// ACK; повтор QUIESCE не открывает закрытый instance.
func TestExecutorFailedAckFlushDoesNotSignalQuiescence(t *testing.T) {
	cleaned, acknowledged := 0, 0
	executor := &commandExecutor{cleanup: func() error { cleaned++; return nil }}
	handler := executorHTTPHandler(t.Context(), executor, func() { acknowledged++ })
	request := ExecutorRequest{Action: "QUIESCE", Binding: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(request)
	writer := &failedExecutorFlush{httptest.NewRecorder()}
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(raw)))
	if cleaned != 1 || acknowledged != 0 || executorTestRequest(t, handler, request).Code != http.StatusConflict {
		t.Fatal("failed transport flush became a positive or replayable ACK")
	}
}

// TestExecutorFailedCleanupCannotAcknowledgeOrReopen rejects an unknown child join before
// any success bytes and keeps the instance permanently closed without retrying its operation.
// TestExecutorFailedCleanupCannotAcknowledgeOrReopen отклоняет unknown child join до записи
// успеха и навсегда закрывает instance без повторного исполнения исходной операции.
func TestExecutorFailedCleanupCannotAcknowledgeOrReopen(t *testing.T) {
	cleaned, acknowledged := 0, 0
	executor := &commandExecutor{cleanup: func() error {
		cleaned++
		return errors.New("synthetic unknown child join")
	}}
	handler := executorHTTPHandler(t.Context(), executor, func() { acknowledged++ })
	request := ExecutorRequest{Action: "QUIESCE", Binding: strings.Repeat("a", 64)}
	if executorTestRequest(t, handler, request).Code != http.StatusConflict ||
		executorTestRequest(t, handler, request).Code != http.StatusConflict ||
		cleaned != 1 || acknowledged != 0 || !executor.closed {
		t.Fatal("unknown child join became an acknowledged or replayable operation")
	}
}

// TestExecutorDrainDeadlineWithoutAckFailsClosed exercises the real server shutdown with a
// short test budget, inert cleanup and no peer; it does not claim PID namespace qualification.
// TestExecutorDrainDeadlineWithoutAckFailsClosed проверяет настоящий server shutdown с коротким
// test budget, inert cleanup и без peer; PID namespace qualification этим не заявляется.
func TestExecutorDrainDeadlineWithoutAckFailsClosed(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "executor.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cleaned := 0
	executor := &commandExecutor{cleanup: func() error { cleaned++; return nil }}
	done := make(chan error, 1)
	go func() { done <- serveExecutor(ctx, listener, executor, 30*time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errExecutor) || cleaned != 1 || !executor.closed {
			t.Fatal("no-ACK deadline was reported as acknowledged quiescence")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor drain deadline was not bounded")
	}
}

// TestExecutorLifecycleCancellationReachesActiveProbe proves the HTTP request context is
// cancelled for active command admission while the separate handshake context remains usable.
// TestExecutorLifecycleCancellationReachesActiveProbe доказывает отмену HTTP request context
// активного command admission при отдельном доступном handshake context.
func TestExecutorLifecycleCancellationReachesActiveProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	executor := &commandExecutor{cleanup: func() error { return nil }, probe: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	handler := executorHTTPHandler(ctx, executor, func() { t.Error("probe failure cannot acknowledge quiescence") })
	done := make(chan int, 1)
	go func() {
		response := executorTestRequest(t, handler, ExecutorRequest{Action: "INITIALIZE", Binding: strings.Repeat("a", 64)})
		done <- response.Code
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("active executor probe did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code != http.StatusConflict || !executor.closed {
			t.Fatal("cancelled active probe reopened admission")
		}
	case <-time.After(time.Second):
		t.Fatal("active executor admission ignored lifecycle cancellation")
	}
}
