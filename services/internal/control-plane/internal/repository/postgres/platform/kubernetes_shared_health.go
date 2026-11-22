package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesHealthSharedSource checks the exact stored shared test snapshot and active SERVICE; it grants no material access by itself.
// queryKubernetesHealthSharedSource проверяет exact stored shared test snapshot и active SERVICE; сама проверка не разрешает material access.
//
//go:embed sql/kubernetes_health__shared_source.sql
var queryKubernetesHealthSharedSource string

// queryKubernetesHealthSharedMaterial reads only the exact KUBE material fact selected by the existing authoritative metadata selector.
// queryKubernetesHealthSharedMaterial читает лишь exact KUBE material fact, выбранный existing authoritative metadata selector.
//
//go:embed sql/kubernetes_health__shared_material.sql
var queryKubernetesHealthSharedMaterial string

// queryKubernetesHealthSharedInsert persists one original shared probe attempt without a synthetic personal binding.
// queryKubernetesHealthSharedInsert сохраняет original shared probe attempt без synthetic personal binding.
//
//go:embed sql/kubernetes_health__shared_insert.sql
var queryKubernetesHealthSharedInsert string

// kubernetesSharedHealthMatches checks the original typed shared lease using the database clock and full opaque snapshot digest.
// kubernetesSharedHealthMatches сверяет original typed shared lease по database clock и full opaque snapshot digest.
func kubernetesSharedHealthMatches(job brokerSharedHealthWork, binding value.ManagedKubernetesBinding, now time.Time) bool {
	return binding.Validate() == nil && binding.InvocationRef == "" && binding.ConnectionTestRef == job.ref && binding.WorkloadInstance == job.instance && binding.LeaseRef == job.leaseRef && kubernetesHealthFenceDigest(binding.Fence) == job.fenceDigest && binding.Generation == job.generation && job.generation == 1 && binding.InputDigest == job.inputDigest && job.expiresAt != nil && now.Before(*job.expiresAt)
}

// sharedKubernetesHealthSourceTx preserves the one authoritative shared credential selector and adds only KUBE-specific package, target and material checks under the original Connection lock.
// sharedKubernetesHealthSourceTx сохраняет единый authoritative shared credential selector и добавляет лишь KUBE-specific package, target и material checks под original Connection lock.
func (repository *Repository) sharedKubernetesHealthSourceTx(ctx context.Context, tx pgx.Tx, organizationID string, job brokerSharedHealthWork, source kubernetesOperatorStored) (value.ManagedKubernetesTarget, value.ManagedKubernetesIdentityLocator, error) {
	var target value.ManagedKubernetesTarget
	var locator value.ManagedKubernetesIdentityLocator
	var fresh bool
	if err := tx.QueryRow(ctx, queryKubernetesHealthSharedSource, pgx.StrictNamedArgs{"organization_id": organizationID, "ref": job.ref}).Scan(&fresh); err != nil {
		return target, locator, errs.ErrUnavailable
	}
	if !fresh {
		return target, locator, errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, job.connectionRef, job.connection.definitionKey, job.connection.definitionVersion, job.connection.definitionDigest)
	if err != nil {
		return target, locator, err
	}
	delivery, known := definition.ConnectionCredentialDelivery()
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !known || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != string(entity.IntegrationBrokerIdentityKubernetes) || definition.Metadata.Key != integrationpackage.KubernetesDefinitionKey || definition.Spec.Adapter != "KUBERNETES" || !exists || health.Operation != "kubernetes.identity" || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
		return target, locator, errs.ErrForbidden
	}
	selected, err := repository.readSharedHealthCredentialSourceTx(ctx, tx, organizationID, job.connection.id, job.connectionRef, definition)
	if err != nil {
		return target, locator, err
	}
	if selected.identity == nil || selected.identityID != job.identityID || selected.identity.Ref != job.identity.Ref || selected.identity.Revision != job.identity.Revision || selected.identity.TargetRef != job.identity.TargetRef || selected.identity.Purpose != entity.IntegrationBrokerIdentityKubernetes {
		return target, locator, errs.ErrForbidden
	}
	for _, entry := range source.registry.Targets {
		if entry.Target.Ref == job.identity.TargetRef {
			target = entry.Target
		}
	}
	if target.Ref == "" {
		return target, locator, errs.ErrForbidden
	}
	locator = value.ManagedKubernetesIdentityLocator{IdentityRef: job.identity.Ref, TargetRef: job.identity.TargetRef, Revision: job.identity.Revision}
	var identityID string
	err = tx.QueryRow(ctx, queryKubernetesHealthSharedMaterial, pgx.StrictNamedArgs{"organization_id": organizationID, "identity_id": job.identityID, "identity_ref": job.identity.Ref, "revision": job.identity.Revision, "target_ref": job.identity.TargetRef}).Scan(&identityID, &locator.Namespace, &locator.Name, &locator.UID, &locator.ResourceVersion, &locator.ContentSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, locator, errs.ErrForbidden
	}
	if err != nil {
		return target, locator, errs.ErrUnavailable
	}
	if identityID != job.identityID || locator.Validate(source.registry.IdentityNamespace) != nil {
		return target, locator, errs.ErrUnavailable
	}
	return target, locator, nil
}

