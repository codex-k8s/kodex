// Package observability содержит ограниченные business-метрики runtime-controller.
package observability

import (
	"github.com/codex-k8s/matter-codex/libs/go/eventing/postgresinbox"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	operations *prometheus.CounterVec
	capacity   *prometheus.GaugeVec
	failures   *prometheus.CounterVec
	inbox      *prometheus.GaugeVec
	inboxOps   *prometheus.CounterVec
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	operations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "operations_total",
		Help: "Total number of bounded runtime-controller lifecycle operations.",
	}, []string{"operation", "outcome"})
	capacity := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "capacity_state",
		Help: "Current bounded runtime-controller capacity state.",
	}, []string{"state"})
	failures := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "failures_total",
		Help: "Total number of bounded runtime-controller worker failures.",
	}, []string{"worker"})
	inbox := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "inbox_state",
		Help: "Current bounded durable inbox and JetStream consumer state.",
	}, []string{"state"})
	inboxOps := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "inbox_operations_total",
		Help: "Total number of bounded PostgreSQL inbox operations.",
	}, []string{"operation", "outcome"})
	if err := register(operations, capacity, failures, inbox, inboxOps); err != nil {
		return nil, err
	}
	return &Metrics{operations: operations, capacity: capacity, failures: failures, inbox: inbox, inboxOps: inboxOps}, nil
}

func (metrics *Metrics) ObserveInboxOperation(operation, outcome string) {
	validOperation := false
	for _, candidate := range []postgresinbox.Operation{postgresinbox.OperationProcess, postgresinbox.OperationClaim,
		postgresinbox.OperationApply, postgresinbox.OperationCheck, postgresinbox.OperationRenew,
		postgresinbox.OperationRepair, postgresinbox.OperationRecover, postgresinbox.OperationDelivery,
		postgresinbox.OperationList, postgresinbox.OperationCleanup} {
		if operation == string(candidate) {
			validOperation = true
		}
	}
	validOutcome := false
	for _, candidate := range []postgresinbox.Outcome{postgresinbox.OutcomeProcessed, postgresinbox.OutcomeClaimed,
		postgresinbox.OutcomeRenewed, postgresinbox.OutcomeDuplicate, postgresinbox.OutcomeStale,
		postgresinbox.OutcomeGap, postgresinbox.OutcomeBusy, postgresinbox.OutcomeRetry,
		postgresinbox.OutcomeDeadLetter, postgresinbox.OutcomeConflict, postgresinbox.OutcomeRepaired,
		postgresinbox.OutcomeCleaned, postgresinbox.OutcomeReady, postgresinbox.OutcomeListed,
		postgresinbox.OutcomeRecovered, postgresinbox.OutcomeCanceled, postgresinbox.OutcomeError} {
		if outcome == string(candidate) {
			validOutcome = true
		}
	}
	if validOperation && validOutcome {
		metrics.inboxOps.WithLabelValues(operation, outcome).Inc()
	}
}

func (metrics *Metrics) SetInboxSnapshot(pending, ackPending, redelivered, blocked, deadLetter float64) {
	metrics.inbox.Reset()
	for state, value := range map[string]float64{
		"pending": pending, "ack_pending": ackPending, "redelivered": redelivered,
		"blocked": blocked, "dead_letter": deadLetter,
	} {
		metrics.inbox.WithLabelValues(state).Set(value)
	}
}

func (metrics *Metrics) ObserveFailure(worker string) {
	switch worker {
	case "claim_loop", "reconcile_loop", "expiry_loop", "temporary_cleanup_loop",
		"readiness_loop", "event_consume":
		metrics.failures.WithLabelValues(worker).Inc()
	}
}

func (metrics *Metrics) Observe(operation, outcome string) {
	if !validOperation(operation) || !validOutcome(outcome) {
		return
	}
	metrics.operations.WithLabelValues(operation, outcome).Inc()
	if operation == "capacity" {
		metrics.capacity.Reset()
		metrics.capacity.WithLabelValues(outcome).Set(1)
	}
}

func validOperation(value string) bool {
	switch value {
	case "claim", "capacity", "evict", "materialize", "heartbeat", "complete",
		"incident", "archive", "restore", "rehydrate", "cleanup_authorization", "cleanup",
		"idle_eviction", "temporary_cleanup", "event_consume":
		return true
	default:
		return false
	}
}

func validOutcome(value string) bool {
	switch value {
	case "empty", "error", "invalid", "deferred", "admitted", "deleted", "rejected",
		"materialized", "renewed", "terminal", "recorded", "scheduled", "consumed", "recovered":
		return true
	default:
		return false
	}
}
