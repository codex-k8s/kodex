// Package recovery reconciles observed Kubernetes effects with authoritative immediate-operation and
// projection decisions; draft retirement stays on its separate owner-controlled route.
// Package recovery согласует observed Kubernetes effects с authoritative immediate-operation и projection
// decisions; draft retirement остаётся в отдельном owner-controlled маршруте.
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
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Owner supplies authoritative immediate-operation recovery decisions for observed effects; this port does
// not cover draft retirement or grant issuance.
// Owner предоставляет authoritative recovery decisions immediate operations для observed effects; draft
// retirement и выдача grants через этот port не выполняются.
type Owner interface {
	// Recover resolves one exact operation/materialization under owner state; callers inspect the response
	// instead of inferring deletion authority from missing metadata.
	// Recover разрешает одну exact operation/materialization по owner state; caller проверяет response вместо
	// вывода deletion authority из отсутствующих metadata.
	Recover(context.Context, string, *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error)
}

// Store exposes managed-effect inventory, exact readback and preconditioned deletion without choosing the
// effect's authoritative owner.
// Store предоставляет managed-effect inventory, exact readback и preconditioned deletion без выбора
// authoritative owner эффекта.
type Store interface {
	// ListManaged returns the adapter's managed materialization snapshot; the reconciler still dispatches only
	// explicit immediate work kind and does not infer source from reference prefixes.
	// ListManaged возвращает managed materialization snapshot adapter; reconciler выбирает лишь explicit
	// immediate work kind, не выводя source из reference prefixes.
	ListManaged(context.Context) ([]kubernetesstore.Materialization, error)
	// ReadbackExact verifies the observed effect's exact object pins before owner recovery rather than
	// accepting a replacement under the same name.
	// ReadbackExact сверяет exact object pins observed effect до owner recovery вместо принятия replacement с
	// тем же именем.
	ReadbackExact(context.Context, kubernetesstore.Materialization) (kubernetesstore.Materialization, error)
	// DeleteExact removes only the verified materialization under exact storage preconditions after an
	// explicit owner DELETE decision.
	// DeleteExact удаляет лишь проверенную materialization с exact storage preconditions после явного owner
	// DELETE decision.
	DeleteExact(context.Context, kubernetesstore.Materialization) error
}

// RecoveryWork copies owner-listed expired-claim metadata, including the generated kind enum, without
// plaintext or a lease timestamp; actual expired eligibility is owned by the listing service.
// RecoveryWork копирует owner-listed expired-claim metadata, включая generated kind enum, без plaintext и
// lease timestamp; фактической expired eligibility владеет listing service.
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

// RecoveryWorkOwner separates listing expired claims from failing their original claimant/generation, so
// replacement pods do not acquire the old claim identity.
// RecoveryWorkOwner разделяет listing expired claims и Fail с original claimant/generation, не выдавая
// replacement Pod identity прежнего claim.
type RecoveryWorkOwner interface {
	// ListRecoveryWork returns owner-selected bounded recovery work without itself completing, reissuing or
	// consuming a new command grant.
	// ListRecoveryWork возвращает owner-selected bounded recovery work без completion, переиздания и
	// потребления нового command grant.
	ListRecoveryWork(context.Context) ([]RecoveryWork, error)
	// FailExpiredClaim reports an exact original operation/claimant/generation/code; success is an owner
	// receipt, not permission to delete a conflicting object.
	// FailExpiredClaim сообщает exact original operation/claimant/generation/code; успех является owner
	// receipt, не разрешением удалить conflicting object.
	FailExpiredClaim(context.Context, string, string, int64, controlplanev1.RuntimeSecretFailureCode) error
}

// RecoveryWorkStore observes whether a claimed immediate effect exists without repeating create or
// broadening lookup to unrelated objects.
// RecoveryWorkStore наблюдает наличие claimed immediate effect без повторного create и расширения lookup
// на посторонние объекты.
type RecoveryWorkStore interface {
	// LookupExpectedEffect resolves exact effect metadata, distinguishing
	// absent/conflicting/invalid/unavailable outcomes while returning no plaintext.
	// LookupExpectedEffect разрешает exact effect metadata, различая absent/conflicting/invalid/unavailable
	// outcomes без возврата plaintext.
	LookupExpectedEffect(context.Context, kubernetesstore.MaterializationEffect) (kubernetesstore.Materialization, error)
}

