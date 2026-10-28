package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// diagnosticRefreshFixture drives degradation and recovery without network access or identity material.
// RU: diagnosticRefreshFixture задаёт деградацию и восстановление без сети и материалов идентификации.
type diagnosticRefreshFixture struct {
	class  string
	calls  int
	cancel context.CancelFunc
}

// SigningKeyFailureClass observes fixture state without issuing another refresh.
// RU: SigningKeyFailureClass читает состояние fixture без нового обновления.
func (fixture *diagnosticRefreshFixture) SigningKeyFailureClass() string { return fixture.class }

// Refresh recovers on the second tick and cancels the worker after a final successful tick.
// RU: Refresh восстанавливается на втором такте и отменяет worker после последнего успешного такта.
func (fixture *diagnosticRefreshFixture) Refresh(context.Context) error {
	fixture.calls++
	if fixture.calls == 1 {
		return errors.New("private fixture cause must not be logged")
	}
	fixture.class = ""
	if fixture.calls == 3 {
		fixture.cancel()
	}
	return nil
}

// TestOIDCInitialFailureAndRecoveryAreObservedOnce proves startup diagnostics add no fetch, first repeated failure is not duplicated and recovery remains observable.
// RU: TestOIDCInitialFailureAndRecoveryAreObservedOnce доказывает отсутствие лишнего fetch, дублирования первого отказа и потерянного сообщения о восстановлении.
func TestOIDCInitialFailureAndRecoveryAreObservedOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	fixture := &diagnosticRefreshFixture{class: "dns", cancel: cancel}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	recordInitialOIDCRefresh(ctx, logger, fixture)
	if fixture.calls != 0 {
		t.Fatal("startup diagnostic performed a refresh")
	}
	err := oidcRefreshWorker(fixture, logger, Config{ReadinessInterval: time.Millisecond, RPCTimeout: time.Second})(ctx)
	if !errors.Is(err, context.Canceled) || fixture.calls != 3 {
		t.Fatal("worker cancellation or refresh lifecycle changed")
	}
	text := output.String()
	if strings.Count(text, oidcInitialDegradedMessage) != 1 || strings.Count(text, oidcRefreshRestoredMessage) != 1 || strings.Contains(text, oidcRefreshDegradedMessage) || strings.Contains(text, "private fixture") || strings.Count(text, "\n") != 2 {
		t.Fatalf("unexpected diagnostic transitions: %s", text)
	}
	if !strings.Contains(text, `"error_class":"dns"`) {
		t.Fatal("initial closed cause was lost")
	}
}
