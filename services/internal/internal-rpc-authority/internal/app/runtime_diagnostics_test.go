package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// diagnosticSentinel is synthetic private-looking test text placed in error details, paths and unknown labels so tests can detect reflection without using credentials.
// diagnosticSentinel — синтетический тестовый текст вида закрытых данных, помещаемый в подробности ошибок, пути и неизвестные метки для обнаружения отражения без использования реквизитов.
const diagnosticSentinel = "synthetic-private-dsn-token-sql-path-sentinel"

// unprintableDiagnosticError is a synthetic cause whose Error method panics, detecting accidental cause formatting by the safe diagnostic path.
// unprintableDiagnosticError — синтетическая причина с паникующим методом Error, обнаруживающая случайное форматирование причины безопасным диагностическим путём.
type unprintableDiagnosticError struct{}

// Error deliberately panics instead of returning text; successful diagnostic tests demonstrate that this synthetic method was not invoked.
// Error намеренно вызывает панику вместо возврата текста; успешные диагностические тесты показывают, что этот синтетический метод не вызывался.
func (unprintableDiagnosticError) Error() string { panic("diagnostic must not format cause") }

// TestSafeRuntimeFailureClosedClassification checks thirteen synthetic error chains for exact bounded fields, SQL/network/context precedence, unknown fallbacks, private-text suppression and original cause retention. The unprintable cause checks that formatting avoids Error; no database or remote RPC runs.
// TestSafeRuntimeFailureClosedClassification проверяет тринадцать синтетических цепочек ошибок по точным ограниченным полям, приоритетам SQL/сети/контекста, запасным значениям, скрытию закрытого текста и сохранению причины. Непечатаемая причина проверяет отсутствие вызова Error при форматировании; БД и удалённый RPC не запускаются.
func TestSafeRuntimeFailureClosedClassification(t *testing.T) {
	postgres := &pgconn.PgError{Code: "42501", Message: diagnosticSentinel, Detail: diagnosticSentinel, Hint: diagnosticSentinel, Where: diagnosticSentinel, SchemaName: diagnosticSentinel, TableName: diagnosticSentinel, File: diagnosticSentinel}
	for _, tt := range []struct {
		name                                string
		err                                 error
		kind, class, sqlstate, contextState string
	}{
		{"postgres nested timeout", errors.Join(failure.Wrap(failure.PersistenceUnavailable, diagnosticSentinel, fmt.Errorf("%s: %w", diagnosticSentinel, postgres)), context.DeadlineExceeded), "PERSISTENCE_UNAVAILABLE", "POSTGRES", "42501", "DEADLINE"},
		{"snapshot rejection timeout", errors.Join(failure.Wrap(failure.SnapshotRejected, diagnosticSentinel, repository.ErrSnapshotRollback), context.DeadlineExceeded), "SNAPSHOT_REJECTED", "SNAPSHOT_REJECTED", "NONE", "DEADLINE"},
		{"domain only", failure.New(failure.SnapshotRejected, diagnosticSentinel), "SNAPSHOT_REJECTED", "UNKNOWN", "NONE", "NONE"},
		{"network", failure.Wrap(failure.PersistenceUnavailable, diagnosticSentinel, &net.OpError{Op: diagnosticSentinel, Net: diagnosticSentinel, Addr: &net.UnixAddr{Name: diagnosticSentinel}, Err: errors.New(diagnosticSentinel)}), "PERSISTENCE_UNAVAILABLE", "NETWORK", "NONE", "NONE"},
		{"network timeout", &net.OpError{Op: diagnosticSentinel, Err: context.DeadlineExceeded}, "NONE", "NETWORK_TIMEOUT", "NONE", "DEADLINE"},
		{"deadline", context.DeadlineExceeded, "NONE", "DEADLINE", "NONE", "DEADLINE"},
		{"canceled", context.Canceled, "NONE", "CANCELED", "NONE", "CANCELED"},
		{"grpc", status.Error(codes.Unavailable, diagnosticSentinel), "NONE", "GRPC_Unavailable", "NONE", "NONE"},
		{"unknown grpc", status.Error(codes.Code(991), diagnosticSentinel), "NONE", "GRPC_UNKNOWN", "NONE", "NONE"},
		{"unknown SQLSTATE", &pgconn.PgError{Code: diagnosticSentinel, Message: diagnosticSentinel}, "NONE", "POSTGRES", "UNKNOWN", "NONE"},
		{"unknown kind", failure.New(failure.Kind(diagnosticSentinel), diagnosticSentinel), "UNKNOWN", "UNKNOWN", "NONE", "NONE"},
		{"private path", &os.PathError{Op: diagnosticSentinel, Path: diagnosticSentinel, Err: errors.New(diagnosticSentinel)}, "NONE", "UNKNOWN", "NONE", "NONE"},
		{"unprintable", unprintableDiagnosticError{}, "NONE", "UNKNOWN", "NONE", "NONE"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			staged := &runtimeStageError{stage: stageSnapshotActivation, cause: tt.err}
			want := fmt.Sprintf("internal-rpc-authority runtime failed: mode=issuer stage=SNAPSHOT_ACTIVATION domain_kind=%s error_class=%s sqlstate=%s context_state=%s", tt.kind, tt.class, tt.sqlstate, tt.contextState)
			got := SafeRuntimeFailure(ModeIssuer, staged)
			if got != want || strings.Contains(got, diagnosticSentinel) {
				t.Fatalf("unexpected safe diagnostic: %q", got)
			}
			if staged.Unwrap() != tt.err {
				t.Fatal("diagnostic wrapping changed cause")
			}
		})
	}
}

