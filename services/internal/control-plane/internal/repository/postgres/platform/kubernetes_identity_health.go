package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesHealthMaterial resolves exact purpose pins only for the selected personal revision.
// queryKubernetesHealthMaterial разрешает exact purpose pins только выбранной personal revision.
//
//go:embed sql/kubernetes_health__material.sql
var queryKubernetesHealthMaterial string

// queryKubernetesHealthInsert consumes the current claim into one immutable admission.
// queryKubernetesHealthInsert расходует current claim в один immutable admission.
//
//go:embed sql/kubernetes_health__insert.sql
var queryKubernetesHealthInsert string

// queryKubernetesHealthRead reads original credit and its immutable completion after owner locking.
// queryKubernetesHealthRead читает original credit и immutable completion после owner locking.
//
//go:embed sql/kubernetes_health__read.sql
var queryKubernetesHealthRead string

// queryKubernetesHealthComplete records one exact fact without replacing earlier evidence.
// queryKubernetesHealthComplete сохраняет exact fact без замены прежнего evidence.
//
//go:embed sql/kubernetes_health__complete.sql
var queryKubernetesHealthComplete string

// queryKubernetesHealthClock uses the database clock after dependent locks and before verification.
// queryKubernetesHealthClock использует database clock после dependent locks до verification.
//
//go:embed sql/kubernetes_health__clock.sql
var queryKubernetesHealthClock string

