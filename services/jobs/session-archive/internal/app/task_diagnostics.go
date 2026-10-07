package app

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	archiveRPCObservationMessage      = "session archive task RPC observation"
	archiveRPCStageAttribute          = "stage"
	archiveTaskRefAttribute           = "task_ref"
	archiveSessionRefAttribute        = "session_ref"
	archiveTaskKindAttribute          = "task_kind"
	archiveContentGenerationAttribute = "content_generation"
	archiveAttemptAttribute           = "attempt"
	archiveDiagnosticUnknown          = "UNKNOWN"
)

type archiveRPCStage string

const (
	archiveRPCRenew                  archiveRPCStage = "RENEW"
	archiveRPCFail                   archiveRPCStage = "FAIL"
	archiveRPCCompleteSnapshot       archiveRPCStage = "COMPLETE_SNAPSHOT"
	archiveRPCCompleteRestore        archiveRPCStage = "COMPLETE_RESTORE"
	archiveRPCCompletePVCDeletion    archiveRPCStage = "COMPLETE_PVC_DELETION"
	archiveRPCCompleteObjectDeletion archiveRPCStage = "COMPLETE_OBJECT_DELETION"
)

var archiveDiagnosticRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// Наблюдение различает результат рабочего процесса и ответ владельца состояния.
// Текст ошибки, lease, fence и содержимое receipt не попадают в диагностику.
func observeArchiveRPC(ctx context.Context, logger *slog.Logger, task model.Task, stage archiveRPCStage, err error) {
	if logger == nil {
		return
	}
	logger.InfoContext(ctx, archiveRPCObservationMessage,
		archiveTaskRefAttribute, archivePublicRef(task.TaskRef),
		archiveSessionRefAttribute, archivePublicRef(task.SessionRef),
		archiveTaskKindAttribute, archiveTaskKind(task.Kind),
		archiveContentGenerationAttribute, max(task.ContentGeneration, 0),
		archiveAttemptAttribute, archiveDiagnosticAttempt(task.Attempt),
		archiveRPCStageAttribute, archiveDiagnosticStage(stage),
		archiveRPCCodeAttribute, archiveDiagnosticRPCCode(err))
}

func archivePublicRef(value string) string {
	if archiveDiagnosticRef.MatchString(value) {
		return value
	}
	return archiveDiagnosticUnknown
}

func archiveTaskKind(value string) string {
	switch value {
	case "SNAPSHOT", "RESTORE", "DELETE_PVC", "DELETE_OBJECT":
		return value
	default:
		return archiveDiagnosticUnknown
	}
}

func archiveDiagnosticAttempt(value int32) int32 {
	if value < 1 || value > 5 {
		return 0
	}
	return value
}

func archiveDiagnosticStage(value archiveRPCStage) string {
	switch value {
	case archiveRPCRenew, archiveRPCFail, archiveRPCCompleteSnapshot, archiveRPCCompleteRestore,
		archiveRPCCompletePVCDeletion, archiveRPCCompleteObjectDeletion:
		return string(value)
	default:
		return archiveDiagnosticUnknown
	}
}

func archiveDiagnosticRPCCode(err error) string {
	code := status.Code(err)
	if code > codes.Unauthenticated {
		return archiveDiagnosticUnknown
	}
	return code.String()
}
