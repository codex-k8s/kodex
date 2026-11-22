package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querySSHSetupCompletionSource reads the immutable original owner only through the exact durable attempt fence; a late receipt does not need a still-active browser actor.
// querySSHSetupCompletionSource читает immutable original owner лишь через exact durable attempt fence; late receipt не требует still-active browser actor.
//
//go:embed sql/ssh_setup__completion_source.sql
var querySSHSetupCompletionSource string

// querySSHSetupCompletionLock retains Connection→binding→setup→attempt lock order even after revocation, so late facts can be recorded without granting new execution authority.
// querySSHSetupCompletionLock сохраняет Connection→binding→setup→attempt lock order после revoke для late facts без новой execution authority.
//
//go:embed sql/ssh_setup__completion_lock.sql
var querySSHSetupCompletionLock string

// querySSHSetupCompletionView returns safe metadata to the already fenced broker after recording evidence; it is not a public visibility or credential-selection query.
// querySSHSetupCompletionView возвращает safe metadata already fenced broker после evidence; это не public visibility либо credential-selection query.
//
//go:embed sql/ssh_setup__completion_view.sql
var querySSHSetupCompletionView string

// querySSHSetupObservation retains one exact receipt with its original database timestamp across retries and never upgrades an absent completion by lease expiry.
// querySSHSetupObservation сохраняет exact receipt с original database timestamp при retry и не повышает absent completion по lease expiry.
//
//go:embed sql/ssh_setup__observation.sql
var querySSHSetupObservation string

// querySSHSetupCompletionReceipt recognizes an exact historical receipt before any current-state transition, so replay cannot close a newer observation attempt or refresh its authority.
// querySSHSetupCompletionReceipt распознаёт exact historical receipt до current-state transition, чтобы replay не закрывал новую observation attempt и не обновлял authority.
//
//go:embed sql/ssh_setup__completion_receipt.sql
var querySSHSetupCompletionReceipt string

// querySSHSetupRecordMaterial keeps the first known UID/resourceVersion immutable while returning existing pins for explicit contradiction detection.
// querySSHSetupRecordMaterial сохраняет first known UID/resourceVersion неизменными и возвращает existing pins для explicit contradiction detection.
//
//go:embed sql/ssh_setup__record_material.sql
var querySSHSetupRecordMaterial string

// querySSHSetupRecordRejection persists damaged successful acknowledgements separately from transport ambiguity, including a known UID witness when available.
// querySSHSetupRecordRejection сохраняет damaged successful acknowledgements отдельно от transport ambiguity, включая known UID witness.
//
//go:embed sql/ssh_setup__record_rejection.sql
var querySSHSetupRecordRejection string

// querySSHSetupCompleteState closes all outstanding setup attempts with a version fence while keeping the immutable receipt history independent of current grant state.
// querySSHSetupCompleteState закрывает outstanding setup attempts с version fence, сохраняя immutable receipt history отдельно от current grant state.
//
//go:embed sql/ssh_setup__complete_state.sql
var querySSHSetupCompleteState string

// querySSHSetupQuarantine blocks only the contradicted original revision and revokes a binding or shared source only if it still points to that revision.
// querySSHSetupQuarantine блокирует лишь contradicted original revision и отзывает binding либо shared source только при ссылке на эту revision.
//
//go:embed sql/ssh_setup__quarantine.sql
var querySSHSetupQuarantine string

// sshUploadCompletionReceipt contains explicit private outcome coordinates for durable comparison, without including the raw fence, key bytes or caller-supplied actor fields.
// sshUploadCompletionReceipt содержит explicit private outcome coordinates для durable comparison без raw fence, key bytes либо caller-supplied actor fields.
type sshUploadCompletionReceipt struct {
	Outcome  string
	Identity *managedssh.IdentityLocator
	Rejected *command.SSHIdentityRejectedAcknowledgement
}

// String hides private storage coordinates and acknowledgement witnesses when an entire receipt is formatted.
// String скрывает private storage coordinates и acknowledgement witnesses при форматировании всего receipt.
func (sshUploadCompletionReceipt) String() string { return "[SSH private completion receipt]" }