// kubernetesHealthDigest binds every private admission field explicitly despite locator JSON redaction.
// The hash never includes kubeconfig material and does not establish authorization on its own.
// kubernetesHealthDigest явно связывает каждое private admission field вопреки locator JSON redaction.
// Hash не содержит kubeconfig material и сам не устанавливает authorization.
func kubernetesHealthDigest(binding value.ManagedKubernetesBinding, admission entity.KubernetesIdentityHealthAdmission, registryRevision int64) (string, error) {
	raw, err := json.Marshal(struct {
		AttemptRef, InputDigest, LeaseRef, FenceDigest, WorkloadInstance  string
		IdentityRef, Namespace, Name, UID, ResourceVersion, ContentDigest string
		IdentityRevision, RegistryRevision, Generation                    int64
		Target                                                            value.ManagedKubernetesTarget
		ExpiresAt                                                         time.Time
	}{AttemptRef: admission.AttemptRef, InputDigest: binding.InputDigest, LeaseRef: binding.LeaseRef,
		FenceDigest: kubernetesHealthFenceDigest(binding.Fence), WorkloadInstance: binding.WorkloadInstance,
		IdentityRef: admission.Identity.IdentityRef, Namespace: admission.Identity.Namespace,
		Name: admission.Identity.Name, UID: admission.Identity.UID, ResourceVersion: admission.Identity.ResourceVersion,
		ContentDigest: admission.Identity.ContentSHA256, IdentityRevision: admission.Identity.Revision,
		RegistryRevision: registryRevision, Generation: binding.Generation, Target: admission.Target, ExpiresAt: admission.ExpiresAt})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// kubernetesPersonalHealthInputTx rechecks the complete personal test snapshot and the current operator.
// It returns no usable credit and cannot treat a TOKEN definition as a broker identity test.
// kubernetesPersonalHealthInputTx повторно проверяет полный personal test snapshot и current operator.
// Не возвращает usable credit и не считает TOKEN definition broker identity test.
func (repository *Repository) kubernetesPersonalHealthInputTx(ctx context.Context, tx pgx.Tx, organizationID string, job personalCredentialWorkJob, source kubernetesOperatorStored) (personalCredentialTestInput, value.ManagedKubernetesTarget, value.ManagedKubernetesIdentityLocator, string, error) {
	input, err := repository.currentPersonalCredentialTestInput(ctx, tx, organizationID, job)
	if err != nil {
		return input, value.ManagedKubernetesTarget{}, value.ManagedKubernetesIdentityLocator{}, "", err
	}
	identity := input.binding.BrokerIdentity
	delivery, known := input.definition.ConnectionCredentialDelivery()
	if !known || delivery.Kind != "BROKER_IDENTITY" || delivery.BrokerPurpose != "KUBERNETES_TARGET" ||
		input.definition.Spec.Adapter != "KUBERNETES" || input.definition.Spec.HealthCheck.Operation != "kubernetes.identity" ||
		job.snapshot.Kind != entity.IntegrationCredentialBrokerIdentity || identity == nil || !identity.ValidMetadata() ||
		identity.Purpose != entity.IntegrationBrokerIdentityKubernetes || job.snapshot.CredentialRef != "" {
		return input, value.ManagedKubernetesTarget{}, value.ManagedKubernetesIdentityLocator{}, "", errs.ErrForbidden
	}
	var target value.ManagedKubernetesTarget
	for _, entry := range source.registry.Targets {
		if entry.Target.Ref == identity.TargetRef {
			target = entry.Target
			break
		}
	}
	if target.Ref == "" {
		return input, target, value.ManagedKubernetesIdentityLocator{}, "", errs.ErrConflict
	}
	locator := value.ManagedKubernetesIdentityLocator{IdentityRef: identity.Ref, TargetRef: identity.TargetRef, Revision: identity.Revision}
	var identityID string
	err = tx.QueryRow(ctx, queryKubernetesHealthMaterial, pgx.StrictNamedArgs{"organization_id": organizationID,
		"identity_ref": identity.Ref, "binding_id": job.bindingID, "subject_id": job.subjectID,
		"target_ref": identity.TargetRef, "revision": identity.Revision}).Scan(&identityID, &locator.Namespace,
		&locator.Name, &locator.UID, &locator.ResourceVersion, &locator.ContentSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return input, target, locator, "", errs.ErrForbidden
	}
	if err != nil || locator.Validate(source.registry.IdentityNamespace) != nil {
		return input, target, locator, "", errs.ErrUnavailable
	}
	return input, target, locator, identityID, nil
}

// kubernetesHealthFenceDigest retains only the original opaque fence hash in durable private state.
// kubernetesHealthFenceDigest сохраняет только original opaque fence hash в durable private state.
func kubernetesHealthFenceDigest(fence string) string {
	sum := sha256.Sum256([]byte(fence))
	return hex.EncodeToString(sum[:])
}

// kubernetesHealthBindingMatches verifies immutable input, exact instance and the owner lease.
// The public claim carries metadata only; the broker must present all original owner coordinates.
// kubernetesHealthBindingMatches проверяет immutable input, exact instance и owner lease.
// Public claim содержит только metadata; broker предъявляет все original owner coordinates.
func kubernetesHealthBindingMatches(job personalCredentialWorkJob, binding value.ManagedKubernetesBinding, now time.Time) bool {
	raw, err := json.Marshal(job.snapshot)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(raw)
	return binding.Validate() == nil && binding.InvocationRef == "" && binding.ConnectionTestRef == job.ref &&
		binding.InputDigest == hex.EncodeToString(sum[:]) && binding.WorkloadInstance == job.workloadInstance &&
		personalCredentialTestLease(job, command.PersonalIntegrationCredentialTestCompletion{TestRef: job.ref,
			LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation}, now)
}

// BeginManagedKubernetesHealthProbe persists one exact admission before releasing selected material pins.
// The original claim cannot be admitted twice after a lost response, restart or lease replacement.
// BeginManagedKubernetesHealthProbe фиксирует exact admission до выдачи selected material pins.
// Original claim не допускается дважды после lost response, restart либо lease replacement.
func (repository *Repository) BeginManagedKubernetesHealthProbe(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding) (result entity.KubernetesIdentityHealthAdmission, resultErr error) {
	if kubernetesSetupBrokerCaller(principal, "platform.runtime.kubernetes.health.begin") != nil || binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return result, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	source, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	connectionRef, shared, err := locateBrokerIdentityHealthTestTx(ctx, tx, current.organizationID, binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	if shared {
		return repository.beginSharedKubernetesHealthTx(ctx, tx, current, binding, source, connectionRef)
	}
	job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	if job.state != "CLAIMED" || !kubernetesHealthBindingMatches(job, binding, now) {
		return result, errs.ErrForbidden
	}
	_, target, locator, identityID, err := repository.kubernetesPersonalHealthInputTx(ctx, tx, current.organizationID, job, source)
	if err != nil {
		return result, err
	}
	ref, err := newRef("khp")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	result = entity.KubernetesIdentityHealthAdmission{AttemptRef: ref, Target: target, Identity: locator, ExpiresAt: job.expiresAt.UTC()}
	result.SnapshotDigest, err = kubernetesHealthDigest(binding, result, source.registry.Revision)
	if err != nil {
		return result, err
	}
	var id string
	if err := tx.QueryRow(ctx, queryKubernetesHealthInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
		"identity_id": identityID, "registry_revision": source.registry.Revision, "target_ref": target.Ref, "target_revision": target.Revision,
		"snapshot_digest": result.SnapshotDigest, "input_digest": binding.InputDigest, "test_id": job.id}).Scan(&id); err != nil {
		return entity.KubernetesIdentityHealthAdmission{}, errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialTestBegin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.version})
	if err != nil || tag.RowsAffected() != 1 {
		return entity.KubernetesIdentityHealthAdmission{}, errs.ErrConflict
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.kubernetes.health.begin", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_PROBE_ADMITTED") != nil || tx.Commit(ctx) != nil {
		return entity.KubernetesIdentityHealthAdmission{}, errs.ErrUnavailable
	}
	if err := (kubernetesOperationOwner{}).creditError(ctx, result.ExpiresAt); err != nil {
		return entity.KubernetesIdentityHealthAdmission{}, err
	}
	return result, nil
}

