package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Проекция целиком принадлежит CP: HTTP проверяет форму, но не пересчитывает
// доступные агрегаты и не подменяет отсутствие активности metadata timestamp.
func validProjectCard(project *cp.Project) bool {
	if project == nil || project.AgentCount < 0 || project.WorkflowCount < 0 || project.ActiveRunCount < 0 || project.PendingGateCount < 0 || !validOptionalCardTime(project.LastActivityAt) {
		return false
	}
	switch project.IntegrationState {
	case "NONE", "READY", "DEGRADED", "UNKNOWN":
		return true
	default:
		return false
	}
}

func validWorkflowCardSummary(summary *cp.WorkflowCardSummary) bool {
	return summary != nil && summary.StageCount >= 0 && summary.UniqueAgentCount >= 0 && summary.ParallelGroupCount >= 0 && summary.ActiveRunCount >= 0 && summary.PendingGateCount >= 0 && validOptionalCardTime(summary.LastActivityAt)
}

func validOptionalCardTime(value *timestamppb.Timestamp) bool {
	return value == nil || value.CheckValid() == nil
}