// TestSafeRuntimeFailureNormalizesEveryDynamicField checks unknown mode, stage and domain labels against one exact fallback line, then checks visibility of all twelve registered stages for verifier. It does not claim exhaustive coverage of arbitrary error implementations.
// TestSafeRuntimeFailureNormalizesEveryDynamicField проверяет неизвестные режим, этап и доменные метки по одной точной запасной строке, затем видимость всех двенадцати зарегистрированных этапов для verifier. Полный перебор произвольных реализаций ошибок не заявляется.
func TestSafeRuntimeFailureNormalizesEveryDynamicField(t *testing.T) {
	err := &runtimeStageError{stage: runtimeStage(diagnosticSentinel), cause: failure.New(failure.Kind(diagnosticSentinel), diagnosticSentinel)}
	got := SafeRuntimeFailure(Mode(diagnosticSentinel), err)
	if got != "internal-rpc-authority runtime failed: mode=UNKNOWN stage=UNKNOWN domain_kind=UNKNOWN error_class=UNKNOWN sqlstate=NONE context_state=NONE" {
		t.Fatalf("unexpected closed fallback: %q", got)
	}
	for _, stage := range []runtimeStage{stageConfiguration, stageObservability, stagePostgres, stageSnapshotLoad, stageAuthorityConstruction, stageReadbackClient, stageRestoreClient, stageRestoreVerify, stageSnapshotActivation, stageRestoreOpen, stageLocalServers, stageRuntime} {
		if !strings.Contains(SafeRuntimeFailure(ModeVerifier, &runtimeStageError{stage: stage, cause: errors.New(diagnosticSentinel)}), "mode=verifier stage="+string(stage)+" ") {
			t.Fatal("registered stage is not observable")
		}
	}
}

// TestRunClassifiesConfigurationFailureWithoutPrintingValues calls LoadConfig and Run with an invalid synthetic mode and verifies a sanitized CONFIGURATION failure. The early mode rejection occurs before dependency setup; this is not a successful issuer/verifier startup test.
// TestRunClassifiesConfigurationFailureWithoutPrintingValues вызывает LoadConfig и Run с некорректным синтетическим режимом и проверяет очищенную ошибку CONFIGURATION. Ранний отказ режима происходит до настройки зависимостей; успешный запуск issuer/verifier этим не проверяется.
func TestRunClassifiesConfigurationFailureWithoutPrintingValues(t *testing.T) {
	_, err := LoadConfig(Mode(diagnosticSentinel))
	if err == nil {
		t.Fatal("invalid mode accepted")
	}
	err = Run(context.Background(), context.Background(), Mode(diagnosticSentinel), "test")
	if err == nil {
		t.Fatal("invalid runtime started")
	}
	got := SafeRuntimeFailure(ModeVerifier, err)
	if !strings.Contains(got, "stage=CONFIGURATION ") || strings.Contains(got, diagnosticSentinel) {
		t.Fatalf("unsafe runtime diagnostic: %q", got)
	}
}
