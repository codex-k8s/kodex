package platform

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// ConfigureSSHComposeSigningKey installs a dedicated owner Ed25519 key without sharing it with workers.
// ConfigureSSHComposeSigningKey устанавливает отдельный owner Ed25519 key без передачи worker.
func (repository *Repository) ConfigureSSHComposeSigningKey(key []byte) error {
	if len(key) != ed25519.PrivateKeySize {
		return errs.ErrInvalid
	}
	private := ed25519.PrivateKey(key)
	if !reflect.DeepEqual(private[32:], ed25519.NewKeyFromSeed(private[:32])[32:]) {
		return errs.ErrInvalid
	}
	repository.sshComposeSigningKey = append([]byte(nil), key...)
	return nil
}

// RenewSSHComposeInvocation issues a fresh short permit only while every owner dependency remains eligible.
// RenewSSHComposeInvocation выдаёт свежий короткий permit лишь при допустимости всех owner dependencies.
func (repository *Repository) RenewSSHComposeInvocation(ctx context.Context, principal value.Principal, binding platformrepo.SSHComposeLeaseBinding) (map[string]any, error) {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.runtime.ssh-compose.renew" {
		return nil, errs.ErrForbidden
	}
	return repository.sshComposeLeaseOperation(ctx, principal, binding, true)
}

// ResolveSSHComposeCredentialProjection returns only exact secret metadata to the broker's protected method.
// ResolveSSHComposeCredentialProjection возвращает только exact secret metadata защищённому broker method.
func (repository *Repository) ResolveSSHComposeCredentialProjection(ctx context.Context, principal value.Principal, binding platformrepo.SSHComposeLeaseBinding) (map[string]any, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.ssh-compose.resolve") {
		return nil, errs.ErrForbidden
	}
	return repository.sshComposeLeaseOperation(ctx, principal, binding, false)
}

// sshComposeLeaseOperation commits any renewal before releasing its signed permit or descriptor.
// sshComposeLeaseOperation фиксирует продление до выдачи подписанного permit либо descriptor.
func (repository *Repository) sshComposeLeaseOperation(ctx context.Context, principal value.Principal, binding platformrepo.SSHComposeLeaseBinding, renew bool) (map[string]any, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := repository.issueSSHComposePermit(ctx, tx, current, binding, "", renew)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return result, nil
}

