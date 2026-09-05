package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type secretDraftRow struct {
	id, ownerID, contentDigest, secretID, projectID, namespace, stagingNamespace, secretState string
	public                                                                                    entity.RuntimeSecretDraft
	encrypted                                                                                 *entity.RuntimeSecretDraftEncryptedDescriptor
}
type secretDraftOperationRow struct {
	recoveryEncrypted                                                             *entity.RuntimeSecretDraftEncryptedDescriptor
	recoveryMaterialization                                                       *entity.RuntimeSecretMaterialization
	id, ref, kind, state, actorID, intentDigest, claimantID, failure, correlation string
	draftVersion, secretVersion, currentRevision, targetRevision, claimGeneration int64
	expires                                                                       time.Time
	lease                                                                         *time.Time
	snapshot, encryptedCleanup, materializationCleanup                            []byte
	cleanupCompleted                                                              bool
}

func scanSecretDraft(row pgx.Row) (secretDraftRow, error) {
	var d secretDraftRow
	var raw []byte
	err := row.Scan(&d.id, &d.public.Ref, &d.public.Version, &d.public.Generation, &d.public.ProjectRef, &d.public.SecretRef, &d.public.Name, &d.public.Description, &d.public.ValueType,
		&d.public.State, &d.public.PublishedRevision, &d.public.CreatedAt, &d.public.UpdatedAt, &d.public.ExpiresAt, &d.ownerID, &d.contentDigest, &raw, &d.secretID, &d.projectID, &d.namespace, &d.stagingNamespace, &d.secretState)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, errs.ErrNotFound
	}
	if err != nil {
		return d, errs.ErrUnavailable
	}
	if len(raw) > 0 && json.Unmarshal(raw, &d.encrypted) != nil {
		return d, errs.ErrUnavailable
	}
	return d, nil
}
func (r *Repository) lockSecretDraft(ctx context.Context, tx pgx.Tx, org, ref string) (secretDraftRow, error) {
	return scanSecretDraft(tx.QueryRow(ctx, querySecretDraftLock, pgx.StrictNamedArgs{"organization_id": org, "draft_ref": ref}))
}
func (r *Repository) lockSecretDraftOperation(ctx context.Context, tx pgx.Tx, org, ref string) (secretDraftOperationRow, error) {
	var o secretDraftOperationRow
	err := tx.QueryRow(ctx, querySecretDraftOperationLock, pgx.StrictNamedArgs{"organization_id": org, "operation_ref": ref}).Scan(&o.id, &o.ref, &o.kind, &o.state, &o.actorID, &o.draftVersion, &o.secretVersion, &o.currentRevision, &o.targetRevision, &o.intentDigest, &o.claimantID, &o.claimGeneration, &o.expires, &o.lease, &o.failure, &o.snapshot, &o.encryptedCleanup, &o.materializationCleanup, &o.cleanupCompleted, &o.correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, errs.ErrNotFound
	}
	if err != nil {
		return o, errs.ErrUnavailable
	}
	if len(o.encryptedCleanup) > 0 && json.Unmarshal(o.encryptedCleanup, &o.recoveryEncrypted) != nil ||
		len(o.materializationCleanup) > 0 && json.Unmarshal(o.materializationCleanup, &o.recoveryMaterialization) != nil {
		return o, errs.ErrUnavailable
	}
	return o, nil
}
func (r *Repository) lookupSecretDraftOperation(ctx context.Context, tx pgx.Tx, org, actor, kind, key, ref, token string) (string, string, error) {
	var operationRef, draftRef string
	if actor == "" {
		actor = "00000000-0000-0000-0000-000000000000"
	}
	err := tx.QueryRow(ctx, querySecretDraftOperationLookup, pgx.StrictNamedArgs{"organization_id": org, "actor_id": actor, "kind": kind, "idempotency_key": key, "operation_ref": ref, "token_digest": token}).Scan(&operationRef, &draftRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrNotFound
	}
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	return operationRef, draftRef, nil
}
func (r *Repository) secretDraftAccess(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, permission string) error {
	if s.actorID != d.ownerID || s.authorityProjectID != "" && s.authorityProjectID != d.projectID {
		return errs.ErrNotFound
	}
	kind, ref := "SECRET", d.public.SecretRef
	if permission == "secret.create" {
		kind, ref = "PROJECT", d.public.ProjectRef
	}
	target, err := r.resolveAccessTarget(ctx, tx, s.organizationID, entity.AccessScope{ResourceKind: kind, ResourceRef: ref})
	if err != nil {
		return err
	}
	if err = r.requireAccess(ctx, tx, s, permission, target); err != nil {
		return errs.ErrNotFound
	}
	return nil
}
func (r *Repository) secretDraftOwnerScope(ctx context.Context, tx pgx.Tx, s scope, actorID string) (scope, error) {
	var ref, org string
	if tx.QueryRow(ctx, querySecretDraftOwnerScope, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": actorID}).Scan(&ref, &org) != nil {
		return scope{}, errs.ErrForbidden
	}
	var owner scope
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, ref, org).Scan(&owner.organizationID, &owner.organizationRef, &owner.actorID, &owner.actorRef, &owner.actorName, &owner.role) != nil {
		return scope{}, errs.ErrForbidden
	}
	return owner, nil
}
func (r *Repository) GetRuntimeSecretDraft(ctx context.Context, p value.Principal, ref string) (entity.RuntimeSecretDraft, error) {
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.RuntimeSecretDraft{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanSecretDraft(tx.QueryRow(ctx, querySecretDraftGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "draft_ref": ref}))
	if err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	permission := "secret.view"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RuntimeSecretDraft{}, errs.ErrUnavailable
	}
	return d.public, nil
}
func (r *Repository) updateSecretDraft(ctx context.Context, tx pgx.Tx, d *secretDraftRow, state string, encrypted *entity.RuntimeSecretDraftEncryptedDescriptor, published int64) error {
	var raw any
	if encrypted != nil {
		encoded, err := json.Marshal(encrypted)
		if err != nil {
			return errs.ErrInvalid
		}
		raw = string(encoded)
	}
	err := tx.QueryRow(ctx, querySecretDraftUpdate, pgx.StrictNamedArgs{"draft_id": d.id, "version": d.public.Version, "state": state, "encrypted": raw, "published_revision": published}).Scan(&d.public.Version, &d.public.UpdatedAt)
	if err != nil {
		return errs.ErrConflict
	}
	d.public.State = state
	d.public.PublishedRevision = published
	if encrypted != nil {
		d.encrypted = encrypted
	}
	return nil
}
func (r *Repository) auditSecretDraft(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, o secretDraftOperationRow, outcome string) error {
	ref, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, querySecretDraftAudit, pgx.StrictNamedArgs{"ref": ref, "organization_id": s.organizationID, "project_id": d.projectID, "actor_id": o.actorID, "action": "runtime-secret-draft." + strings.ToLower(o.kind), "secret_ref": d.public.SecretRef, "outcome": outcome, "correlation_ref": o.correlation})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