// CredentialProjectionOwner decides current eligibility of pinned runtime projections; local age or a
// caller's arbitrary flag is not the source of deletion authority.
// CredentialProjectionOwner определяет текущую eligibility закреплённых runtime projections; локальный
// возраст и произвольный caller flag не являются deletion authority.
type CredentialProjectionOwner interface {
	// ValidateRuntimeCredentialProjection checks the exact authority/execution/credential snapshot and returns
	// false for an invalid projection; a transport error must not be treated as false.
	// ValidateRuntimeCredentialProjection проверяет exact authority/execution/credential snapshot и возвращает
	// false для invalid projection; transport error нельзя приравнивать к false.
	ValidateRuntimeCredentialProjection(context.Context, *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error)
}

// CredentialProjectionStore inventories and deletes exact managed projections without granting access to
// their source credentials or changing source Secrets.
// CredentialProjectionStore составляет inventory и удаляет exact managed projections без доступа к их
// source credentials и изменения source Secrets.
type CredentialProjectionStore interface {
	// ListRuntimeCredentialProjections returns exact projection descriptors/manifests for owner validation,
	// not plaintext values.
	// ListRuntimeCredentialProjections возвращает exact projection descriptors/manifests для owner validation,
	// не plaintext values.
	ListRuntimeCredentialProjections(context.Context) ([]kubernetesstore.CredentialProjection, error)
	// DeleteRuntimeCredentialProjection removes only the supplied observed projection using adapter-enforced
	// object identity, not the underlying provider/runtime sources.
	// DeleteRuntimeCredentialProjection удаляет лишь переданную observed projection с adapter-enforced object
	// identity, не нижележащие provider/runtime sources.
	DeleteRuntimeCredentialProjection(context.Context, kubernetesstore.CredentialProjection) error
}

// healthSnapshot is the immutable atomic publication of the last overall scan result, not a live
// dependency probe or authority record.
// healthSnapshot — immutable atomic публикация результата последнего общего scan, не live dependency probe
// и не authority record.
type healthSnapshot struct {
	ready bool
}

// Reconciler combines immediate effects, optional expired claims and optional projection recovery under
// owner decisions, fixed metrics and atomic last-scan health; trusted composition installs optional ports
// before workers run.
// Reconciler согласует immediate effects, optional expired claims и optional projection recovery по owner
// decisions с фиксированными metrics и atomic last-scan health; trusted composition устанавливает optional
// ports до запуска workers.
type Reconciler struct {
	owner           Owner
	store           Store
	interval        time.Duration
	timeout         time.Duration
	logger          *slog.Logger
	metrics         *Metrics
	health          atomic.Pointer[healthSnapshot]
	work            RecoveryWorkOwner
	workStore       RecoveryWorkStore
	namespace       string
	projectionOwner CredentialProjectionOwner
	projectionStore CredentialProjectionStore
}

// EnableExpiredClaimRecovery installs nonnil owner/store and the exact runtime namespace. Composition must
// call it before workers: it does not lock, enforce one-time use or authorize live reconfiguration.
// EnableExpiredClaimRecovery устанавливает nonnil owner/store и exact runtime namespace. Composition
// вызывает до workers: метод не блокирует, не обеспечивает однократность и не разрешает live
// reconfiguration.
func (reconciler *Reconciler) EnableExpiredClaimRecovery(owner RecoveryWorkOwner, store RecoveryWorkStore, namespace string) error {
	if owner == nil || store == nil || namespace != "kodex-runtime" {
		return errors.New("runtime secret expired claim recovery configuration is invalid")
	}
	reconciler.work, reconciler.workStore, reconciler.namespace = owner, store, namespace
	return nil
}

// EnableCredentialProjectionRecovery installs nonnil projection owner/store for later validation/cleanup
// without starting work or probing readiness; mutation belongs to pre-worker composition.
// EnableCredentialProjectionRecovery устанавливает nonnil projection owner/store для последующих
// validation/cleanup без запуска работы и readiness probe; mutation относится к pre-worker composition.
func (reconciler *Reconciler) EnableCredentialProjectionRecovery(owner CredentialProjectionOwner, store CredentialProjectionStore) error {
	if owner == nil || store == nil {
		return errors.New("credential projection recovery configuration is invalid")
	}
	reconciler.projectionOwner, reconciler.projectionStore = owner, store
	return nil
}

