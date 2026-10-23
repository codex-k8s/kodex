// Package observability records bounded business outcomes for backup-controller.
// RU: Package observability учитывает ограниченные бизнес-исходы backup-controller.
package observability

import (
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics groups counters and timestamps for backup, restore, object and retention outcomes.
// RU: Metrics объединяет счётчики и временные метки копирования, восстановления, объектов и хранения.
type Metrics struct {
	backupRuns       *prometheus.CounterVec
	databaseActions  *prometheus.CounterVec
	objectActions    *prometheus.CounterVec
	retentionRuns    *prometheus.CounterVec
	retentionDeleted prometheus.Counter
	restoreRuns      *prometheus.CounterVec
	lastBackup       prometheus.Gauge
	lastRestore      prometheus.Gauge
}

// New registers the controller's bounded collectors in the shared metrics registry.
// RU: New регистрирует ограниченные метрики контроллера в общем реестре.
func New(registry *shared.Metrics) (*Metrics, error) {
	metrics := &Metrics{
		backupRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "backup_runs_total",
			Help: "Total number of completed backup controller runs.",
		}, []string{"outcome"}),
		databaseActions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "database_actions_total",
			Help: "Total number of completed PostgreSQL backup and restore actions.",
		}, []string{"operation", "outcome"}),
		objectActions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "object_actions_total",
			Help: "Total number of completed immutable object actions.",
		}, []string{"operation", "outcome"}),
		retentionRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "retention_runs_total",
			Help: "Total number of completed retention evaluations.",
		}, []string{"outcome"}),
		retentionDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "retention_deleted_backups_total",
			Help: "Total number of backup prefixes deleted by retention.",
		}),
		restoreRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "restore_drills_total",
			Help: "Total number of completed owner-gated restore drills.",
		}, []string{"outcome"}),
		lastBackup: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "last_successful_backup_timestamp_seconds",
			Help: "Unix timestamp of the last independently verified backup.",
		}),
		lastRestore: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: "backup_controller", Name: "last_verified_restore_timestamp_seconds",
			Help: "Unix timestamp of the last successful restore drill.",
		}),
	}
	if err := registry.Register(metrics.backupRuns, metrics.databaseActions, metrics.objectActions,
		metrics.retentionRuns, metrics.retentionDeleted, metrics.restoreRuns, metrics.lastBackup, metrics.lastRestore); err != nil {
		return nil, err
	}
	return metrics, nil
}

// BackupFinished counts one completed backup cycle under a closed outcome label.
// RU: BackupFinished учитывает завершённый цикл копирования с меткой исхода из закрытого набора.
func (metrics *Metrics) BackupFinished(outcome string) {
	metrics.backupRuns.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

// DatabaseCompleted counts a bounded PostgreSQL operation and its closed outcome.
// RU: DatabaseCompleted учитывает ограниченную операцию PostgreSQL и её закрытый исход.
func (metrics *Metrics) DatabaseCompleted(operation, outcome string) {
	metrics.databaseActions.WithLabelValues(normalizeOperation(operation), normalizeOutcome(outcome)).Inc()
}

// ObjectCompleted counts an immutable-object action using closed operation and outcome labels.
// RU: ObjectCompleted учитывает действие с неизменяемым объектом по закрытым меткам операции и исхода.
func (metrics *Metrics) ObjectCompleted(operation, outcome string) {
	metrics.objectActions.WithLabelValues(normalizeOperation(operation), normalizeOutcome(outcome)).Inc()
}

// RetentionFinished counts one policy evaluation and separately records deleted backup prefixes.
// RU: RetentionFinished учитывает оценку политики и отдельно число удалённых префиксов копий.
func (metrics *Metrics) RetentionFinished(outcome string, deleted int) {
	metrics.retentionRuns.WithLabelValues(normalizeRetentionOutcome(outcome)).Inc()
	if deleted > 0 {
		metrics.retentionDeleted.Add(float64(deleted))
	}
}

// RestoreFinished counts one completed owner-gated restore drill under a closed outcome.
// RU: RestoreFinished учитывает завершённое подтверждённое пробное восстановление с закрытым исходом.
func (metrics *Metrics) RestoreFinished(outcome string) {
	metrics.restoreRuns.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

// SetLastSuccessfulBackup stores the Unix timestamp of the most recent verified backup.
// RU: SetLastSuccessfulBackup сохраняет Unix-время последней проверенной копии.
func (metrics *Metrics) SetLastSuccessfulBackup(value time.Time) {
	metrics.lastBackup.Set(float64(value.Unix()))
}

// SetLastVerifiedRestore stores the Unix timestamp of the last successful restore drill.
// RU: SetLastVerifiedRestore сохраняет Unix-время последнего успешного пробного восстановления.
func (metrics *Metrics) SetLastVerifiedRestore(value time.Time) {
	metrics.lastRestore.Set(float64(value.Unix()))
}

// normalizeOperation maps arbitrary input to a fixed backup-object operation label set.
// RU: normalizeOperation отображает произвольный ввод в фиксированный набор меток операции с копией.
func normalizeOperation(value string) string {
	switch value {
	case "backup", "restore", "verify", "delete":
		return value
	default:
		return "unknown"
	}
}

// normalizeOutcome maps arbitrary input to success, error, skipped or unknown.
// RU: normalizeOutcome отображает произвольный ввод в success, error, skipped либо unknown.
func normalizeOutcome(value string) string {
	switch value {
	case "success", "error", "skipped":
		return value
	default:
		return "unknown"
	}
}

// normalizeRetentionOutcome preserves the protected outcome in addition to ordinary closed outcomes.
// RU: normalizeRetentionOutcome сохраняет исход protected наряду с обычными закрытыми исходами.
func normalizeRetentionOutcome(value string) string {
	if value == "protected" {
		return value
	}
	return normalizeOutcome(value)
}
