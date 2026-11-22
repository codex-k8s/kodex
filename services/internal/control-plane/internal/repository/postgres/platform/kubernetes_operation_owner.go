package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesExecutionPlanRead returns immutable existing-plan metadata without issuing effect credit.
// queryKubernetesExecutionPlanRead возвращает immutable existing-plan metadata без effect credit.
//
//go:embed sql/kubernetes_execution__plan_read.sql
var queryKubernetesExecutionPlanRead string

// queryKubernetesExecutionActiveStream checks the exact stream inside the verified original claim.
// queryKubernetesExecutionActiveStream проверяет exact stream внутри verified original claim.
//
//go:embed sql/kubernetes_execution__active_stream.sql
var queryKubernetesExecutionActiveStream string

// kubernetesOperationOwner retains one fully rechecked existing claim inside its owner transaction.
// The technical broker never replaces the originating actor, selected identity, grant or route.
// kubernetesOperationOwner сохраняет fully rechecked existing claim внутри owner transaction.
// Technical broker не заменяет originating actor, selected identity, grant либо route.
type kubernetesOperationOwner struct {
	current     scope
	candidate   integrationInvocationCandidate
	selected    kubernetesInvocationIdentity
	input       managedkubernetes.OperationInput
	normalized  []byte
	lease       entity.ManagedKubernetesInvocationLease
	accessUntil time.Time
}

// kubernetesOperationBrokerCaller requires the exact registered broker method and invocation variant.
// Workload identity authenticates transport only; the owner still checks every originating dependency.
// kubernetesOperationBrokerCaller требует exact registered broker method и invocation variant.
// Workload identity подтверждает transport; owner отдельно проверяет originating dependencies.
func kubernetesOperationBrokerCaller(principal value.Principal, binding value.ManagedKubernetesBinding, permission string) error {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != permission || principal.ProjectRef != "" || binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" {
		return errs.ErrForbidden
	}
	return nil
}

// readKubernetesOperationOwnerTx locks source→Connection/binding→invocation and reuses the canonical
// running invocation predicate. It checks originating actor ACL, exact claim pins, current immutable
// package/input/approval and runtime deadline; no lease is renewed and no external credit is issued.
// readKubernetesOperationOwnerTx блокирует source→Connection/binding→invocation и использует canonical
// running invocation predicate. Проверяет originating actor ACL, exact claim pins, current immutable
// package/input/approval и runtime deadline; не продлевает lease и не выдаёт external credit.
func (repository *Repository) readKubernetesOperationOwnerTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding) (result kubernetesOperationOwner, resultErr error) {
	result.current = current
	if _, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare)); err != nil {
		return result, err
	}
	_, selected, err := repository.currentKubernetesInvocationSourceTx(ctx, tx, current.organizationID, binding.InvocationRef)
	if err != nil {
		return result, err
	}
	result.selected = selected
	err = scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState, current.organizationID, 1, "integration-gateway", "MANAGED_MCP", false, binding.InvocationRef, true, false, false), &result.candidate)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	actor := current
	actor.actorRef = result.candidate.initiatorRef
	actor.actorID = ""
	accessUntil, err := repository.requireAccessUntil(ctx, tx, actor, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: result.candidate.connectionRef})
	if err != nil {
		return result, err
	}
	result.accessUntil = accessUntil
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, result.candidate.connectionRef, result.candidate.definitionKey, result.candidate.definitionVersion, result.candidate.definitionDigest)
	if err != nil {
		return result, err
	}
	capability, known := definition.Capability(result.candidate.capabilityKey)
	if !known || result.candidate.definitionKey != integrationpackage.KubernetesDefinitionKey || result.candidate.resourceKind != "KUBERNETES_TARGET" || capability.Operation != result.candidate.operation || capability.Risk != result.candidate.risk || definition.ValidateInvocationApprovalPolicy(capability, result.candidate.approvalPolicy, result.candidate.boundedInput) != nil {
		return result, errs.ErrForbidden
	}
	result.input, result.normalized, err = managedkubernetes.DecodeOperationInput(result.candidate.operation, result.candidate.boundedInput)
	if err != nil {
		return result, errs.ErrForbidden
	}
	var resourceScope map[string]string
	if json.Unmarshal(result.candidate.resourceScope, &resourceScope) != nil || len(resourceScope) != 2 || resourceScope["target_ref"] != selected.target.Ref || result.input.ValidateGrantScope(result.candidate.operation, resourceScope["namespace"]) != nil {
		return result, errs.ErrForbidden
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "instance": binding.WorkloadInstance, "lease_ref": binding.LeaseRef, "fence_digest": kubernetesHealthFenceDigest(binding.Fence), "generation": binding.Generation, "input_digest": binding.InputDigest}
	err = tx.QueryRow(ctx, queryKubernetesInvocationLease, args).Scan(&result.lease.Ref, &result.lease.Generation, &result.lease.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, result.candidate.runID)
	if err != nil {
		return result, err
	}
	if expired {
		return result, errs.ErrForbidden
	}
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(result.lease.ExpiresAt) {
		result.lease.ExpiresAt = deadline.EffectiveDeadlineAt
	}
	if !result.accessUntil.IsZero() && result.accessUntil.Before(result.lease.ExpiresAt) {
		result.lease.ExpiresAt = result.accessUntil
	}
	if err := result.creditError(ctx, result.lease.ExpiresAt); err != nil {
		return result, err
	}
	return result, nil
}

