// Package recovery восстанавливает разрыв между Kubernetes effect и terminal
// состоянием runtime-secret operation в control-plane.
package recovery

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/prometheus/client_golang/prometheus"
)

type Owner interface {
	Recover(context.Context, string, *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error)
}

type Store interface {
	ListManaged(context.Context) ([]kubernetesstore.Materialization, error)
	ReadbackExact(context.Context, kubernetesstore.Materialization) (kubernetesstore.Materialization, error)
	DeleteExact(context.Context, kubernetesstore.Materialization) error
}

// RecoveryWork является внутренней проекцией просроченной CLAIMED operation.
// Она не зависит от generated Proto и позволяет подключить owner RPC без
// переноса transport-типов в recovery policy.
type RecoveryWork struct {
	OperationRef          string
	Kind                  controlplanev1.RuntimeSecretOperationKind
	ClaimantID            string
	ClaimGeneration       int64
	Namespace             string
	SecretRef             string
	TargetRevision        int64
	SecretKey             string
	ExpectedContentSHA256 string
}

type RecoveryWorkOwner interface {
	ListRecoveryWork(context.Context) ([]RecoveryWork, error)
	FailExpiredClaim(context.Context, string, string, int64, controlplanev1.RuntimeSecretFailureCode) error
}

type RecoveryWorkStore interface {
	LookupExpectedEffect(context.Context, kubernetesstore.MaterializationEffect) (kubernetesstore.Materialization, error)
}

type healthSnapshot struct {
	ready bool
}

type Reconciler struct {
	owner     Owner
	store     Store
	interval  time.Duration
	timeout   time.Duration
	logger    *slog.Logger
	metrics   *Metrics
	health    atomic.Pointer[healthSnapshot]
	work      RecoveryWorkOwner
	workStore RecoveryWorkStore
	namespace string
}

// EnableExpiredClaimRecovery подключает owner-side список просроченных claim.
// Вызов выполняется один раз composition root до startup barrier и Worker.
func (reconciler *Reconciler) EnableExpiredClaimRecovery(owner RecoveryWorkOwner, store RecoveryWorkStore, namespace string) error {
	if owner == nil || store == nil || namespace != "kodex-runtime" {
		return errors.New("runtime secret expired claim recovery configuration is invalid")
	}
	reconciler.work, reconciler.workStore, reconciler.namespace = owner, store, namespace
	return nil
}

func New(owner Owner, store Store, interval, timeout time.Duration, logger *slog.Logger, metrics *Metrics) (*Reconciler, error) {
	if owner == nil || store == nil || interval < time.Second || timeout < time.Second || logger == nil || metrics == nil {
		return nil, errors.New("runtime secret reconciler configuration is invalid")
	}
	reconciler := &Reconciler{owner: owner, store: store, interval: interval, timeout: timeout, logger: logger, metrics: metrics}
	reconciler.health.Store(&healthSnapshot{})
	return reconciler, nil
}

func (reconciler *Reconciler) Check(context.Context) error {
	if snapshot := reconciler.health.Load(); snapshot != nil && snapshot.ready {
		return nil
	}
	return errors.New("runtime secret recovery is not ready")
}

// ReconcileOnce обрабатывает весь bounded snapshot и не прекращает проход
// после ошибки одного объекта. Возвращаемые ошибки не содержат identifiers.
func (reconciler *Reconciler) ReconcileOnce(ctx context.Context) error {
	failures, joined := reconciler.reconcileExpiredClaims(ctx)
	items, err := reconciler.store.ListManaged(ctx)
	if err != nil {
		failures++
		reconciler.metrics.ObserveError("list")
		joined = errors.Join(joined, errors.New("list runtime secret recovery candidates"))
		items = nil
	}
	for _, item := range items {
		exact, readErr := reconciler.store.ReadbackExact(ctx, item)
		if errors.Is(readErr, kubernetesstore.ErrMaterializationNotFound) {
			reconciler.metrics.ObserveAction("not_found")
			continue
		}
		if readErr != nil {
			failures++
			reconciler.metrics.ObserveError("readback")
			joined = errors.Join(joined, errors.New("read back runtime secret recovery candidate"))
			continue
		}
		decision, decisionErr := reconciler.owner.Recover(ctx, exact.OperationRef, castMaterialization(exact))
		if decisionErr != nil {
			failures++
			reconciler.metrics.ObserveError("decision")
			joined = errors.Join(joined, errors.New("resolve runtime secret recovery decision"))
			continue
		}
		if decision.GetOperationState() == controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_UNSPECIFIED {
			failures++
			reconciler.metrics.ObserveError("protocol")
			joined = errors.Join(joined, errors.New("runtime secret recovery decision is incomplete"))
			continue
		}
		switch decision.GetAction() {
		case controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP:
			reconciler.metrics.ObserveAction("keep")
		case controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE:
			if deleteErr := reconciler.store.DeleteExact(ctx, exact); deleteErr != nil {
				failures++
				reconciler.metrics.ObserveError("delete")
				joined = errors.Join(joined, errors.New("delete runtime secret recovery candidate"))
				continue
			}
			reconciler.metrics.ObserveAction("delete")
		default:
			failures++
			reconciler.metrics.ObserveError("protocol")
			joined = errors.Join(joined, errors.New("runtime secret recovery action is invalid"))
		}
	}
	reconciler.metrics.SetBacklog(failures)
	if joined != nil {
		reconciler.metrics.ObserveRun("error")
		reconciler.setHealth(false)
		return joined
	}
	reconciler.metrics.ObserveRun("success")
	reconciler.setHealth(true)
	return nil
}