// CompleteManagedKubernetesHealthProbe stores one immutable authentication fact for original credit.
// Fresh owner checks govern every new result, including failures. Late facts never restore authority;
// exact terminal replay returns history and cannot substitute another observed subject or outcome.
// CompleteManagedKubernetesHealthProbe сохраняет immutable authentication fact исходного credit.
// Fresh owner checks обязательны для каждого нового result, включая failures. Late facts не
// восстанавливают authority; exact terminal replay не заменяет observed subject либо outcome.
func (repository *Repository) CompleteManagedKubernetesHealthProbe(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.KubernetesIdentityHealthCompletion) (result entity.KubernetesIdentityHealthReceipt, resultErr error) {
	if kubernetesSetupBrokerCaller(principal, "platform.runtime.kubernetes.health.complete") != nil || binding.Validate() != nil || binding.ConnectionTestRef == "" ||
		!managedkubernetes.ValidLocator(input.AttemptRef) || !managedkubernetes.ValidDigest(input.SnapshotDigest) {
		return result, errs.ErrForbidden
	}
	if input.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
		if !managedkubernetes.ValidDigest(input.ObservedSubjectDigest) {
			return result, errs.ErrInvalid
		}
	} else if (input.Outcome != entity.PersonalIntegrationCredentialAuthenticationRejected && input.Outcome != entity.PersonalIntegrationCredentialVerificationUnavailable) || input.ObservedSubjectDigest != "" {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	source, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	connectionRef, shared, err := locateBrokerIdentityHealthTestTx(ctx, tx, current.organizationID, binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	if shared {
		return repository.completeSharedKubernetesHealthTx(ctx, tx, current, binding, input, source, connectionRef)
	}
	job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	var id, digest, inputDigest, leaseRef, fenceDigest, instance, targetRef string
	var generation, registryRevision, targetRevision int64
	var expiresAt time.Time
	var recordedOutcome, recordedSubject, recordedState string
	var recordedApplied bool
	err = tx.QueryRow(ctx, queryKubernetesHealthRead, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"test_id": job.id, "attempt_ref": input.AttemptRef, "shared": false}).Scan(&id, &digest, &inputDigest, &leaseRef, &fenceDigest,
		&generation, &instance, &expiresAt, &registryRevision, &targetRef, &targetRevision,
		&recordedOutcome, &recordedSubject, &recordedState, &recordedApplied)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if digest != input.SnapshotDigest || inputDigest != binding.InputDigest || leaseRef != binding.LeaseRef ||
		fenceDigest != kubernetesHealthFenceDigest(binding.Fence) || generation != binding.Generation || instance != binding.WorkloadInstance {
		return result, errs.ErrForbidden
	}
	if recordedOutcome != "" {
		if recordedOutcome != string(input.Outcome) || recordedSubject != input.ObservedSubjectDigest {
			return result, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return entity.KubernetesIdentityHealthReceipt{TestRef: job.ref, State: recordedState, Applied: recordedApplied}, nil
	}
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	result = entity.KubernetesIdentityHealthReceipt{TestRef: job.ref, State: "CANCELLED"}
	resolved, target, _, _, eligibilityErr := repository.kubernetesPersonalHealthInputTx(ctx, tx, current.organizationID, job, source)
	if errors.Is(eligibilityErr, errs.ErrUnavailable) {
		return result, eligibilityErr
	}
	fresh := eligibilityErr == nil && job.state == "PROBING" && kubernetesHealthBindingMatches(job, binding, now) &&
		now.Before(expiresAt) && source.registry.Revision == registryRevision && target.Ref == targetRef && target.Revision == targetRevision
	if fresh {
		observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: input.Outcome}
		if input.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
			observation.ConfirmedCapabilityKeys = []string{job.snapshot.HealthCapabilityKey}
		}
		verified, err := platformservice.CompletePersonalIntegrationCredentialVerification(resolved.binding, job.snapshot,
			job.snapshot.DefinitionDigest, job.snapshot.ConfigurationDigest, observation)
		if err != nil {
			return result, err
		}
		applied, err := persistKubernetesHealthVerificationTx(ctx, tx, job, resolved.binding.Version, verified, expiresAt)
		if err != nil {
			return result, err
		}
		if applied {
			result.Applied, result.State = true, "FAILED"
			if input.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
				result.State = "SUCCEEDED"
			}
		}
	}
	if job.state == "PROBING" || job.state == "CLAIMED" || job.state == "DUE" {
		outcome := string(input.Outcome)
		if !result.Applied {
			outcome = "CANCELLED"
			if resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job) != nil {
				return result, errs.ErrUnavailable
			}
		}
		if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, result.State, outcome); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, queryKubernetesHealthComplete, pgx.StrictNamedArgs{"attempt_id": id, "outcome": string(input.Outcome),
		"subject_digest": input.ObservedSubjectDigest, "state": result.State, "applied": result.Applied}); err != nil {
		return result, errs.ErrUnavailable
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.kubernetes.health.complete", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_COMPLETED") != nil || tx.Commit(ctx) != nil {
		return entity.KubernetesIdentityHealthReceipt{}, errs.ErrUnavailable
	}
	return result, nil
}