// creditError separates loss of the request/infrastructure context from proven original-actor ACL expiry; neither case renews the exact finite credit or blocks historical cleanup.
// creditError отделяет потерю request/infrastructure context от доказанного original-actor ACL expiry; credit не продлевается и historical cleanup не блокируется.
func (owner kubernetesOperationOwner) creditError(ctx context.Context, until time.Time) error {
	if ctx.Err() != nil {
		return errs.ErrUnavailable
	}
	now := time.Now()
	if !until.After(now) || !owner.accessUntil.IsZero() && !now.Before(owner.accessUntil) {
		return errs.ErrForbidden
	}
	return nil
}

// kubernetesOperationSnapshotDigest binds immutable execution/source pins without expiring read clocks.
// A lease renewal changes credit time, never the operation snapshot or the original health lineage.
// kubernetesOperationSnapshotDigest связывает immutable execution/source pins без expiring read clocks.
// Продление lease меняет время credit, не operation snapshot либо original health lineage.
func kubernetesOperationSnapshotDigest(owner kubernetesOperationOwner, binding value.ManagedKubernetesBinding) (string, error) {
	return kubernetesOperationSnapshotDigestFromClaim(owner, kubernetesHealthFenceDigest(binding.Fence), binding.Generation)
}

// kubernetesOperationSnapshotDigestFromClaim verifies an owner-stored claim without recovering its raw fence.
// The digest alone grants neither broker binding nor effect credit; callers must lock and validate the live
// original claim and all current source pins. Hash bytes remain identical to the broker-bound snapshot.
// kubernetesOperationSnapshotDigestFromClaim сверяет owner-stored claim без восстановления raw fence.
// Digest не выдаёт broker binding либо effect credit; caller блокирует и проверяет live original claim
// и current source pins. Hash bytes совпадают с исходным broker-bound snapshot.
func kubernetesOperationSnapshotDigestFromClaim(owner kubernetesOperationOwner, fenceDigest string, generation int64) (string, error) {
	if !managedkubernetes.ValidDigest(fenceDigest) || generation < 1 {
		return "", errs.ErrForbidden
	}
	raw, err := json.Marshal(struct {
		Domain, Invocation, InputDigest, FenceDigest, Identity, Subject, Definition, Configuration, Scope string
		Generation, IdentityRevision, BindingGeneration, RegistryRevision, TargetRevision                 int64
	}{"kodex.managed-kubernetes.operation/v1", owner.candidate.ref, owner.candidate.inputDigest, fenceDigest, owner.selected.identity.Ref, owner.selected.subjectDigest, owner.candidate.definitionDigest, owner.selected.configurationDigest, owner.candidate.resourceScopeDigest, generation, owner.selected.identity.Revision, owner.selected.bindingGeneration, owner.selected.source.registry.Revision, owner.selected.target.Revision})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ResolveManagedKubernetesOperation exposes only broker-private metadata after the same current
// owner predicate used by claims and renewals. Resolve itself grants no target request or result credit.
// ResolveManagedKubernetesOperation выдаёт только broker-private metadata после current owner predicate,
// общего с claim и renew. Resolve не разрешает target request либо result credit.
func (repository *Repository) ResolveManagedKubernetesOperation(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, streamRefs ...string) (result entity.ManagedKubernetesOperationSnapshot, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.resolve"); err != nil {
		return result, err
	}
	if len(streamRefs) > 1 || len(streamRefs) == 1 && streamRefs[0] != "" && (len(streamRefs[0]) > 96 || !managedkubernetes.ValidLocator(streamRefs[0])) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return result, err
	}
	result.Digest, err = kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	selected := owner.selected
	result.Operation, result.Risk, result.ApprovalPolicy = owner.candidate.operation, owner.candidate.risk, owner.candidate.approvalPolicy
	result.IdentityRef, result.SubjectDigest, result.IdentityGeneration = selected.identity.Ref, selected.subjectDigest, selected.bindingGeneration
	if result.IdentityGeneration == 0 {
		result.IdentityGeneration = selected.identity.Revision
	}
	result.Target, result.NormalizedInputJSON, result.ExpiresAt = selected.target, append([]byte(nil), owner.normalized...), owner.lease.ExpiresAt
	for _, entry := range selected.source.registry.Targets {
		if entry.Target.Ref != selected.target.Ref {
			continue
		}
		for _, namespace := range entry.ProtectedNamespaces {
			result.ProtectedNamespaces = append(result.ProtectedNamespaces, namespace.Name)
		}
		if entry.ResourcePolicy != nil {
			raw, err := json.Marshal(entry.ResourcePolicy)
			if err != nil {
				return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
			}
			policy, digest, err := managedkubernetes.DecodeResourcePolicy(raw)
			if err != nil {
				return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
			}
			result.ResourcePolicy, result.ResourcePolicySHA256 = &policy, digest
		}
	}
	result.IdentityLocator = value.ManagedKubernetesIdentityLocator{IdentityRef: selected.identity.Ref, TargetRef: selected.target.Ref, Revision: selected.identity.Revision}
	err = tx.QueryRow(ctx, queryKubernetesSetupMaterialRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "identity_id": selected.identityID}).Scan(&result.IdentityLocator.Namespace, &result.IdentityLocator.Name, &result.IdentityLocator.UID, &result.IdentityLocator.ResourceVersion, &result.IdentityLocator.ContentSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
	}
	if result.IdentityLocator.Validate(selected.source.registry.IdentityNamespace) != nil {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
	}
	if owner.input.Resource != "" {
		var resourceScope map[string]string
		if json.Unmarshal(owner.candidate.resourceScope, &resourceScope) != nil {
			return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
		}
		result.Resource = value.ManagedKubernetesResourceAddress{Group: owner.input.Group, Version: owner.input.Version, Resource: owner.input.Resource, Namespace: owner.input.Namespace, Name: owner.input.Name, UID: owner.input.UID, ResourceVersion: owner.input.ResourceVersion, Subresource: owner.input.Subresource, Namespaced: resourceScope["namespace"] != "@cluster", AllNamespaces: resourceScope["namespace"] == "@all-namespaces"}
		if result.Resource.Validate() != nil {
			return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrForbidden
		}
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrInvalid
	}
	planArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": result.Digest}
	err = tx.QueryRow(ctx, queryKubernetesExecutionPlanRead, planArgs).Scan(&result.PlanRef, &result.PlanDigest, &result.ReconcileOnly)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
	}
	if err == nil {
		result.PlanRevision = 1
	}
	if len(streamRefs) == 1 && streamRefs[0] != "" {
		var nodeRef string
		var nodeGeneration int64
		var streamDeadline time.Time
		streamArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": result.Digest, "generation": binding.Generation, "stream_ref": streamRefs[0]}
		err = tx.QueryRow(ctx, queryKubernetesExecutionActiveStream, streamArgs).Scan(&nodeRef, &nodeGeneration, &streamDeadline)
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrForbidden
		}
		if err != nil {
			return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
		}
		if streamDeadline.Before(result.ExpiresAt) {
			result.ExpiresAt = streamDeadline
		}
		if nodeRef != "" {
			for _, node := range selected.source.registry.Nodes {
				if node.Transport.Ref == nodeRef && node.Transport.Generation == nodeGeneration && !node.Retired {
					pin := node.Transport
					result.NodeTransport = &pin
				}
			}
			if result.NodeTransport == nil {
				return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrForbidden
			}
		}
	}
	if err := owner.creditError(ctx, result.ExpiresAt); err != nil {
		clear(result.NormalizedInputJSON)
		return entity.ManagedKubernetesOperationSnapshot{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrUnavailable
	}
	if err := owner.creditError(ctx, result.ExpiresAt); err != nil {
		clear(result.NormalizedInputJSON)
		return entity.ManagedKubernetesOperationSnapshot{}, err
	}
	return result, nil
}
