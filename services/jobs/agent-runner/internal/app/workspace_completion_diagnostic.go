package app

import (
	"context"
	"errors"
	"log/slog"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

const (
	// workspaceCompletionMessage fixes the warning text shared by completion-check and result-publication failures without inserting private paths or artifact content.
	// workspaceCompletionMessage закрепляет предупреждение для отказов completion-check и result-publication без вставки приватных путей и содержимого артефактов.
	workspaceCompletionMessage = "Runtime workspace completion denied"
	// workspaceCompletionCheck names the closed diagnostic stage for pre-completion workspace checks; arbitrary caller stages are normalized separately.
	// workspaceCompletionCheck задаёт закрытый диагностический этап проверки workspace до завершения; произвольные этапы вызывающего нормализуются отдельно.
	workspaceCompletionCheck = "CHECK_WORKSPACE"
	// workspaceCompletionPublish names the closed diagnostic stage for collecting/publishing completion results, not a distinct execution permission.
	// workspaceCompletionPublish задаёт закрытый диагностический этап сбора/публикации результатов завершения, а не отдельное полномочие исполнения.
	workspaceCompletionPublish = "PUBLISH_RESULT"
	// workspaceCompletionStage is the structured key for the normalized completion substage, preserving which guard failed without serializing raw errors.
	// workspaceCompletionStage — ключ нормализованного подэтапа завершения, сохраняющий место отказа guard без сериализации сырых ошибок.
	workspaceCompletionStage = "stage"
	// workspaceCompletionReason is the structured key whose value is restricted to the four existing workspace denial codes.
	// workspaceCompletionReason — ключ со значением, ограниченным четырьмя прежними кодами отказа workspace.
	workspaceCompletionReason = "reason"
)

// logWorkspaceCompletionDenial emits one warning through the supplied logger with a closed stage and normalized workspace/artifact reason. Nil loggers are ignored and neither original error chains nor paths, workspace bodies or arbitrary reasons are logged.
// logWorkspaceCompletionDenial пишет одно предупреждение через переданный logger с закрытым этапом и нормализованной причиной workspace/artifact. Nil logger пропускается; исходные цепочки ошибок, пути, содержимое workspace и произвольные причины не журналируются.
func logWorkspaceCompletionDenial(ctx context.Context, logger *slog.Logger, stage string, err error) {
	if logger == nil {
		return
	}
	if stage != workspaceCompletionCheck && stage != workspaceCompletionPublish {
		stage = "UNKNOWN"
	}
	reason := workspacepolicy.DenialReason(err)
	var artifactDenial *completionArtifactDenial
	if errors.As(err, &artifactDenial) {
		reason = artifactDenial.reason
	}
	reason = closedWorkspaceReason(reason)
	logger.WarnContext(ctx, workspaceCompletionMessage,
		workspaceCompletionStage, stage, workspaceCompletionReason, reason)
}

// completionArtifactDenial retains only the closed artifact-collection guard reason, intentionally discarding the original error, path, file contents and driver diagnostics.
// completionArtifactDenial сохраняет лишь закрытую причину guard сбора артефактов, намеренно отбрасывая исходную ошибку, путь, содержимое и driver diagnostics.
type completionArtifactDenial struct{ reason string }

// Error returns a fixed artifact-collection denial message; the separately stored closed reason is available only through typed inspection.
// Error возвращает фиксированный отказ сбора артефактов; отдельно сохранённая закрытая причина доступна лишь через типизированную проверку.
func (*completionArtifactDenial) Error() string { return "runtime artifact collection denied" }

// denyCompletionArtifact derives a workspace reason, classifies permission/traversal errno and normalizes the result before constructing a redacted typed denial, without wrapping the original error or treating nil as success.
// denyCompletionArtifact извлекает workspace reason, классифицирует errno доступа/обхода и нормализует результат до создания очищенного типизированного отказа без оборачивания исходной ошибки и без трактовки nil как успеха.
func denyCompletionArtifact(err error) error {
	reason := workspacepolicy.DenialReason(err)
	switch {
	case errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		reason = runtimecontract.RuntimeWorkspaceReadOnly
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EINVAL):
		reason = runtimecontract.RuntimeWorkspacePathOutsideWorkspace
	}
	return &completionArtifactDenial{reason: closedWorkspaceReason(reason)}
}

// closedWorkspaceReason accepts only the four existing workspace failure codes and maps any other text to generic I/O; OK remains a canary success token, not a denial reason.
// closedWorkspaceReason принимает лишь четыре прежних кода отказа workspace, заменяя прочий текст общей I/O; OK остаётся признаком успеха canary, а не причиной отказа.
func closedWorkspaceReason(reason string) string {
	switch reason {
	case runtimecontract.RuntimeWorkspaceReadOnly, runtimecontract.RuntimeWorkspaceQuotaExceeded,
		runtimecontract.RuntimeWorkspacePathOutsideWorkspace, runtimecontract.RuntimeWorkspaceIOError:
		return reason
	default:
		return runtimecontract.RuntimeWorkspaceIOError
	}
}