// issueSSHComposePermit rechecks the fenced invocation, immutable input and current source/secret graph.
// issueSSHComposePermit проверяет fenced invocation, immutable input и current source/secret graph.
func (repository *Repository) issueSSHComposePermit(ctx context.Context, tx pgx.Tx, current scope, binding platformrepo.SSHComposeLeaseBinding, purpose string, renew bool) (map[string]any, error) {
	if len(repository.sshComposeSigningKey) != ed25519.PrivateKeySize {
		return nil, errs.ErrUnavailable
	}
	if binding.ConnectionTestRef != "" {
		return repository.issueSSHComposeTestPermit(ctx, tx, current, binding)
	}
	var id, nodeID, projectID, connectionID, operation, effectKey, inputDigest, initiatorRef, state, ref, leaseRef, connectionRef, runID string
	var rawInput, rawSnapshot []byte
	var deadline *time.Time
	var expires time.Time
	var generation int64
	var recovery bool
	if err := tx.QueryRow(ctx, querySSHComposeLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "lease_ref": binding.LeaseRef,
		"generation": binding.Generation, "fence_digest": sshcompose.Digest([]byte(binding.Fence)), "workload_instance": binding.WorkloadInstance,
	}).Scan(&id, &nodeID, &projectID, &connectionID, &operation, &rawInput, &rawSnapshot, &deadline, &effectKey, &inputDigest, &initiatorRef, &expires, &state, &ref, &leaseRef, &generation, &connectionRef, &runID, &recovery); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrForbidden
		}
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	initiator := current
	initiator.actorRef = initiatorRef
	if err := repository.requireAccess(ctx, tx, initiator, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}); err != nil {
		return nil, err
	}
	var stored sshcompose.Snapshot
	var input map[string]any
	if sshcompose.Decode(rawSnapshot, &stored, 32768) != nil || json.Unmarshal(rawInput, &input) != nil {
		return nil, errs.ErrForbidden
	}
	resolved, err := repository.resolveSSHComposeSnapshot(ctx, tx, initiator, projectID, nodeID, connectionID, operation, input, &stored)
	if err != nil {
		return nil, err
	}
	if operation == "ssh.compose.deploy" {
		resolved.DeploymentRef, resolved.DeploymentInputDigest, resolved.DeploymentEffectKey = stored.DeploymentRef, stored.DeploymentInputDigest, stored.DeploymentEffectKey
	}
	if !reflect.DeepEqual(stored, resolved) {
		return nil, errs.ErrForbidden
	}
	now := time.Now().UTC()
	if recovery {
		if renew {
			return nil, errs.ErrForbidden
		}
		purpose = "STATUS"
		end := expires
		deadline = &end
	}
	if deadline == nil {
		end := now.Add(sshcompose.MaximumLifetime)
		deadline = &end
	}
	if !deadline.After(now) {
		return nil, errs.ErrForbidden
	}
	if renew {
		if operation != "ssh.compose.deploy" {
			return nil, errs.ErrForbidden
		}
		expires = now.Add(30 * time.Second)
	}
	if deadline.Before(expires) {
		expires = *deadline
	}
	runtimeDeadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, runID)
	if err != nil {
		return nil, err
	}
	if expired {
		return nil, errs.ErrForbidden
	}
	if runtimeDeadline != nil && runtimeDeadline.EffectiveDeadlineAt.Before(expires) {
		expires = runtimeDeadline.EffectiveDeadlineAt
	}
	if purpose == "" {
		switch operation {
		case "ssh.compose.inspect":
			purpose = "INSPECT"
		case "ssh.compose.deploy":
			purpose = "DEPLOY"
		case "ssh.compose.status":
			purpose = "STATUS"
		default:
			return nil, errs.ErrForbidden
		}
		if renew {
			purpose = "RENEW"
		}
	}
	permit := sshcompose.Permit{Version: sshcompose.ProtocolVersion, Purpose: purpose, Target: stored.Target, InvocationRef: ref, RootRunRef: stored.RootRunRef,
		SessionRef: stored.SessionRef, TurnRef: stored.TurnRef, Attempt: stored.Attempt, LeaseRef: leaseRef, Generation: generation, EffectKey: effectKey, InputDigest: inputDigest,
		DeploymentRef: stored.DeploymentRef, DeploymentInputDigest: stored.DeploymentInputDigest, MergedSHA: stored.MergedSHA, TreeSHA: stored.TreeSHA, BundleSHA256: stored.BundleSHA256,
		IssuedAt: now.Unix(), ExpiresAt: expires.Unix(), Deadline: deadline.Unix()}
	if stored.DeploymentEffectKey != "" {
		permit.EffectKey = stored.DeploymentEffectKey
	}
	signed, err := sshcompose.Sign(permit, ed25519.PrivateKey(repository.sshComposeSigningKey), now)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	if _, err := tx.Exec(ctx, querySSHComposeRenew, pgx.StrictNamedArgs{"invocation_id": id, "expires_at": expires, "deadline": *deadline}); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	encoded, _ := json.Marshal(signed)
	return map[string]any{"permit": encoded, "snapshot": rawSnapshot, "targetRef": stored.Target.Ref, "secretRef": stored.SSHSecret.Ref, "expiresAt": expires, "leaseRef": leaseRef, "generation": generation, "fence": binding.Fence,
		"descriptor": entity.RuntimeSecretRevisionDescriptor{Revision: stored.SSHSecret.Revision, Namespace: stored.SSHSecret.Namespace, SecretName: stored.SSHSecret.Name, SecretKey: stored.SSHSecret.Key,
			SecretUID: stored.SSHSecret.UID, SecretResourceVersion: stored.SSHSecret.ResourceVersion, ContentSHA256: stored.SSHSecret.ContentSHA256}}, nil
}
