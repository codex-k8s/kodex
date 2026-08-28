// Package observability содержит bounded business metrics backup-controller.
package observability

import (
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/prometheus/client_golang/prometheus"
)

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

func (metrics *Metrics) BackupFinished(outcome string) {
	metrics.backupRuns.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) DatabaseCompleted(operation, outcome string) {
	metrics.databaseActions.WithLabelValues(normalizeOperation(operation), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) ObjectCompleted(operation, outcome string) {
	metrics.objectActions.WithLabelValues(normalizeOperation(operation), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) RetentionFinished(outcome string, deleted int) {
	metrics.retentionRuns.WithLabelValues(normalizeRetentionOutcome(outcome)).Inc()
	if deleted > 0 {
		metrics.retentionDeleted.Add(float64(deleted))
	}
}

func (metrics *Metrics) RestoreFinished(outcome string) {
	metrics.restoreRuns.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) SetLastSuccessfulBackup(value time.Time) {
	metrics.lastBackup.Set(float64(value.Unix()))
}

func (metrics *Metrics) SetLastVerifiedRestore(value time.Time) {
	metrics.lastRestore.Set(float64(value.Unix()))
}

func normalizeOperation(value string) string {
	switch value {
	case "backup", "restore", "verify", "delete":
		return value
	default:
		return "unknown"
	}
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "error", "skipped":
		return value
	default:
		return "unknown"
	}
}

func normalizeRetentionOutcome(value string) string {
	if value == "protected" {
		return value
	}
	return normalizeOutcome(value)
}