// New requires nonnil ports/logger/metrics and interval/timeout of at least one second, initializes
// unready health and starts no worker; it neither detects typed nils nor imposes upper timing limits.
// New требует nonnil ports/logger/metrics и interval/timeout не меньше секунды, задаёт unready health без
// запуска worker; typed nil и верхние timing limits не проверяет.
func New(owner Owner, store Store, interval, timeout time.Duration, logger *slog.Logger, metrics *Metrics) (*Reconciler, error) {
	if owner == nil || store == nil || interval < time.Second || timeout < time.Second || logger == nil || metrics == nil {
		return nil, errors.New("runtime secret reconciler configuration is invalid")
	}
	reconciler := &Reconciler{owner: owner, store: store, interval: interval, timeout: timeout, logger: logger, metrics: metrics}
	reconciler.health.Store(&healthSnapshot{})
	return reconciler, nil
}

// Check reads only the atomic last-scan readiness and ignores its context; a successful prior scan is not
// an active owner or Kubernetes health check.
// Check читает лишь atomic last-scan readiness и игнорирует context; прежний успешный scan не является
// текущей проверкой owner либо Kubernetes.
func (reconciler *Reconciler) Check(context.Context) error {
	if snapshot := reconciler.health.Load(); snapshot != nil && snapshot.ready {
		return nil
	}
	return errors.New("runtime secret recovery is not ready")
}

