package app

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeStage string

const (
	stageConfiguration         runtimeStage = "CONFIGURATION"
	stageObservability         runtimeStage = "OBSERVABILITY"
	stagePostgres              runtimeStage = "POSTGRES"
	stageSnapshotLoad          runtimeStage = "SNAPSHOT_LOAD"
	stageAuthorityConstruction runtimeStage = "AUTHORITY_CONSTRUCTION"
	stageReadbackClient        runtimeStage = "READBACK_CLIENT"
	stageRestoreClient         runtimeStage = "RESTORE_CLIENT"
	stageRestoreVerify         runtimeStage = "RESTORE_VERIFY"
	stageSnapshotActivation    runtimeStage = "SNAPSHOT_ACTIVATION"
	stageRestoreOpen           runtimeStage = "RESTORE_OPEN"
	stageLocalServers          runtimeStage = "LOCAL_SERVERS"
	stageRuntime               runtimeStage = "RUNTIME"
	diagnosticUnknown                       = "UNKNOWN"
	diagnosticNone                          = "NONE"
	diagnosticDeadline                      = "DEADLINE"
	diagnosticCanceled                      = "CANCELED"
	diagnosticNetwork                       = "NETWORK"
	diagnosticNetworkTimeout                = "NETWORK_TIMEOUT"
)

type runtimeStageError struct {
	stage runtimeStage
	cause error
}

func (err *runtimeStageError) Error() string { return "authority runtime stage failed" }
func (err *runtimeStageError) Unwrap() error { return err.cause }

// SafeRuntimeFailure — единственная строка CLI для issuer/verifier. Она не
// вызывает Error() причины и не выводит Message/Detail/Hint/URL/path/SQL.
// Исходная цепочка сохраняется для errors.Is/As, но не сериализуется.
func SafeRuntimeFailure(mode Mode, err error) string {
	role := diagnosticUnknown
	if mode == ModeIssuer || mode == ModeVerifier {
		role = string(mode)
	}
	stage := diagnosticUnknown
	var staged *runtimeStageError
	if errors.As(err, &staged) {
		switch staged.stage {
		case stageConfiguration, stageObservability, stagePostgres, stageSnapshotLoad,
			stageAuthorityConstruction, stageReadbackClient, stageRestoreClient,
			stageRestoreVerify, stageSnapshotActivation, stageRestoreOpen, stageLocalServers, stageRuntime:
			stage = string(staged.stage)
		}
	}
	kind := diagnosticNone
	var domain *failure.Error
	if errors.As(err, &domain) {
		kind = diagnosticUnknown
		switch domain.Kind {
		case failure.InvalidRequest, failure.NotFound, failure.Unauthenticated,
			failure.PermissionDenied, failure.OperationNotAllowed, failure.AuthorityRejected,
			failure.BindingMismatch, failure.ReplayDetected, failure.SnapshotRejected,
			failure.PersistenceUnavailable, failure.Internal:
			kind = string(domain.Kind)
		}
	}
	contextState := diagnosticNone
	if errors.Is(err, context.DeadlineExceeded) {
		contextState = diagnosticDeadline
	} else if errors.Is(err, context.Canceled) {
		contextState = diagnosticCanceled
	}
	class, sqlstate := runtimeErrorClass(err)
	return fmt.Sprintf("internal-rpc-authority runtime failed: mode=%s stage=%s domain_kind=%s error_class=%s sqlstate=%s context_state=%s", role, stage, kind, class, sqlstate, contextState)
}

func runtimeErrorClass(err error) (string, string) {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		code := diagnosticUnknown
		switch postgres.Code {
		case "08000", "08001", "08003", "08004", "08006", "08007", "08P01",
			"22001", "22003", "22P02", "23502", "23503", "23505", "23514",
			"28000", "28P01", "40001", "40P01", "42501", "42601", "42703",
			"42883", "42P01", "53300", "53400", "57014", "57P01", "57P02", "57P03":
			code = postgres.Code
		}
		return "POSTGRES", code
	}
	// После retry budget к исходному отказу присоединяется ctx.Err(). Сохраняем
	// его причину отдельно от context_state, не подменяем всё словом timeout.
	if errors.Is(err, repository.ErrSnapshotRollback) {
		return "SNAPSHOT_REJECTED", diagnosticNone
	}
	var rpc interface{ GRPCStatus() *status.Status }
	if errors.As(err, &rpc) && rpc.GRPCStatus() != nil {
		switch rpc.GRPCStatus().Code() {
		case codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded,
			codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted,
			codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
			codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated:
			return "GRPC_" + rpc.GRPCStatus().Code().String(), diagnosticNone
		}
		return "GRPC_UNKNOWN", diagnosticNone
	}
	var network *net.OpError
	if errors.As(err, &network) {
		if network.Timeout() {
			return diagnosticNetworkTimeout, diagnosticNone
		}
		return diagnosticNetwork, diagnosticNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return diagnosticDeadline, diagnosticNone
	}
	if errors.Is(err, context.Canceled) {
		return diagnosticCanceled, diagnosticNone
	}
	var otherNetwork net.Error
	if errors.As(err, &otherNetwork) {
		if otherNetwork.Timeout() {
			return diagnosticNetworkTimeout, diagnosticNone
		}
		return diagnosticNetwork, diagnosticNone
	}
	return diagnosticUnknown, diagnosticNone
}