// beginSharedKubernetesHealthTx consumes the same shared job once, retaining exact SERVICE identity, original worker lease and immutable input; caller owns transaction rollback.
// beginSharedKubernetesHealthTx однократно расходует shared job, сохраняя exact SERVICE identity, original worker lease и immutable input; rollback принадлежит caller.
func (repository *Repository) beginSharedKubernetesHealthTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, source kubernetesOperatorStored, connectionRef string) (entity.KubernetesIdentityHealthAdmission, error) {
	var result entity.KubernetesIdentityHealthAdmission
	job, err := repository.lockSharedBrokerIdentityHealthTx(ctx, tx, current.organizationID, binding.ConnectionTestRef, connectionRef, entity.IntegrationBrokerIdentityKubernetes)
	if err != nil {
		return result, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	if job.state != "CLAIMED" || !kubernetesSharedHealthMatches(job, binding, now) {
		return result, errs.ErrForbidden
	}
	target, locator, err := repository.sharedKubernetesHealthSourceTx(ctx, tx, current.organizationID, job, source)
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
	err = tx.QueryRow(ctx, queryKubernetesHealthSharedInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "identity_id": job.identityID, "registry_revision": source.registry.Revision, "target_ref": target.Ref, "target_revision": target.Revision, "snapshot_digest": result.SnapshotDigest, "input_digest": binding.InputDigest, "test_id": job.id}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.KubernetesIdentityHealthAdmission{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.KubernetesIdentityHealthAdmission{}, mapWriteError(err)
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.kubernetes.health.begin", "i18n:INTEGRATION_CONNECTION_TEST_PROBE_ADMITTED") != nil || tx.Commit(ctx) != nil {
		return entity.KubernetesIdentityHealthAdmission{}, errs.ErrUnavailable
	}
	if err := (kubernetesOperationOwner{}).creditError(ctx, result.ExpiresAt); err != nil {
		return entity.KubernetesIdentityHealthAdmission{}, err
	}
	return result, nil
}

// completeSharedKubernetesHealthTx stores exact late evidence independently from publication; current owner/identity/source and the original deadline alone admit shared Connection changes.
// completeSharedKubernetesHealthTx сохраняет exact late evidence отдельно от publication; shared Connection меняется лишь при current owner/identity/source и original deadline.
func (repository *Repository) completeSharedKubernetesHealthTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, input command.KubernetesIdentityHealthCompletion, source kubernetesOperatorStored, connectionRef string) (entity.KubernetesIdentityHealthReceipt, error) {
	var result entity.KubernetesIdentityHealthReceipt
	job, err := repository.lockSharedBrokerIdentityHealthTx(ctx, tx, current.organizationID, binding.ConnectionTestRef, connectionRef, entity.IntegrationBrokerIdentityKubernetes)
	if err != nil {
		return result, err
	}
	var id, digest, inputDigest, lease, fence, instance, targetRef, outcome, subject, state string
	var generation, registryRevision, targetRevision int64
	var expires time.Time
	var applied bool
	err = tx.QueryRow(ctx, queryKubernetesHealthRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "test_id": job.id, "attempt_ref": input.AttemptRef, "shared": true}).Scan(&id, &digest, &inputDigest, &lease, &fence, &generation, &instance, &expires, &registryRevision, &targetRef, &targetRevision, &outcome, &subject, &state, &applied)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if digest != input.SnapshotDigest || inputDigest != binding.InputDigest || lease != binding.LeaseRef || fence != kubernetesHealthFenceDigest(binding.Fence) || generation != binding.Generation || instance != binding.WorkloadInstance {
		return result, errs.ErrForbidden
	}
	if outcome != "" {
		if outcome != string(input.Outcome) || subject != input.ObservedSubjectDigest {
			return result, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return entity.KubernetesIdentityHealthReceipt{TestRef: job.ref, State: state, Applied: applied}, nil
	}
	target, _, sourceErr := repository.sharedKubernetesHealthSourceTx(ctx, tx, current.organizationID, job, source)
	if sourceErr != nil && (accessInfrastructureFailure(sourceErr) || !errors.Is(sourceErr, errs.ErrForbidden) && !errors.Is(sourceErr, errs.ErrNotFound) && !errors.Is(sourceErr, errs.ErrConflict)) {
		return result, sourceErr
	}
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	result = entity.KubernetesIdentityHealthReceipt{TestRef: job.ref, State: "CANCELLED"}
	fresh := sourceErr == nil && job.state == "CLAIMED" && kubernetesSharedHealthMatches(job, binding, now) && now.Before(expires) && source.registry.Revision == registryRevision && target.Ref == targetRef && target.Revision == targetRevision
	changed := false
	if fresh {
		next := "FAILED"
		connectionState := "DEGRADED"
		errorCode := "INTEGRATION_UNAVAILABLE"
		authenticated := input.Outcome == entity.PersonalIntegrationCredentialAuthenticated
		if authenticated {
			next = "SUCCEEDED"
			connectionState = "CONNECTED"
			errorCode = ""
		}
		publication, err := tx.Begin(ctx)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if _, err := publication.Exec(ctx, queryBrokerHealthSharedFinish, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "state": next, "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", "error_code": errorCode}); err != nil {
			_ = publication.Rollback(ctx)
			return result, errs.ErrUnavailable
		}
		var updated string
		err = publication.QueryRow(ctx, queryBrokerHealthSharedConnection, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": job.connectionRef, "identity_id": job.identityID, "deadline": expires, "state": connectionState, "authenticated": authenticated, "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED"}).Scan(&updated)
		if errors.Is(err, pgx.ErrNoRows) {
			if publication.Rollback(ctx) != nil {
				return result, errs.ErrUnavailable
			}
		} else if err != nil {
			_ = publication.Rollback(ctx)
			return result, errs.ErrUnavailable
		} else {
			if publication.Commit(ctx) != nil {
				return result, errs.ErrUnavailable
			}
			result.State = next
			result.Applied = true
			changed = true
			job.state = next
		}
	}
	if job.state == "CLAIMED" {
		if _, err := tx.Exec(ctx, queryBrokerHealthSharedFinish, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "state": "CANCELLED", "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", "error_code": "INTEGRATION_CREDENTIAL_UNAVAILABLE"}); err != nil {
			return result, errs.ErrUnavailable
		}
		var reset string
		err := tx.QueryRow(ctx, queryBrokerHealthSharedReset, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id}).Scan(&reset)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrUnavailable
		}
		changed = err == nil
	}
	if _, err := tx.Exec(ctx, queryKubernetesHealthComplete, pgx.StrictNamedArgs{"attempt_id": id, "outcome": string(input.Outcome), "subject_digest": input.ObservedSubjectDigest, "state": result.State, "applied": result.Applied}); err != nil {
		return result, errs.ErrUnavailable
	}
	if changed {
		connection, err := scanConnection(tx.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, current.organizationID, job.connectionRef))
		if err != nil {
			return result, err
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", job.connectionRef, "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", connection.Version, connection.State); err != nil {
			return result, err
		}
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.kubernetes.health.complete", "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED") != nil || tx.Commit(ctx) != nil {
		return entity.KubernetesIdentityHealthReceipt{}, errs.ErrUnavailable
	}
	return result, nil
}
