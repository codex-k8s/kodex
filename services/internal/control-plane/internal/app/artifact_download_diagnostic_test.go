package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// artifactDiagnosticTestStream embeds unsupported stream operations and supplies only a caller
// context for error-boundary observer tests.
// artifactDiagnosticTestStream встраивает неподдерживаемые stream операции и предоставляет только
// caller context для тестов error boundary observer.
type artifactDiagnosticTestStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the configured synthetic stream context without transport metadata or external
// stream activity.
// Context возвращает заданный синтетический stream context без transport metadata либо внешней
// stream активности.
func (stream artifactDiagnosticTestStream) Context() context.Context { return stream.ctx }

// artifactDiagnosticTestStatus keeps a synthetic gRPC code and unwrap cause separate so tests
// prove observation cannot leak nested private diagnostic text.
// artifactDiagnosticTestStatus разделяет синтетические gRPC code и unwrap cause для доказательства
// отсутствия утечки вложенного private diagnostic текста в наблюдение.
type artifactDiagnosticTestStatus struct {
	cause error
	code  codes.Code
}

// Error returns only the fixed safe gRPC status error string while leaving the nested synthetic
// cause available through Unwrap.
// Error возвращает только фиксированную безопасную строку gRPC status error с сохранением
// вложенной синтетической причины через Unwrap.
func (err artifactDiagnosticTestStatus) Error() string { return err.GRPCStatus().Err().Error() }

// Unwrap exposes the synthetic nested cause to typed diagnostic lookup without incorporating it
// into the safe status text.
// Unwrap предоставляет синтетическую вложенную причину типизированному diagnostic поиску без
// включения в безопасный status текст.
func (err artifactDiagnosticTestStatus) Unwrap() error { return err.cause }

// GRPCStatus returns the configured gRPC code with one fixed safe unavailable message instead of
// the nested fixture error.
// GRPCStatus возвращает заданный gRPC code с одним фиксированным безопасным сообщением
// недоступности вместо вложенной ошибки оснастки.
func (err artifactDiagnosticTestStatus) GRPCStatus() *status.Status {
	return status.New(err.code, "operation is temporarily unavailable")
}

// TestArtifactDownloadDiagnosticStreamObserverLogsOnlyClosedProjection checks exactly one
// unchanged Unavailable observation includes only known fields, normalizes unknown SQLSTATE and
// excludes nested private cause or synthetic identity text.
// TestArtifactDownloadDiagnosticStreamObserverLogsOnlyClosedProjection проверяет ровно одно
// неизменённое Unavailable наблюдение лишь с известными полями, нормализацию неизвестного SQLSTATE
// и отсутствие вложенной private причины либо синтетического identity текста.
func TestArtifactDownloadDiagnosticStreamObserverLogsOnlyClosedProjection(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	method := controlplanev1.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	for _, sqlState := range []string{"40001", "PRIVATE_SQLSTATE"} {
		output.Reset()
		cause := artifactread.WithFailure(fmt.Errorf("PRIVATE_TOKEN SELECT secret user@example.test: %w", errs.ErrUnavailable), artifactread.NewDiagnostic(artifactread.StageAudit, artifactread.ClassPostgres, sqlState))
		cause = artifactread.WithAttempt(cause, artifactread.MaximumAttempts, true)
		failure := artifactDiagnosticTestStatus{cause: cause, code: codes.Unavailable}
		calls := 0
		boundary := grpcserver.StreamErrorBoundary(grpcserver.ErrorObserverFunc(func(ctx context.Context, method string, code codes.Code, err error) {
			calls++
			logUnexpectedGRPCStreamFailure(ctx, method, code, err)
		}))
		actual := boundary(nil, artifactDiagnosticTestStream{ctx: t.Context()}, &grpc.StreamServerInfo{FullMethod: method}, func(any, grpc.ServerStream) error { return failure })
		if status.Code(actual) != codes.Unavailable || calls != 1 {
			t.Fatal("error boundary changed outcome or emitted duplicate observation")
		}
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatal("stream observer did not produce one JSON record")
		}
		state := sqlState
		if sqlState != "40001" {
			state = "OTHER"
		}
		expected := map[string]any{
			"level": "ERROR", "msg": unexpectedGRPCStreamFailureMessage, "method": method, "code": "Unavailable",
			artifactReadStageKey: artifactread.StageAudit, artifactReadClassKey: artifactread.ClassPostgres,
			artifactReadSQLStateKey: state, artifactReadAttemptKey: float64(artifactread.MaximumAttempts), artifactReadExhaustedKey: true,
		}
		if len(record) != len(expected)+1 || record["time"] == nil || strings.Contains(output.String(), "PRIVATE") || strings.Contains(output.String(), "user@example") {
			t.Fatal("stream diagnostic leaked raw cause or introduced unexpected fields")
		}
		for key, value := range expected {
			if record[key] != value {
				t.Fatalf("safe field %s differs", key)
			}
		}
	}
}

// TestArtifactDownloadDiagnosticObserverDoesNotTrustTextOrOtherMethod checks raw text cannot forge
// typed fields, another RPC gets no owner-download diagnostic and expected Aborted produces no
// server-error observation.
// TestArtifactDownloadDiagnosticObserverDoesNotTrustTextOrOtherMethod проверяет невозможность
// подделки typed полей raw текстом, отсутствие owner download диагностики у другого RPC и
// отсутствие server-error наблюдения для ожидаемого Aborted.
func TestArtifactDownloadDiagnosticObserverDoesNotTrustTextOrOtherMethod(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	method := controlplanev1.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	safe := artifactread.WithFailure(errs.ErrUnavailable, artifactread.NewDiagnostic(artifactread.StageAudit, artifactread.ClassPostgres, "40001"))
	for _, test := range []struct {
		method string
		cause  error
	}{
		{method, errors.New("PRIVATE_TOKEN safe_stage=AUDIT_INSERT sqlstate=40001")},
		{controlplanev1.PlatformCommandService_DownloadArtifact_FullMethodName, safe},
	} {
		output.Reset()
		logUnexpectedGRPCStreamFailure(t.Context(), test.method, codes.Unavailable, test.cause)
		var record map[string]any
		if json.Unmarshal(output.Bytes(), &record) != nil || len(record) != 5 || record["time"] == nil || record["method"] != test.method || strings.Contains(output.String(), "PRIVATE") {
			t.Fatal("untyped cause or another method acquired diagnostic fields")
		}
	}
	output.Reset()
	boundary := grpcserver.StreamErrorBoundary(grpcserver.ErrorObserverFunc(logUnexpectedGRPCStreamFailure))
	_ = boundary(nil, artifactDiagnosticTestStream{ctx: t.Context()}, &grpc.StreamServerInfo{FullMethod: method}, func(any, grpc.ServerStream) error {
		return artifactDiagnosticTestStatus{cause: safe, code: codes.Aborted}
	})
	if output.Len() != 0 {
		t.Fatal("expected conflict became server error observation")
	}
}
