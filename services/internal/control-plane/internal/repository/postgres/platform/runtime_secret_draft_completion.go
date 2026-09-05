package platform

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func validDraftEncrypted(d secretDraftRow, descriptor *entity.RuntimeSecretDraftEncryptedDescriptor) bool {
	if descriptor == nil {
		return false
	}
	work := secretDraftWork(d, secretDraftOperationRow{})
	raw, err := hex.DecodeString(descriptor.CiphertextSHA256)
	return err == nil && len(raw) == 32 && strings.ToLower(descriptor.CiphertextSHA256) == descriptor.CiphertextSHA256 &&
		descriptor.Namespace == work.StagedNamespace && descriptor.SecretName == work.StagedSecretName && descriptor.SecretKey == work.StagedSecretKey &&
		len(descriptor.SecretUID) > 0 && len(descriptor.SecretUID) <= 128 && len(descriptor.SecretResourceVersion) > 0 && len(descriptor.SecretResourceVersion) <= 128 &&
		len(descriptor.EncryptionKeyID) > 0 && len(descriptor.EncryptionKeyID) <= 128 && descriptor.EncryptionKeyGeneration > 0
}
func (r *Repository) finishDraftOperation(ctx context.Context, tx pgx.Tx, o secretDraftOperationRow, result entity.RuntimeSecretDraftResult, failure string) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, querySecretDraftOperationFinish, pgx.StrictNamedArgs{"operation_id": o.id, "state": result.State, "failure_code": failure, "snapshot": string(raw)})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
func (r *Repository) publishSecretDraft(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, o secretDraftOperationRow, materialization *entity.RuntimeSecretMaterialization) (*entity.RuntimeSecret, error) {
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
	if err != nil {
		return nil, err
	}
	if secret.version != o.secretVersion || secret.currentRevision != o.currentRevision || secret.state != "ACTIVE" && secret.state != "PROVISIONING" {
		return nil, errs.ErrVersionMismatch
	}
	name, nameErr := runtimesecret.VersionedKubernetesName(secret.ref, o.targetRevision)
	if nameErr != nil || !validRuntimeSecretMaterialization(materialization) || materialization.Namespace != d.namespace || materialization.SecretName != name || materialization.SecretKey != runtimeSecretKey || !runtimeSecretDigestsEqual(materialization.ContentSHA256, d.contentDigest) {
		return nil, errs.ErrInvalid
	}
	revisionRef, err := newRef("secr")
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, queryRuntimeSecretRevisionInsert, pgx.StrictNamedArgs{"ref": revisionRef, "secret_id": secret.id, "revision": o.targetRevision, "namespace": materialization.Namespace, "secret_name": materialization.SecretName, "secret_key": materialization.SecretKey, "secret_uid": materialization.SecretUID, "secret_resource_version": materialization.SecretResourceVersion, "content_sha256": materialization.ContentSHA256})
	if err != nil {
		return nil, mapWriteError(err)
	}
	result := &entity.RuntimeSecret{Ref: secret.ref, ProjectRef: secret.projectRef, Name: secret.name, Description: secret.description, ValueType: secret.valueType, Namespace: secret.namespace, CreatedAt: secret.createdAt}
	var prefix, suffix string
	err = tx.QueryRow(ctx, queryRuntimeSecretActivate, pgx.StrictNamedArgs{"secret_id": secret.id, "revision": o.targetRevision, "hint_prefix": "", "hint_suffix": "", "expected_version": secret.version, "expected_current_revision": secret.currentRevision, "expected_state": secret.state}).Scan(&result.Version, &result.State, &result.CurrentRevision, &prefix, &suffix, &result.UpdatedAt)
	if err != nil {
		return nil, errs.ErrConflict
	}
	result.CurrentRevisionDescriptor = &entity.RuntimeSecretRevisionDescriptor{Revision: o.targetRevision, Namespace: materialization.Namespace, SecretName: materialization.SecretName, SecretKey: materialization.SecretKey, SecretUID: materialization.SecretUID, SecretResourceVersion: materialization.SecretResourceVersion, ContentSHA256: materialization.ContentSHA256}
	return result, nil
}