// GoString enforces the same privacy boundary for detailed Go diagnostics.
// GoString сохраняет ту же privacy boundary для подробных Go diagnostics.
func (receipt sshUploadCompletionReceipt) GoString() string { return receipt.String() }

// sshCompletionReceipt validates the closed outcome union again at the persistence boundary and produces exact canonical bytes for append-only evidence replay.
// sshCompletionReceipt повторно проверяет closed outcome union на persistence boundary и выдаёт exact canonical bytes для append-only evidence replay.
func sshCompletionReceipt(input command.SSHIdentityUploadComplete) (sshUploadCompletionReceipt, string, string, error) {
	var receipt sshUploadCompletionReceipt
	count := 0
	if input.Unknown {
		count++
		receipt.Outcome = "UNKNOWN"
	}
	if input.Identity != nil {
		if !input.Identity.Valid() {
			return receipt, "", "", errs.ErrInvalid
		}
		count++
		receipt.Outcome = "KNOWN"
		receipt.Identity = input.Identity
	}
	if input.Rejected != nil {
		if !managedssh.Digest(input.Rejected.ResponseSHA256) || len(input.Rejected.ObservedUID) > 256 || !utf8.ValidString(input.Rejected.ObservedUID) {
			return receipt, "", "", errs.ErrInvalid
		}
		count++
		receipt.Outcome = "ACK_REJECTED"
		receipt.Rejected = input.Rejected
	}
	if count != 1 {
		return receipt, "", "", errs.ErrInvalid
	}
	raw, err := json.Marshal(receipt)
	if err != nil || len(raw) > 8192 {
		return receipt, "", "", errs.ErrInvalid
	}
	hash := sha256.Sum256(raw)
	return receipt, string(raw), hex.EncodeToString(hash[:]), nil
}

// sshUploadProvision derives the sole immutable material tuple from owner state and the broker authentication hash pinned by Claim, never from a completion locator.
// sshUploadProvision выводит sole immutable material tuple из owner state и broker authentication hash Claim, не из completion locator.
func sshUploadProvision(row sshUploadRow) managedssh.IdentityProvision {
	return managedssh.IdentityProvision{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: row.input.IdentityRef, TargetRef: row.input.TargetRef, Revision: row.input.IdentityRevision, Namespace: row.input.Namespace, ContentSHA256: row.input.ContentDigest, AuthenticationSHA256: row.authenticationDigest}
}