// ReconcileOnce processes expired claims, projections and then managed immediate effects, joining bounded
// diagnostics and continuing after per-item failures. It skips DRAFT, rejects unknown source and requires
// a specified owner state plus known action, not full local terminal-state validation; port inventory
// limits remain adapter-owned.
// ReconcileOnce обрабатывает expired claims, projections, затем managed immediate effects, объединяя
// bounded diagnostics и продолжая после per-item failures. Пропускает DRAFT, отклоняет unknown source и
// требует specified owner state с known action, не полную local terminal-state validation; inventory
// limits принадлежат adapter.
func (reconciler *Reconciler) ReconcileOnce(ctx context.Context) error {
	failures, joined := reconciler.reconcileExpiredClaims(ctx)
	projectionFailures, projectionErr := reconciler.reconcileCredentialProjections(ctx)
	failures += projectionFailures
	joined = errors.Join(joined, projectionErr)
	items, err := reconciler.store.ListManaged(ctx)
	if err != nil {
		failures++
		reconciler.metrics.ObserveError("list")
		joined = errors.Join(joined, errors.New("list runtime secret recovery candidates"))
		items = nil
	}
	for _, item := range items {
		if item.WorkKind == kubernetesstore.WorkKindDraft {
			// Данный маршрут не имеет draft claimant fence и не может заменять
			// отдельный owner-controlled draft recovery/retirement worker.
			continue
		}
		if item.WorkKind != kubernetesstore.WorkKindImmediate {
			failures++
			reconciler.metrics.ObserveError("protocol")
			joined = errors.Join(joined, errors.New("runtime secret recovery work kind is invalid"))
			continue
		}
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

// reconcileCredentialProjections is disabled unless both optional ports are installed; it preserves
// projections on validation error and deletes an exact item only after a false eligibility result,
// continuing with independent items.
// reconcileCredentialProjections отключён без обоих optional ports; сохраняет projections при validation
// error и удаляет exact item лишь после false eligibility result, продолжая независимые элементы.
func (reconciler *Reconciler) reconcileCredentialProjections(ctx context.Context) (int, error) {
	if reconciler.projectionOwner == nil || reconciler.projectionStore == nil {
		return 0, nil
	}
	items, err := reconciler.projectionStore.ListRuntimeCredentialProjections(ctx)
	if err != nil {
		reconciler.metrics.ObserveError("projection_list")
		return 1, errors.New("list runtime credential projections")
	}
	failures := 0
	var joined error
	for _, item := range items {
		valid, validationErr := reconciler.projectionOwner.ValidateRuntimeCredentialProjection(ctx, projectionValidationRequest(item.Manifest))
		if validationErr != nil {
			failures++
			reconciler.metrics.ObserveError("projection_decision")
			joined = errors.Join(joined, errors.New("validate runtime credential projection"))
			continue
		}
		if valid {
			reconciler.metrics.ObserveAction("projection_keep")
			continue
		}
		if err := reconciler.projectionStore.DeleteRuntimeCredentialProjection(ctx, item); err != nil {
			failures++
			reconciler.metrics.ObserveError("projection_delete")
			joined = errors.Join(joined, errors.New("delete invalid runtime credential projection"))
			continue
		}
		reconciler.metrics.ObserveAction("projection_delete")
	}
	return failures, joined
}

// projectionValidationRequest copies manifest authority, execution lineage, provider and runtime-secret
// pins into a new owner request without reading values or establishing fresh authority; timestamp validity
// belongs to validated inputs.
// projectionValidationRequest копирует manifest authority, execution lineage, provider и runtime-secret
// pins в новый owner request без чтения values и создания fresh authority; timestamp validity обеспечивают
// проверенные inputs.
func projectionValidationRequest(manifest kubernetesstore.CredentialProjectionManifest) *controlplanev1.ValidateRuntimeCredentialProjectionRequest {
	request := &controlplanev1.ValidateRuntimeCredentialProjectionRequest{
		Authority: &controlplanev1.CredentialProjectionAuthority{
			ActorId: manifest.Authority.ActorID, TenantId: manifest.Authority.TenantID, ProjectId: manifest.Authority.ProjectID,
			SourceRevision: manifest.Authority.SourceRevision, SourceDigestSha256: manifest.Authority.SourceDigestSHA256,
			RpcProfile: manifest.Authority.RPCProfile, ProofJti: manifest.Authority.ProofJTI, CallerWorkloadId: manifest.Authority.CallerWorkloadID,
			CallerFullMethod: manifest.Authority.CallerFullMethod, CallerCredentialRevision: manifest.Authority.CallerCredentialRevision,
			ExpiresAt: timestamppb.New(manifest.Authority.ExpiresAt),
		},
		WorkloadInstance: manifest.WorkloadInstance, LeaseRef: manifest.LeaseRef, Generation: manifest.Generation,
		RuntimeRevisionRef: manifest.RuntimeRevisionRef, RuntimeRevisionDigest: manifest.RuntimeRevisionDigest,
		SessionRef: manifest.SessionRef, TurnRef: manifest.TurnRef, Attempt: manifest.Attempt, InputDigest: manifest.InputDigest,
		ProviderCredential: &controlplanev1.ProviderCredentialBinding{
			AccountRef: manifest.ProviderCredential.AccountRef, CredentialRevisionRef: manifest.ProviderCredential.CredentialRevisionRef,
			CredentialRevision: manifest.ProviderCredential.CredentialRevision, SecretName: manifest.ProviderCredential.SecretName,
			SecretUid: manifest.ProviderCredential.SecretUID, SecretResourceVersion: manifest.ProviderCredential.SecretResourceVersion,
			ContentSha256: manifest.ProviderCredential.ContentSHA256,
		},
	}
	for _, item := range manifest.RuntimeSecrets {
		request.RuntimeSecrets = append(request.RuntimeSecrets, &controlplanev1.RuntimeSecretDescriptor{
			Name: item.Name, SecretRef: item.SecretRef, Revision: item.Revision, Namespace: item.Namespace,
			SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUid: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSha256: item.ContentSHA256,
		})
	}
	return request
}

// reconcileExpiredClaims lists optional work, resolves each effect and reports any required failure using
// the original fence; an acknowledged materialization conflict remains an unresolved manual-cleanup
// failure without deleting the object.
// reconcileExpiredClaims получает optional work, разрешает каждый effect и сообщает нужный failure с
// original fence; подтверждённый materialization conflict остаётся unresolved manual-cleanup failure без
// удаления объекта.
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

// reconcileExpiredClaim validates original claim shape/namespace, observes CREATE/ROTATE effect presence
// and classifies absence/conflict; REVEAL/REVOKE are failed without replaying their action. It returns a
// proposed failure code, never sends Fail or Delete itself.
// reconcileExpiredClaim проверяет original claim shape/namespace, наблюдает CREATE/ROTATE effect и
// классифицирует absence/conflict; REVEAL/REVOKE завершаются failure без повтора действия. Возвращает
// предлагаемый failure code, сам Fail и Delete не отправляет.
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
			WorkKind:     kubernetesstore.WorkKindImmediate,
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

// Worker returns a caller-owned loop that waits for the first interval tick, bounds each scan with the
// shared lifecycle context and tolerates scan failures; cancellation returns context error, and adapters
// must honor deadlines.
// Worker возвращает caller-owned loop, ожидающий первого interval tick, ограничивающий scan общим
// lifecycle context и продолжающий после scan failures; отмена возвращает context error, deadlines должны
// соблюдать adapters.
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

// setHealth atomically publishes last-scan readiness and logs only changes from a previous initialized
// snapshot, with a fixed failure class and no resource identifiers.
// setHealth атомарно публикует last-scan readiness и логирует лишь изменения прежнего initialized snapshot
// с фиксированным failure class без resource identifiers.
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

// castMaterialization copies exact storage name/key/UID/RV/digest pins to an owner DTO without exposing
// plaintext or performing another lookup.
// castMaterialization копирует exact storage name/key/UID/RV/digest pins в owner DTO без раскрытия
// plaintext и дополнительного lookup.
func castMaterialization(value kubernetesstore.Materialization) *controlplanev1.RuntimeSecretMaterialization {
	return &controlplanev1.RuntimeSecretMaterialization{
		Namespace: value.Namespace, SecretName: value.Name, SecretKey: value.Key,
		SecretUid: value.UID, SecretResourceVersion: value.ResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

// Metrics tracks scan outcomes, normalized actions/stages and last-scan failure count; the backlog gauge
// is not an exact durable queue length or unique-object count.
// Metrics учитывает scan outcomes, нормализованные actions/stages и число failures последнего scan;
// backlog gauge не является точной durable queue length либо unique-object count.
type Metrics struct {
	runs    *prometheus.CounterVec
	actions *prometheus.CounterVec
	errors  *prometheus.CounterVec
	backlog prometheus.Gauge
}

// NewMetrics constructs four unregistered collectors with fixed dimensions and English HELP; public
// observation methods normalize values before creating label series.
// NewMetrics создаёт четыре незарегистрированных collectors с фиксированными dimensions и английским HELP;
// observation methods нормализуют values до создания label series.
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

// Collectors returns a fresh slice referencing the same counters/gauge for registration without cloning
// accumulated observations.
// Collectors возвращает новый slice со ссылками на те же counters/gauge для регистрации без копирования
// накопленных observations.
func (metrics *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.runs, metrics.actions, metrics.errors, metrics.backlog}
}

// ObserveRun allows only success/error and maps every other input to error before incrementing a series.
// ObserveRun допускает лишь success/error и отображает остальные inputs в error до увеличения series.
func (metrics *Metrics) ObserveRun(outcome string) {
	if outcome != "success" && outcome != "error" {
		outcome = "error"
	}
	metrics.runs.WithLabelValues(outcome).Inc()
}

// ObserveAction accepts the closed recovery-action set and maps unknown input to not_found; that fallback
// is cardinality control, not proof an object was absent.
// ObserveAction принимает закрытый recovery-action set и отображает unknown input в not_found; этот
// fallback ограничивает cardinality, не доказывает отсутствие объекта.
func (metrics *Metrics) ObserveAction(action string) {
	switch action {
	case "keep", "delete", "not_found", "effect_present", "claim_failed", "claim_conflict", "projection_keep", "projection_delete":
	default:
		action = "not_found"
	}
	metrics.actions.WithLabelValues(action).Inc()
}

// ObserveError accepts only fixed recovery stages and maps every other string to protocol before
// incrementing the counter.
// ObserveError принимает лишь фиксированные recovery stages и отображает остальные строки в protocol до
// увеличения counter.
func (metrics *Metrics) ObserveError(stage string) {
	switch stage {
	case "list", "readback", "decision", "delete", "protocol", "work_list", "work_lookup", "work_fail", "work_conflict", "work_protocol",
		"projection_list", "projection_decision", "projection_delete":
	default:
		stage = "protocol"
	}
	metrics.errors.WithLabelValues(stage).Inc()
}

// SetBacklog publishes the supplied last-scan failure count as a gauge, clamping negatives to zero without
// counting or reading durable work.
// SetBacklog публикует переданное число failures последнего scan как gauge, заменяя отрицательные значения
// нулём без подсчёта и чтения durable work.
func (metrics *Metrics) SetBacklog(value int) {
	if value < 0 {
		value = 0
	}
	metrics.backlog.Set(float64(value))
}