func (r *Repository) FinishRuntimeSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, error) {
	var empty entity.RuntimeSecretDraftResult
	permission := map[string]string{"COMPLETE": "platform.runtime-secret-drafts.operations.complete", "FAIL": "platform.runtime-secret-drafts.operations.fail", "RECOVER": "platform.runtime-secret-drafts.materialization.recover", "CLEANUP": "platform.runtime-secret-drafts.cleanup.complete"}[input.Action]
	if permission == "" || !validRuntimeSecretWorkPrincipal(p, permission) || input.OperationRef == "" || input.ClaimGeneration < 0 {
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
	opRef, draftRef, err := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, "", "", "", input.OperationRef, "")
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
	if o.claimantID != input.ClaimantID || o.claimGeneration != input.ClaimGeneration {
		return empty, errs.ErrConflict
	}
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	if input.Action == "RECOVER" || input.Action == "CLEANUP" {
		result, err := r.recoverSecretDraft(ctx, tx, s, &d, &o, input, now)
		if err != nil {
			return empty, err
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return result, nil
	}
	if o.state == "FAILED" && input.Action == "FAIL" {
		if input.FailureCode != o.failure {
			return empty, errs.ErrConflict
		}
		result := entity.RuntimeSecretDraftResult{Draft: d.public, State: "FAILED"}
		if len(o.snapshot) > 0 && json.Unmarshal(o.snapshot, &result) != nil {
			return empty, errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return result, nil
	}
	if o.state == "COMPLETED" && input.Action == "COMPLETE" {
		if !reflect.DeepEqual(input.Encrypted, d.encrypted) {
			return empty, errs.ErrConflict
		}
		var result entity.RuntimeSecretDraftResult
		if json.Unmarshal(o.snapshot, &result) != nil {
			return empty, errs.ErrUnavailable
		}
		if result.Secret == nil {
			if input.Materialization != nil {
				return empty, errs.ErrConflict
			}
		} else {
			descriptor := result.Secret.CurrentRevisionDescriptor
			m := input.Materialization
			if m == nil || descriptor == nil || m.Namespace != descriptor.Namespace || m.SecretName != descriptor.SecretName || m.SecretKey != descriptor.SecretKey || m.SecretUID != descriptor.SecretUID || m.SecretResourceVersion != descriptor.SecretResourceVersion || m.ContentSHA256 != descriptor.ContentSHA256 {
				return empty, errs.ErrConflict
			}
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return result, nil
	}
	if o.state != "CLAIMED" || o.lease == nil || !now.Before(*o.lease) || o.draftVersion != d.public.Version {
		return empty, errs.ErrConflict
	}
	result := entity.RuntimeSecretDraftResult{Draft: d.public, State: "COMPLETED"}
	if input.Action == "FAIL" {
		if !validRuntimeSecretFailureCode(input.FailureCode) {
			return empty, errs.ErrInvalid
		}
		state := d.public.State
		if o.kind == "SAVE" {
			state = "FAILED"
		} else if o.kind == "PUBLISH" {
			state = "VALID"
		}
		if err = r.updateSecretDraft(ctx, tx, &d, state, nil, d.public.PublishedRevision); err != nil {
			return empty, err
		}
		result.Draft = d.public
		result.State = "FAILED"
	} else {
		owner, err := r.secretDraftOwnerScope(ctx, tx, s, o.actorID)
		if err != nil {
			return empty, err
		}
		permission := "secret.rotate"
		if d.secretState == "PROVISIONING" {
			permission = "secret.create"
		}
		if err = r.secretDraftAccess(ctx, tx, owner, d, permission); err != nil {
			return empty, err
		}
		if !now.Before(d.public.ExpiresAt) && o.kind != "DISCARD" {
			return empty, errs.ErrConflict
		}
		switch o.kind {
		case "SAVE":
			if d.public.State != "PREPARING" || !validDraftEncrypted(d, input.Encrypted) || input.Materialization != nil {
				return empty, errs.ErrInvalid
			}
			err = r.updateSecretDraft(ctx, tx, &d, "DRAFT", input.Encrypted, 0)
		case "VALIDATE":
			if d.public.State != "DRAFT" && d.public.State != "VALID" || !reflect.DeepEqual(d.encrypted, input.Encrypted) || !validDraftEncrypted(d, input.Encrypted) || input.Materialization != nil {
				return empty, errs.ErrInvalid
			}
			err = r.updateSecretDraft(ctx, tx, &d, "VALID", nil, 0)
		case "PUBLISH":
			if d.public.State != "PUBLISHING" || !reflect.DeepEqual(d.encrypted, input.Encrypted) || !validDraftEncrypted(d, input.Encrypted) {
				return empty, errs.ErrInvalid
			}
			result.Secret, err = r.publishSecretDraft(ctx, tx, s, d, o, input.Materialization)
			if err == nil {
				d.public.SecretVersion = result.Secret.Version
				err = r.updateSecretDraft(ctx, tx, &d, "PUBLISHED", nil, o.targetRevision)
			}
		case "DISCARD":
			if d.public.State != "DISCARDED" || !reflect.DeepEqual(d.encrypted, input.Encrypted) || input.Materialization != nil {
				return empty, errs.ErrInvalid
			}
			_, err = tx.Exec(ctx, querySecretDraftCleanupComplete, pgx.StrictNamedArgs{"operation_id": o.id})
		default:
			return empty, errs.ErrInvalid
		}
		if err != nil {
			return empty, err
		}
		result.Draft = d.public
	}
	if err = r.finishDraftOperation(ctx, tx, o, result, input.FailureCode); err != nil {
		return empty, err
	}
	outcome := "SUCCEEDED"
	if result.State == "FAILED" {
		outcome = "FAILED"
	}
	if err = r.auditSecretDraft(ctx, tx, s, d, o, outcome); err != nil {
		return empty, err
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return result, nil
}

func (r *Repository) recoverSecretDraft(ctx context.Context, tx pgx.Tx, s scope, d *secretDraftRow, o *secretDraftOperationRow, input repoport.RuntimeSecretDraftWorkInput, now time.Time) (entity.RuntimeSecretDraftResult, error) {
	result := entity.RuntimeSecretDraftResult{Draft: d.public, State: o.state, EncryptedAction: "KEEP", MaterializationAction: "KEEP"}
	if input.Action == "CLEANUP" {
		var encrypted *entity.RuntimeSecretDraftEncryptedDescriptor
		var materialization *entity.RuntimeSecretMaterialization
		if len(o.encryptedCleanup) > 0 && json.Unmarshal(o.encryptedCleanup, &encrypted) != nil || len(o.materializationCleanup) > 0 && json.Unmarshal(o.materializationCleanup, &materialization) != nil {
			return result, errs.ErrUnavailable
		}
		if !reflect.DeepEqual(encrypted, input.Encrypted) || !reflect.DeepEqual(materialization, input.Materialization) || o.state != "COMPLETED" && o.state != "FAILED" {
			return result, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, querySecretDraftCleanupComplete, pgx.StrictNamedArgs{"operation_id": o.id}); err != nil {
			return result, errs.ErrUnavailable
		}
		result.Completed = true
		return result, nil
	}
	if o.state == "CLAIMED" && o.lease != nil && now.Before(*o.lease) || o.state == "PREPARED" && now.Before(o.expires) {
		return result, errs.ErrConflict
	}
	// Устойчивое намерение сохраняет точный UID/RV даже после удаления и потери ACK.
	if o.recoveryEncrypted != nil {
		if input.Encrypted != nil && !reflect.DeepEqual(input.Encrypted, o.recoveryEncrypted) {
			return result, errs.ErrConflict
		}
		input.Encrypted = o.recoveryEncrypted
	}
	if o.recoveryMaterialization != nil {
		if input.Materialization != nil && !reflect.DeepEqual(input.Materialization, o.recoveryMaterialization) {
			return result, errs.ErrConflict
		}
		input.Materialization = o.recoveryMaterialization
	}
	if input.Encrypted != nil && !validDraftEncrypted(*d, input.Encrypted) {
		return result, errs.ErrInvalid
	}
	if input.Materialization != nil {
		name, err := runtimesecret.VersionedKubernetesName(d.public.SecretRef, o.targetRevision)
		if err != nil || !validRuntimeSecretMaterialization(input.Materialization) || input.Materialization.Namespace != d.namespace || input.Materialization.SecretName != name || input.Materialization.SecretKey != runtimeSecretKey || input.Materialization.ContentSHA256 != d.contentDigest {
			return result, errs.ErrInvalid
		}
		stored, err := r.runtimeSecretRevision(ctx, tx, d.secretID, o.targetRevision)
		if err == nil {
			if stored.SecretUID != input.Materialization.SecretUID || stored.SecretResourceVersion != input.Materialization.SecretResourceVersion || stored.ContentSHA256 != input.Materialization.ContentSHA256 {
				return result, errs.ErrConflict
			}
			var retained bool
			if tx.QueryRow(ctx, queryRuntimeSecretRevisionRetained, s.organizationID, d.secretID, o.targetRevision).Scan(&retained) != nil {
				return result, errs.ErrUnavailable
			}
			if !retained {
				result.MaterializationAction = "DELETE"
				if _, err := tx.Exec(ctx, queryRuntimeSecretRetireRevision, d.secretID, o.targetRevision); err != nil {
					return result, errs.ErrUnavailable
				}
			}
		} else if !errorsIsNotFound(err) {
			return result, err
		} else {
			result.MaterializationAction = "DELETE"
		}
	}
	if o.state == "CLAIMED" || o.state == "PREPARED" {
		state := d.public.State
		if state == "PREPARING" {
			state = "FAILED"
		} else if state == "PUBLISHING" {
			state = "VALID"
		}
		if err := r.updateSecretDraft(ctx, tx, d, state, nil, d.public.PublishedRevision); err != nil {
			return result, err
		}
		o.state = "FAILED"
		result.State = "FAILED"
		result.Draft = d.public
		if err := r.finishDraftOperation(ctx, tx, *o, result, "GRANT_EXPIRED"); err != nil {
			return result, err
		}
	}
	if !now.Before(d.public.ExpiresAt) && d.public.State != "PUBLISHED" && d.public.State != "DISCARDED" && d.public.State != "EXPIRED" {
		if err := r.updateSecretDraft(ctx, tx, d, "EXPIRED", nil, 0); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, querySecretDraftExpireOperations, pgx.StrictNamedArgs{"draft_id": d.id}); err != nil {
			return result, errs.ErrUnavailable
		}
		result.Draft = d.public
	}
	if input.Encrypted != nil {
		if d.encrypted != nil && !reflect.DeepEqual(input.Encrypted, d.encrypted) {
			return result, errs.ErrConflict
		}
		if d.encrypted == nil || d.public.State == "PUBLISHED" || d.public.State == "DISCARDED" || d.public.State == "EXPIRED" || d.public.State == "FAILED" {
			result.EncryptedAction = "DELETE"
		}
	}
	var encrypted, materialization any
	if result.EncryptedAction == "DELETE" {
		raw, _ := json.Marshal(input.Encrypted)
		encrypted = string(raw)
	}
	if result.MaterializationAction == "DELETE" {
		safe := *input.Materialization
		safe.DisplayHint = nil
		raw, _ := json.Marshal(safe)
		materialization = string(raw)
	}
	if _, err := tx.Exec(ctx, querySecretDraftCleanupIntent, pgx.StrictNamedArgs{"operation_id": o.id, "encrypted": encrypted, "materialization": materialization}); err != nil {
		return result, errs.ErrUnavailable
	}
	if err := r.auditSecretDraft(ctx, tx, s, *d, *o, "SUCCEEDED"); err != nil {
		return result, err
	}
	return result, nil
}

func errorsIsNotFound(err error) bool { return errors.Is(err, errs.ErrNotFound) }

// Общий scan runtime Secret разрешает D6 через того же авторитетного владельца.
func (r *Repository) recoverDraftFromLegacy(ctx context.Context, tx pgx.Tx, s scope, input repoport.RuntimeSecretRecoveryInput) (repoport.RuntimeSecretRecoveryResult, error) {
	var empty repoport.RuntimeSecretRecoveryResult
	operationRef, draftRef, err := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, "", "", "", input.OperationRef, "")
	if err != nil {
		return empty, err
	}
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, draftRef)
	if err != nil {
		return empty, err
	}
	o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, operationRef)
	if err != nil {
		return empty, err
	}
	name, err := runtimesecret.VersionedKubernetesName(d.public.SecretRef, o.targetRevision)
	m := input.Materialization
	if err != nil || o.kind != "PUBLISH" || m.Namespace != d.namespace || m.SecretName != name || m.SecretKey != runtimeSecretKey || m.ContentSHA256 != d.contentDigest {
		return empty, errs.ErrConflict
	}
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	result := repoport.RuntimeSecretRecoveryResult{Action: "KEEP", OperationState: o.state}
	if o.state != "CLAIMED" || o.lease == nil || !now.Before(*o.lease) {
		recovered, err := r.recoverSecretDraft(ctx, tx, s, &d, &o, repoport.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: o.ref, ClaimantID: o.claimantID, ClaimGeneration: o.claimGeneration, Materialization: &m}, now)
		if err != nil {
			return empty, err
		}
		result.Action, result.OperationState = recovered.MaterializationAction, recovered.State
		if result.Action == "KEEP" && o.state == "COMPLETED" {
			var snapshot entity.RuntimeSecretDraftResult
			if json.Unmarshal(o.snapshot, &snapshot) != nil {
				return empty, errs.ErrUnavailable
			}
			result.Secret = snapshot.Secret
		}
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return result, nil
}