// persistKubernetesHealthVerificationTx checks the original deadline in the final binding UPDATE.
// The preceding lock and validation cannot extend authorization while another dependency was slow.
// persistKubernetesHealthVerificationTx проверяет original deadline в final binding UPDATE.
// Предшествующие lock и validation не продлевают authorization при медленной dependency.
func persistKubernetesHealthVerificationTx(ctx context.Context, tx pgx.Tx, job personalCredentialWorkJob, version int64, binding entity.PersonalIntegrationCredential, deadline time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, queryKubernetesHealthBindingState, pgx.StrictNamedArgs{"organization_id": binding.OrganizationID,
		"binding_id": job.bindingID, "state": binding.State, "version": binding.Version, "expected_version": version,
		"generation": binding.Generation, "definition_digest": binding.VerifiedDefinitionDigest, "configuration_digest": binding.VerifiedConfigurationDigest,
		"confirmed": append([]string{}, binding.VerifiedCapabilities...), "denied": append([]string{}, binding.DeniedCapabilities...),
		"profiles": append([]string{}, binding.ObservedPermissionProfiles...), "expires_at": binding.ExpiresAt,
		"github_discovery": asJSON(binding.GitHubDiscovery), "deadline": deadline})
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return tag.RowsAffected() == 1, nil
}

// queryKubernetesHealthBindingState pins the owner deadline in the final source-specific UPDATE.
// queryKubernetesHealthBindingState закрепляет owner deadline в final source-specific UPDATE.
//
//go:embed sql/kubernetes_health__binding_state.sql
var queryKubernetesHealthBindingState string
