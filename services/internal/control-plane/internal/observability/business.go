// Package observability содержит ограниченные бизнесовые метрики control-plane.
package observability

import (
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/prometheus/client_golang/prometheus"
)

// BusinessMetrics реализует domain observer без aggregate/tenant labels.
type BusinessMetrics struct {
	mutations           *prometheus.CounterVec
	scheduleMaintenance *prometheus.CounterVec
}

// NewBusinessMetrics регистрирует закрытые kind/action labels.
func NewBusinessMetrics(
	register func(...prometheus.Collector) error,
) (*BusinessMetrics, error) {
	mutations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex",
		Subsystem: "control_plane",
		Name:      "mutations_total",
		Help:      "Total number of durably committed control-plane mutations.",
	}, []string{"kind", "action"})
	scheduleMaintenance := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "control_plane",
		Name: "schedule_maintenance_total",
		Help: "Total number of independently committed schedule maintenance effects.",
	}, []string{"effect"})
	if err := register(mutations, scheduleMaintenance); err != nil {
		return nil, err
	}
	return &BusinessMetrics{mutations: mutations, scheduleMaintenance: scheduleMaintenance}, nil
}

func (metrics *BusinessMetrics) ObserveScheduleMaintenance(effect string) {
	switch effect {
	case "requeue", "dead_letter", "overlap_skip", "quarantine", "blocked_recovery", "repair", "reservation_expired":
		metrics.scheduleMaintenance.WithLabelValues(effect).Inc()
	}
}

// ObserveMutation учитывает только уже проверенные enum/action.
func (metrics *BusinessMetrics) ObserveMutation(kind enum.Kind, action string) {
	if !kind.Valid() {
		return
	}
	switch action {
	case "create", "update", "transition", "delete",
		"enqueue_turn", "claim_turn", "complete_turn",
		"claim_schedules", "resolve_owner_gate",
		"role_definition_create", "role_definition_update", "role_definition_reconcile_git", "role_definition_archive", "role_definition_delete",
		"agent_create", "agent_update", "agent_reconcile_git", "agent_pause", "agent_resume", "agent_enable", "agent_disable",
		"agent_bind_bot", "agent_rebind_bot", "agent_revoke_bot", "agent_archive", "agent_delete",
		"agent_assignment_assign", "agent_assignment_unassign",
		"instruction_set_create", "instruction_set_update", "instruction_set_reconcile_git", "instruction_set_validate",
		"instruction_set_publish", "instruction_set_rollback", "instruction_set_detach",
		"instruction_set_copy", "instruction_set_archive", "instruction_set_delete",
		"provider_reference_register", "provider_reference_refresh", "provider_reference_archive",
		"provider_pool_create", "provider_pool_update", "provider_pool_reconcile_git", "provider_pool_archive", "provider_pool_delete",
		"bind_schedule_configuration",
		"manage_workspace_mapping_bind", "manage_workspace_mapping_relink", "manage_workspace_mapping_unlink",
		"manage_workspace_backup_create", "manage_workspace_backup_cancel", "manage_workspace_backup_retry",
		"manage_workspace_backup_complete", "manage_workspace_backup_fail", "manage_workspace_backup_expire",
		"manage_workspace_restore_create", "manage_workspace_restore_cancel", "manage_workspace_restore_retry",
		"manage_workspace_restore_start", "manage_workspace_restore_complete", "manage_workspace_restore_fail",
		"manage_workspace_restore_expire",
		"manage_runtime_incident_acknowledge", "manage_runtime_incident_retry",
		"manage_runtime_incident_release", "manage_runtime_incident_close":
		metrics.mutations.WithLabelValues(string(kind), action).Inc()
	}
}