func (r *Repository) CheckRuntimeSecretDraftWork(ctx context.Context, p value.Principal) error {
	if !validRuntimeSecretWorkPrincipal(p, "platform.runtime-secret-drafts.readiness.check") {
		return errs.ErrForbidden
	}
	var ready bool
	if r.pool.QueryRow(ctx, querySecretDraftReadiness).Scan(&ready) != nil || !ready {
		return errs.ErrUnavailable
	}
	return nil
}

func (r *Repository) ListRuntimeSecretDraftRecovery(ctx context.Context, p value.Principal, page query.Page) ([]entity.RuntimeSecretDraftWork, string, error) {
	if !validRuntimeSecretWorkPrincipal(p, "platform.runtime-secret-drafts.operations.recover") {
		return nil, "", errs.ErrForbidden
	}
	limit, err := boundedRuntimeSecretRecoveryPage(page.Size)
	if err != nil {
		return nil, "", err
	}
	if page.Token != "" && (!strings.HasPrefix(page.Token, "sdop_") || len(page.Token) > 96) {
		return nil, "", errs.ErrInvalid
	}
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return nil, "", err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, querySecretDraftRecoveryList, pgx.StrictNamedArgs{"organization_id": s.organizationID, "cursor_ref": page.Token, "page_size": limit + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	var refs [][2]string
	for rows.Next() {
		var pair [2]string
		if rows.Scan(&pair[0], &pair[1]) != nil {
			rows.Close()
			return nil, "", errs.ErrUnavailable
		}
		refs = append(refs, pair)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(refs) > int(limit) {
		refs = refs[:limit]
		next = refs[len(refs)-1][0]
	}
	var result []entity.RuntimeSecretDraftWork
	for _, pair := range refs {
		d, err := r.lockSecretDraft(ctx, tx, s.organizationID, pair[1])
		if err != nil {
			return nil, "", err
		}
		o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, pair[0])
		if err != nil {
			return nil, "", err
		}
		result = append(result, secretDraftWork(d, o))
	}
	if tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, next, nil
}