// CompleteSSHIdentityUpload retains fenced receipts even after cancellation or expiry; only a fresh current-owner attachment may mark COMPLETED, while contradictory successful evidence permanently quarantines the original revision.
// CompleteSSHIdentityUpload сохраняет fenced receipts после cancel либо expiry; лишь fresh current-owner attachment даёт COMPLETED, contradictory successful evidence навсегда quarantines original revision.
func (repository *Repository) CompleteSSHIdentityUpload(ctx context.Context, principal value.Principal, input command.SSHIdentityUploadComplete) (entity.SSHIdentitySetupView, error) {
	var result entity.SSHIdentitySetupView
	if err := sshSetupBrokerCaller(principal, "platform.runtime.ssh.setup.complete"); err != nil {
		return result, err
	}
	if !sshPurposeReference(input.SetupRef, "ssu_") || !sshPurposeReference(input.AttemptRef, "ssa_") || input.Generation < 1 || input.Generation > 8 || !managedssh.OwnerReference(input.Fence) || !managedssh.OwnerReference(input.WorkloadInstance) {
		return result, errs.ErrInvalid
	}
	receipt, body, digest, err := sshCompletionReceipt(input)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256([]byte(input.Fence))
	sourceArgs := pgx.StrictNamedArgs{"setup_ref": input.SetupRef, "attempt_ref": input.AttemptRef, "generation": input.Generation, "instance": input.WorkloadInstance, "fence_digest": hex.EncodeToString(hash[:])}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := scanSSHUploadRow(tx.QueryRow(ctx, querySSHSetupCompletionSource, sourceArgs))
	if err != nil {
		return result, err
	}
	var attemptID, state string
	var mode enum.SSHIdentityUploadMode
	var expires, now time.Time
	err = tx.QueryRow(ctx, querySSHSetupCompletionLock, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "connection_id": row.input.ConnectionID, "binding_id": row.input.BindingID, "setup_id": row.id, "attempt_ref": input.AttemptRef, "generation": input.Generation, "instance": input.WorkloadInstance, "fence_digest": sourceArgs["fence_digest"]}).Scan(&attemptID, &mode, &state, &expires, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrConflict
	}
	if err != nil || !mode.Valid() || (state != "ACTIVE" && state != "CLOSED") || expires.IsZero() || now.IsZero() {
		return result, errs.ErrUnavailable
	}
	row, err = scanSSHUploadRow(tx.QueryRow(ctx, querySSHSetupCompletionSource, sourceArgs))
	if err != nil {
		return result, err
	}
	if !sshUploadProvision(row).Valid() || (receipt.Identity != nil && receipt.Identity.Provision() != sshUploadProvision(row)) {
		return result, errs.ErrConflict
	}
	var previous string
	err = tx.QueryRow(ctx, querySSHSetupCompletionReceipt, pgx.StrictNamedArgs{"attempt_id": attemptID, "outcome": receipt.Outcome, "receipt_digest": digest}).Scan(&previous)
	if err == nil {
		if previous != body {
			return result, errs.ErrUnavailable
		}
		result, err = readSSHSetupView(tx.QueryRow(ctx, querySSHSetupCompletionView, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id}))
		if err != nil {
			return result, err
		}
		if tx.Commit(ctx) != nil {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	info, err := readSSHRecoveryEligibility(ctx, tx, row)
	if err != nil {
		return result, err
	}
	var recorded time.Time
	if tx.QueryRow(ctx, querySSHSetupObservation, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id, "attempt_id": attemptID, "outcome": receipt.Outcome, "receipt_digest": digest, "receipt_body": body}).Scan(&recorded) != nil {
		return result, errs.ErrUnavailable
	}
	next, reason := row.state, ""
	if receipt.Rejected != nil {
		if _, err = tx.Exec(ctx, querySSHSetupRecordRejection, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id, "attempt_id": attemptID, "identity_id": row.input.IdentityID, "response_digest": receipt.Rejected.ResponseSHA256, "truncated": receipt.Rejected.Truncated, "uid": receipt.Rejected.ObservedUID}); err != nil {
			return result, errs.ErrUnavailable
		}
		next, reason = enum.SSHIdentitySetupAcknowledgementRejected, "ACK_REJECTED"
		if info.known {
			next, reason = enum.SSHIdentitySetupQuarantined, "QUARANTINED"
		}
	} else if receipt.Identity != nil {
		// A rejection is terminal even when a later lookup returns a syntactically valid object.
		// Rejection остаётся terminal, даже если поздний lookup возвращает syntactically valid object.
		if info.rejected {
			next, reason = enum.SSHIdentitySetupAcknowledgementRejected, "ACK_REJECTED"
		} else {
			stored := *receipt.Identity
			err = tx.QueryRow(ctx, querySSHSetupRecordMaterial, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "connection_id": row.input.ConnectionID, "identity_id": row.input.IdentityID, "setup_id": row.id, "attempt_id": attemptID, "namespace": stored.Namespace, "name": stored.SecretName, "uid": stored.SecretUID, "resource_version": stored.SecretResourceVersion, "content_digest": stored.ContentSHA256, "authentication_digest": stored.AuthenticationSHA256}).Scan(&stored.Namespace, &stored.SecretName, &stored.SecretUID, &stored.SecretResourceVersion, &stored.ContentSHA256, &stored.AuthenticationSHA256)
			if err != nil {
				return result, errs.ErrUnavailable
			}
			if stored != *receipt.Identity {
				next, reason = enum.SSHIdentitySetupQuarantined, "QUARANTINED"
			} else if row.state != enum.SSHIdentitySetupCompleted {
				if row.state.Terminal() {
					next, reason = row.state, "MATERIAL_RECORDED_NO_ATTACHMENT"
				} else {
					next, reason = enum.SSHIdentitySetupCancelled, "MATERIAL_RECORDED_NO_ATTACHMENT"
				}
				if !row.state.Terminal() && state == "ACTIVE" && now.Before(expires) && now.Before(row.input.ExpiresAt) {
					// A definitive denial rolls back only attachment; infrastructure failure aborts the entire owner transaction without manufacturing a terminal state or receipt credit.
					// Definitive denial откатывает лишь attachment; infrastructure failure отменяет всю owner transaction без изготовления terminal state либо receipt credit.
					save, saveErr := tx.Begin(ctx)
					if saveErr != nil {
						return result, errs.ErrUnavailable
					}
					current, accessUntil, ownerErr := repository.validateSSHUploadOwnerTx(ctx, save, row, principal.CorrelationRef)
					if ownerErr == nil {
						var tag pgconn.CommandTag
						tag, ownerErr = save.Exec(ctx, querySSHSetupCompleteState, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id, "version": row.version, "state": "COMPLETED", "reason": "COMPLETED", "authorized_until": earlierAccessDeadline(expires, accessUntil)})
						if ownerErr == nil && tag.RowsAffected() != 1 {
							ownerErr = errs.ErrConflict
						}
					}
					if ownerErr == nil {
						_, ownerErr = repository.attachIntegrationBrokerIdentityRevisionTx(ctx, save, current, row.owner, row.input.IdentityID, accessUntil)
					}
					if ownerErr == nil {
						if save.Commit(ctx) != nil {
							return result, errs.ErrUnavailable
						}
					} else {
						if save.Rollback(ctx) != nil || !brokerIdentitySetupAuthorityLost(ownerErr) {
							return result, errs.ErrUnavailable
						}
					}
					if ownerErr == nil {
						next, reason = enum.SSHIdentitySetupCompleted, ""
					}
				}
			}
		}
	} else if !row.state.Terminal() {
		next, reason = enum.SSHIdentitySetupCancelled, "OWNER_REVOKED"
		if recorded.Before(expires) && recorded.Before(row.input.ExpiresAt) && state == "ACTIVE" {
			if _, _, ownerErr := repository.validateSSHUploadOwnerTx(ctx, tx, row, principal.CorrelationRef); ownerErr == nil {
				next, reason = enum.SSHIdentitySetupUnknown, "MATERIAL_UNKNOWN"
			} else if !brokerIdentitySetupAuthorityLost(ownerErr) {
				return result, errs.ErrUnavailable
			}
		} else {
			next, reason = enum.SSHIdentitySetupExpired, "SETUP_EXPIRED_WITH_MATERIAL_UNRESOLVED"
		}
	}
	if row.state == enum.SSHIdentitySetupQuarantined {
		next, reason = row.state, "QUARANTINED"
	}
	if next == enum.SSHIdentitySetupQuarantined {
		if _, err = tx.Exec(ctx, querySSHSetupQuarantine, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "connection_id": row.input.ConnectionID, "identity_id": row.input.IdentityID, "setup_id": row.id}); err != nil {
			return result, errs.ErrUnavailable
		}
		closing := scope{organizationID: row.input.OrganizationID, actorID: row.input.ActorID, correlationRef: principal.CorrelationRef}
		if err := repository.closeSSHExecutionDependenciesTx(ctx, tx, closing, row.input.ConnectionRef, "", row.input.IdentityID, ""); err != nil {
			return result, err
		}
	}
	if reason != "" {
		tag, writeErr := tx.Exec(ctx, querySSHSetupCompleteState, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id, "version": row.version, "state": string(next), "reason": reason, "authorized_until": expires})
		if writeErr != nil || tag.RowsAffected() != 1 {
			return result, errs.ErrUnavailable
		}
	}
	result, err = readSSHSetupView(tx.QueryRow(ctx, querySSHSetupCompletionView, pgx.StrictNamedArgs{"organization_id": row.input.OrganizationID, "setup_id": row.id}))
	if err != nil {
		return result, err
	}
	auditRef, auditErr := newRef("aud")
	if auditErr != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, row.input.OrganizationID, nil, row.input.ActorID, "platform.integrations.ssh.setup.complete", "INTEGRATION_CONNECTION", row.input.ConnectionRef, "i18n:SSH_IDENTITY_SETUP_EVIDENCE_RECORDED", principal.CorrelationRef); err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	return result, nil
}
