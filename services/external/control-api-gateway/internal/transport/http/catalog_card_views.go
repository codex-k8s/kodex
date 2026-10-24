package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// validProjectCard checks nonnegative owner-provided counts, optional activity time, trash/purge
// timestamp pairing and the integration-state allowlist. It never recalculates visible counts or
// substitutes metadata timestamps for absent activity.
// validProjectCard проверяет неотрицательные счётчики владельца, необязательное время активности,
// пару времён корзины/удаления и закрытый набор integration state. Видимые счётчики не
// пересчитываются, а отсутствие активности не заменяется временем метаданных.
func validProjectCard(project *cp.Project) bool {
	if project == nil || project.AgentCount < 0 || project.WorkflowCount < 0 || project.ActiveRunCount < 0 || project.PendingGateCount < 0 || !validOptionalCardTime(project.LastActivityAt) {
		return false
	}
	switch project.Lifecycle {
	case cp.EntityLifecycle_ENTITY_LIFECYCLE_TRASHED, cp.EntityLifecycle_ENTITY_LIFECYCLE_PURGE_PENDING:
		if project.DeletedAt == nil || project.PurgeAfter == nil || project.DeletedAt.CheckValid() != nil ||
			project.PurgeAfter.CheckValid() != nil || !project.PurgeAfter.AsTime().After(project.DeletedAt.AsTime()) {
			return false
		}
	default:
		if project.DeletedAt != nil || project.PurgeAfter != nil {
			return false
		}
	}
	switch project.IntegrationState {
	case "NONE", "READY", "DEGRADED", "UNKNOWN":
		return true
	default:
		return false
	}
}

// validWorkflowCardSummary accepts only a present workflow summary with nonnegative
// stage/agent/group/run/gate counts and a valid optional activity timestamp. Authorization and
// aggregation remain with CP.
// validWorkflowCardSummary допускает только имеющуюся сводку workflow с неотрицательными
// счётчиками этапов, агентов, групп, запусков и шлюзов и корректным необязательным временем
// активности. Авторизация и агрегация остаются у CP.
func validWorkflowCardSummary(summary *cp.WorkflowCardSummary) bool {
	return summary != nil && summary.StageCount >= 0 && summary.UniqueAgentCount >= 0 && summary.ParallelGroupCount >= 0 && summary.ActiveRunCount >= 0 && summary.PendingGateCount >= 0 && validOptionalCardTime(summary.LastActivityAt)
}

// validOptionalCardTime allows an absent activity timestamp and validates protobuf bounds when
// present, preserving the distinction between no activity and a recorded time.
// validOptionalCardTime разрешает отсутствие времени активности и проверяет границы protobuf при
// его наличии, сохраняя различие между отсутствием активности и записанным временем.
func validOptionalCardTime(value *timestamppb.Timestamp) bool {
	return value == nil || value.CheckValid() == nil
}
