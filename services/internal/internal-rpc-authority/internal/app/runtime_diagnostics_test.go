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

const diagnosticSentinel = "synthetic-private-dsn-token-sql-path-sentinel"

type unprintableDiagnosticError struct{}

func (unprintableDiagnosticError) Error() string { panic("diagnostic must not format cause") }

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
