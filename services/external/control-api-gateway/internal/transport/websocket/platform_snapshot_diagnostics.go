package websockettransport

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	platformSnapshotDiagnosticKindKey = "kind"
	platformSnapshotDiagnosticStage   = "PLATFORM_SNAPSHOT_READ"
	platformSnapshotDiagnosticUnknown = "UNKNOWN"
	platformSnapshotReadStageKey      = "read_stage"
	platformSnapshotAttemptKey        = "read_attempt"
	platformSnapshotAssistantPageKey  = "assistant_page_size"
)

type platformSnapshotReadStage string

const (
	platformSnapshotAssistantGetStage     platformSnapshotReadStage = "SYSTEM_ASSISTANT_GET"
	platformSnapshotConversationListStage platformSnapshotReadStage = "ASSISTANT_CONVERSATIONS_LIST"
	platformSnapshotBootstrapGetStage     platformSnapshotReadStage = "BOOTSTRAP_STATE_GET"
	platformSnapshotAssistantProjectStage platformSnapshotReadStage = "SYSTEM_ASSISTANT_PROJECTION"
	platformSnapshotRunsListStage         platformSnapshotReadStage = "RUNS_LIST"
	platformSnapshotOwnerGatesListStage   platformSnapshotReadStage = "OWNER_GATES_LIST"
	platformSnapshotOverviewGetStage      platformSnapshotReadStage = "OVERVIEW_GET"
	platformSnapshotRunProjectStage       platformSnapshotReadStage = "RUN_PROJECTION"
	platformSnapshotStageFailureMessage                             = "platform snapshot stage failed"
)

// Этап не содержит input; исходная ошибка сохраняется только для прежнего mapping.
type platformSnapshotStageError struct {
	stage platformSnapshotReadStage
	err   error
}

func (err *platformSnapshotStageError) Error() string { return platformSnapshotStageFailureMessage }
func (err *platformSnapshotStageError) Unwrap() error { return err.err }

func withPlatformSnapshotReadStage(stage platformSnapshotReadStage, err error) error {
	if err == nil {
		return nil
	}
	return &platformSnapshotStageError{stage: stage, err: err}
}

func platformSnapshotErrorStage(kind string, err error) string {
	var staged *platformSnapshotStageError
	if !errors.As(err, &staged) {
		return platformSnapshotDiagnosticUnknown
	}
	switch kind {
	case "SYSTEM_ASSISTANT":
		switch staged.stage {
		case platformSnapshotAssistantGetStage, platformSnapshotConversationListStage,
			platformSnapshotBootstrapGetStage, platformSnapshotAssistantProjectStage:
			return string(staged.stage)
		}
	case "RUN":
		switch staged.stage {
		case platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage,
			platformSnapshotOverviewGetStage, platformSnapshotRunProjectStage:
			return string(staged.stage)
		}
	}
	return platformSnapshotDiagnosticUnknown
}

// Единая запись отказа содержит только закрытые диагностические значения.
func observePlatformSnapshotReadFailure(ctx context.Context, started time.Time, parentBudget int64, kind string, err error, attempt int, pageSize int32) {
	if err == nil {
		return
	}
	if !slices.Contains(platformBootstrapKinds, kind) {
		kind = platformSnapshotDiagnosticUnknown
	}
	code := status.Code(err)
	if code < codes.OK || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	attempt, pageSize = platformSnapshotDiagnosticPage(kind, attempt, pageSize)
	slog.ErrorContext(ctx, platformSnapshotReadFailure,
		platformSnapshotDiagnosticKindKey, kind,
		platformSnapshotReadStageKey, platformSnapshotErrorStage(kind, err),
		platformSnapshotAttemptKey, attempt,
		platformSnapshotAssistantPageKey, pageSize,
		runSnapshotDiagnosticErrorClassKey, "dependency",
		runSnapshotDiagnosticStageKey, platformSnapshotDiagnosticStage,
		runSnapshotDiagnosticGRPCCodeKey, code.String(),
		runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds(),
		runSnapshotDiagnosticParentBudgetKey, parentBudget,
		runSnapshotDiagnosticParentRemainingKey, runSnapshotParentBudget(ctx, time.Now()),
		runSnapshotDiagnosticParentStateKey, runSnapshotContextState(ctx))
}

// Диагностика допускает только существующую конечную лестницу целых страниц.
func platformSnapshotDiagnosticPage(kind string, attempt int, pageSize int32) (int, int32) {
	if kind == "SYSTEM_ASSISTANT" {
		for number, size := 1, int32(platformSnapshotPageSize); ; number, size = number+1, max(1, size/2) {
			if attempt >= 1 && attempt <= number && size == pageSize {
				return attempt, size
			}
			if size == 1 {
				break
			}
		}
	}
	return 0, 0
}