func (reconciler *Reconciler) reconcileExpiredClaims(ctx context.Context) (int, error) {
	if reconciler.work == nil || reconciler.workStore == nil {
		return 0, nil
	}
	items, err := reconciler.work.ListRecoveryWork(ctx)
	if err != nil {
		reconciler.metrics.ObserveError("work_list")
		return 1, errors.New("list expired runtime secret claims")
	}
	failures := 0
	var joined error
	for _, item := range items {
		failureCode, outcome, recoveryErr := reconciler.reconcileExpiredClaim(ctx, item)
		if recoveryErr != nil {
			failures++
			reconciler.metrics.ObserveError(outcome)
			joined = errors.Join(joined, recoveryErr)
			continue
		}
		if failureCode == controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED {
			reconciler.metrics.ObserveAction(outcome)
			continue
		}
		if err := reconciler.work.FailExpiredClaim(ctx, item.OperationRef, item.ClaimantID, item.ClaimGeneration, failureCode); err != nil {
			failures++
			reconciler.metrics.ObserveError("work_fail")
			joined = errors.Join(joined, errors.New("fail expired runtime secret claim"))
			continue
		}
		reconciler.metrics.ObserveAction(outcome)
		if failureCode == controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT {
			failures++
			reconciler.metrics.ObserveError("work_conflict")
			joined = errors.Join(joined, errors.New("runtime secret recovery materialization requires manual cleanup"))
		}
	}
	return failures, joined
}

func (reconciler *Reconciler) reconcileExpiredClaim(
	ctx context.Context,
	item RecoveryWork,
) (controlplanev1.RuntimeSecretFailureCode, string, error) {
	if item.OperationRef == "" || item.ClaimantID == "" || item.ClaimGeneration < 1 || item.Namespace != reconciler.namespace {
		return 0, "work_protocol", errors.New("expired runtime secret claim is invalid")
	}
	switch item.Kind {
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE:
		effect := kubernetesstore.MaterializationEffect{
			OperationRef: item.OperationRef, ClaimGeneration: item.ClaimGeneration,
			SecretRef: item.SecretRef, Key: item.SecretKey, Revision: item.TargetRevision,
			ContentSHA256: item.ExpectedContentSHA256,
		}
		_, err := reconciler.workStore.LookupExpectedEffect(ctx, effect)
		switch {
		case err == nil:
			return 0, "effect_present", nil
		case errors.Is(err, kubernetesstore.ErrMaterializationNotFound):
			return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED, "claim_failed", nil
		case errors.Is(err, kubernetesstore.ErrMaterializationConflict):
			return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT, "claim_conflict", nil
		case errors.Is(err, kubernetesstore.ErrMaterializationInvalid):
			return 0, "work_protocol", errors.New("expired runtime secret claim effect is invalid")
		default:
			return 0, "work_lookup", errors.New("look up expired runtime secret claim effect")
		}
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE:
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED, "claim_failed", nil
	default:
		return 0, "work_protocol", errors.New("expired runtime secret claim kind is invalid")
	}
}

func (reconciler *Reconciler) Worker() serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(reconciler.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				run, cancel := context.WithTimeout(ctx, reconciler.timeout)
				_ = reconciler.ReconcileOnce(run)
				cancel()
			}
		}
	}
}

func (reconciler *Reconciler) setHealth(ready bool) {
	previous := reconciler.health.Swap(&healthSnapshot{ready: ready})
	if previous == nil || previous.ready == ready {
		return
	}
	if ready {
		reconciler.logger.Info("runtime secret recovery restored")
	} else {
		reconciler.logger.Warn("runtime secret recovery failed", "error_class", "reconciliation")
	}
}

func castMaterialization(value kubernetesstore.Materialization) *controlplanev1.RuntimeSecretMaterialization {
	return &controlplanev1.RuntimeSecretMaterialization{
		Namespace: value.Namespace, SecretName: value.Name, SecretKey: value.Key,
		SecretUid: value.UID, SecretResourceVersion: value.ResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

type Metrics struct {
	runs    *prometheus.CounterVec
	actions *prometheus.CounterVec
	errors  *prometheus.CounterVec
	backlog prometheus.Gauge
}

func NewMetrics() *Metrics {
	return &Metrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "secret_broker", Name: "recovery_runs_total",
			Help: "Total number of completed runtime secret recovery scans.",
		}, []string{"outcome"}),
		actions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "secret_broker", Name: "recovery_actions_total",
			Help: "Total number of bounded runtime secret recovery actions.",
		}, []string{"action"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "secret_broker", Name: "recovery_errors_total",
			Help: "Total number of runtime secret recovery errors by bounded stage.",
		}, []string{"stage"}),
		backlog: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: "secret_broker", Name: "recovery_backlog",
			Help: "Number of runtime secret materializations left unresolved by the last scan.",
		}),
	}
}

func (metrics *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.runs, metrics.actions, metrics.errors, metrics.backlog}
}

func (metrics *Metrics) ObserveRun(outcome string) {
	if outcome != "success" && outcome != "error" {
		outcome = "error"
	}
	metrics.runs.WithLabelValues(outcome).Inc()
}

func (metrics *Metrics) ObserveAction(action string) {
	switch action {
	case "keep", "delete", "not_found", "effect_present", "claim_failed", "claim_conflict":
	default:
		action = "not_found"
	}
	metrics.actions.WithLabelValues(action).Inc()
}

func (metrics *Metrics) ObserveError(stage string) {
	switch stage {
	case "list", "readback", "decision", "delete", "protocol", "work_list", "work_lookup", "work_fail", "work_conflict", "work_protocol":
	default:
		stage = "protocol"
	}
	metrics.errors.WithLabelValues(stage).Inc()
}

func (metrics *Metrics) SetBacklog(value int) {
	if value < 0 {
		value = 0
	}
	metrics.backlog.Set(float64(value))
}