func secretDraftGrantExpiry(p value.Principal, now time.Time) time.Time {
	expires := now.Add(runtimeSecretOperationTTL)
	freshUntil := p.CredentialAuthenticatedAt.Add(5 * time.Minute)
	if freshUntil.Before(expires) {
		expires = freshUntil
	}
	return expires
}
func (r *Repository) PrepareRuntimeSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
	var empty entity.RuntimeSecretDraftOperationReceipt
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	var d secretDraftRow
	var secret lockedRuntimeSecret
	if input.Kind == "SAVE" {
		decoded, decodeErr := hex.DecodeString(input.ExpectedContentSHA256)
		if decodeErr != nil || len(decoded) != 32 || strings.ToLower(input.ExpectedContentSHA256) != input.ExpectedContentSHA256 {
			return empty, errs.ErrInvalid
		}
		kind := "ROTATE"
		if input.SecretRef == "" {
			kind = "CREATE"
		}
		secret, err = r.prepareRuntimeSecretTarget(ctx, tx, s, p, repoport.RuntimeSecretPrepareInput{Kind: kind, ProjectRef: input.ProjectRef, SecretRef: input.SecretRef, Name: input.Name, Description: input.Description, ValueType: input.ValueType, Mutation: input.Mutation})
		if err != nil {
			return empty, err
		}
		if kind == "CREATE" {
			var creatorMatches bool
			if tx.QueryRow(ctx, querySecretDraftCreatorMatches, pgx.StrictNamedArgs{"secret_id": secret.id, "actor_id": s.actorID}).Scan(&creatorMatches) != nil {
				return empty, errs.ErrUnavailable
			}
			if !creatorMatches {
				return empty, errs.ErrNotFound
			}
		}
	} else {
		d, err = r.lockSecretDraft(ctx, tx, s.organizationID, input.DraftRef)
		if err != nil {
			return empty, err
		}
		secret, err = r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
		if err != nil {
			return empty, err
		}
		permission := "secret.rotate"
		if secret.state == "PROVISIONING" {
			permission = "secret.create"
		}
		if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
			return empty, err
		}
	}
	opRef, existingDraftRef, lookupErr := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, s.actorID, input.Kind, input.Mutation.IdempotencyKey, "", "")
	if lookupErr == nil {
		if input.Kind == "SAVE" {
			d, err = r.lockSecretDraft(ctx, tx, s.organizationID, existingDraftRef)
			if err != nil {
				return empty, err
			}
		}
		if d.public.Ref != existingDraftRef || d.secretID != secret.id || d.ownerID != s.actorID {
			return empty, errs.ErrConflict
		}
		o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, opRef)
		if err != nil {
			return empty, err
		}
		if o.intentDigest != input.Mutation.IntentDigest {
			return empty, errs.ErrConflict
		}
		result := entity.RuntimeSecretDraftOperationReceipt{OperationRef: o.ref, State: o.state, FailureCode: o.failure, Draft: d.public}
		if o.state == "COMPLETED" {
			var saved entity.RuntimeSecretDraftResult
			if json.Unmarshal(o.snapshot, &saved) != nil {
				return empty, errs.ErrUnavailable
			}
			result.Draft = saved.Draft
			result.TerminalSecret = saved.Secret
		} else if o.state == "PREPARED" && o.draftVersion == d.public.Version && now.Before(d.public.ExpiresAt) {
			grant, token, err := newRuntimeSecretGrant()
			if err != nil {
				return empty, errs.ErrUnavailable
			}
			expires := secretDraftGrantExpiry(p, now)
			if tx.QueryRow(ctx, querySecretDraftOperationReissue, pgx.StrictNamedArgs{"operation_id": o.id, "token_digest": token, "grant_expires_at": expires}).Scan(&result.ExpiresAt) != nil {
				return empty, errs.ErrConflict
			}
			result.OperationGrant = grant
		} else if o.state != "FAILED" {
			return empty, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(lookupErr, errs.ErrNotFound) {
		return empty, lookupErr
	}
	if input.Kind == "SAVE" {
		if secret.valueType != input.ValueType || secret.state != "ACTIVE" && secret.state != "PROVISIONING" || input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != secret.version {
			return empty, errs.ErrVersionMismatch
		}
		if input.SecretRef == "" && secret.state != "PROVISIONING" {
			return empty, errs.ErrConflict
		}
		ref, err := newRef("sdft")
		if err != nil {
			return empty, errs.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, querySecretDraftInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": s.organizationID, "secret_id": secret.id, "actor_id": s.actorID, "content_sha256": input.ExpectedContentSHA256, "staged_namespace": r.runtimeSecretStagingNamespace}); err != nil {
			return empty, mapWriteError(err)
		}
		d, err = r.lockSecretDraft(ctx, tx, s.organizationID, ref)
		if err != nil {
			return empty, err
		}
	} else {
		if *input.Mutation.ExpectedVersion != d.public.Version {
			return empty, errs.ErrVersionMismatch
		}
		if !now.Before(d.public.ExpiresAt) {
			return empty, errs.ErrConflict
		}
		var active bool
		if tx.QueryRow(ctx, querySecretDraftOperationActive, pgx.StrictNamedArgs{"draft_id": d.id}).Scan(&active) != nil {
			return empty, errs.ErrUnavailable
		}
		if active {
			return empty, errs.ErrConflict
		}
		switch input.Kind {
		case "VALIDATE":
			if d.public.State != "DRAFT" && d.public.State != "VALID" {
				return empty, errs.ErrConflict
			}
		case "PUBLISH":
			var publishing bool
			if tx.QueryRow(ctx, querySecretDraftPublishingActive, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&publishing) != nil {
				return empty, errs.ErrUnavailable
			}
			if publishing {
				return empty, errs.ErrConflict
			}
			if d.public.State != "VALID" || secret.version != input.ExpectedSecretVersion || secret.state != "ACTIVE" && secret.state != "PROVISIONING" {
				return empty, errs.ErrVersionMismatch
			}
			if _, active, err := r.lockActiveRuntimeSecretMutation(ctx, tx, secret.id); err != nil {
				return empty, err
			} else if active {
				return empty, errs.ErrConflict
			}
			if err = r.updateSecretDraft(ctx, tx, &d, "PUBLISHING", nil, 0); err != nil {
				return empty, err
			}
		case "DISCARD":
			if d.public.State != "DRAFT" && d.public.State != "VALID" && d.public.State != "FAILED" {
				return empty, errs.ErrConflict
			}
			if err = r.updateSecretDraft(ctx, tx, &d, "DISCARDED", nil, 0); err != nil {
				return empty, err
			}
		}
	}
	targetRevision := int64(0)
	if input.Kind == "PUBLISH" {
		if tx.QueryRow(ctx, querySecretDraftTargetRevision, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&targetRevision) != nil {
			return empty, errs.ErrUnavailable
		}
	}
	grant, token, err := newRuntimeSecretGrant()
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	opRef, err = newRef("sdop")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	expires := secretDraftGrantExpiry(p, now)
	_, err = tx.Exec(ctx, querySecretDraftOperationInsert, pgx.StrictNamedArgs{"ref": opRef, "organization_id": s.organizationID, "draft_id": d.id, "actor_id": s.actorID, "kind": input.Kind, "draft_version": d.public.Version, "secret_version": secret.version, "current_revision": secret.currentRevision, "target_revision": targetRevision, "token_digest": token, "idempotency_key": input.Mutation.IdempotencyKey, "intent_digest": input.Mutation.IntentDigest, "grant_expires_at": expires, "correlation_ref": p.CorrelationRef})
	if err != nil {
		return empty, mapWriteError(err)
	}
	o := secretDraftOperationRow{ref: opRef, kind: input.Kind, actorID: s.actorID, correlation: p.CorrelationRef}
	if err = r.auditSecretDraft(ctx, tx, s, d, o, "SUCCEEDED"); err != nil {
		return empty, err
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return entity.RuntimeSecretDraftOperationReceipt{OperationRef: opRef, OperationGrant: grant, State: "PREPARED", ExpiresAt: expires, Draft: d.public}, nil
}

func (r *Repository) ConsumeRuntimeSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error) {
	var empty entity.RuntimeSecretDraftWork
	if !validRuntimeSecretWorkPrincipal(p, "platform.runtime-secret-drafts.operations.consume") || !validRuntimeSecretClaimant(input.ClaimantID) || len(input.OperationGrant) < 32 || len(input.OperationGrant) > 256 {
		return empty, errs.ErrForbidden
	}
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := sha256.Sum256([]byte(input.OperationGrant))
	opRef, draftRef, err := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, "", "", "", "", hex.EncodeToString(hash[:]))
	if err != nil {
		return empty, err
	}
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, draftRef)
	if err != nil {
		return empty, err
	}
	o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, opRef)
	if err != nil {
		return empty, err
	}
	owner, err := r.secretDraftOwnerScope(ctx, tx, s, o.actorID)
	if err != nil {
		return empty, err
	}
	permission := "secret.rotate"
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
	if err != nil {
		return empty, err
	}
	if secret.state == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, owner, d, permission); err != nil {
		return empty, err
	}
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	if o.state != "PREPARED" || o.draftVersion != d.public.Version || !now.Before(d.public.ExpiresAt) || o.kind != "DISCARD" && (d.public.State == "DISCARDED" || d.public.State == "EXPIRED" || d.public.State == "PUBLISHED") {
		return empty, errs.ErrConflict
	}
	if o.kind == "PUBLISH" && (secret.version != o.secretVersion || secret.currentRevision != o.currentRevision) {
		return empty, errs.ErrVersionMismatch
	}
	o.claimantID = input.ClaimantID
	var deadline time.Time
	if tx.QueryRow(ctx, querySecretDraftOperationConsume, pgx.StrictNamedArgs{"operation_id": o.id, "claimant_id": input.ClaimantID}).Scan(&o.claimGeneration, &deadline) != nil {
		return empty, errs.ErrConflict
	}
	o.lease = &deadline
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return secretDraftWork(d, o), nil
}
func secretDraftWork(d secretDraftRow, o secretDraftOperationRow) entity.RuntimeSecretDraftWork {
	digest := sha256.Sum256([]byte(d.public.Ref))
	work := entity.RuntimeSecretDraftWork{OperationRef: o.ref, Kind: o.kind, Draft: d.public, ExpectedContentSHA256: d.contentDigest, Namespace: d.namespace, StagedNamespace: d.stagingNamespace, StagedSecretName: "runtime-secret-draft-" + hex.EncodeToString(digest[:16]), StagedSecretKey: "ciphertext", TargetRevision: o.targetRevision, Encrypted: d.encrypted, ClaimantID: o.claimantID, ClaimGeneration: o.claimGeneration, ExpiresAt: o.expires}
	if o.lease != nil {
		work.LeaseDeadline = *o.lease
	}
	work.RecoveryEncrypted = o.recoveryEncrypted
	work.RecoveryMaterialization = o.recoveryMaterialization
	return work
}
