package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHSetupPrivateLock serializes one immutable setup after the current Connection and credential binding locks; the permit reader never acquires locks in the opposite order.
// querySSHSetupPrivateLock сериализует immutable setup после current Connection и credential binding locks; permit reader не берёт locks в обратном порядке.
//
//go:embed sql/ssh_setup__private_lock.sql
var querySSHSetupPrivateLock string

// querySSHSetupClaim spends an explicit owner-selected generation before storage dispatch, retaining expired intents without manufacturing transport outcomes.
// querySSHSetupClaim расходует explicit owner-selected generation до storage dispatch и сохраняет expired intents без изготовления transport outcomes.
//
//go:embed sql/ssh_setup__claim.sql
var querySSHSetupClaim string

// lockSSHUploadSetupTx acquires the final setup lock only after current owner admission, then rereads all canonical input and authority coordinates under that lock.
// lockSSHUploadSetupTx берёт final setup lock лишь после current owner admission и повторно читает canonical input и authority coordinates под этим lock.
func (repository *Repository) lockSSHUploadSetupTx(ctx context.Context, tx pgx.Tx, input command.SSHIdentityUploadResolve, correlation string) (sshUploadRow, error) {
	args := pgx.StrictNamedArgs{"setup_ref": input.SetupRef, "permit_digest": input.PermitSHA256}
	row, err := scanSSHUploadRow(tx.QueryRow(ctx, querySSHSetupPrivatePermit, args))
	if err != nil {
		return row, err
	}
	_, initialUntil, err := repository.validateSSHUploadOwnerTx(ctx, tx, row, correlation)
	if err != nil {
		return sshUploadRow{}, err
	}
	var id string
	if tx.QueryRow(ctx, querySSHSetupPrivateLock, pgx.StrictNamedArgs{"setup_id": row.id, "organization_id": row.input.OrganizationID}).Scan(&id) != nil || id != row.id {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	row, err = scanSSHUploadRow(tx.QueryRow(ctx, querySSHSetupPrivatePermit, args))
	if err != nil {
		return row, err
	}
	_, currentUntil, err := repository.validateSSHUploadOwnerTx(ctx, tx, row, correlation)
	if err != nil {
		return sshUploadRow{}, err
	}
	row.accessUntil = earlierAccessDeadline(initialUntil, currentUntil)
	return row, nil
}

// ClaimSSHIdentityUpload commits the sole WRITE intent or a receipt-backed OBSERVE before returning a private fence; it neither replays a raw fence nor turns an unreported write into observation authority.
// ClaimSSHIdentityUpload фиксирует sole WRITE intent либо receipt-backed OBSERVE до возврата private fence; raw fence не повторяется, unreported write не создаёт observation authority.
func (repository *Repository) ClaimSSHIdentityUpload(ctx context.Context, principal value.Principal, input command.SSHIdentityUploadClaim) (entity.SSHIdentityUploadAdmission, error) {
	var result entity.SSHIdentityUploadAdmission
	if err := sshSetupBrokerCaller(principal, "platform.runtime.ssh.setup.claim"); err != nil {
		return result, err
	}
	if !sshPurposeReference(input.SetupRef, "ssu_") || !managedssh.Digest(input.PermitSHA256) || !managedssh.Digest(input.SnapshotDigest) || !managedssh.Digest(input.Key.ContentSHA256) || !managedssh.Digest(input.Key.AuthenticationSHA256) || !managedssh.OwnerReference(input.WorkloadInstance) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := repository.lockSSHUploadSetupTx(ctx, tx, command.SSHIdentityUploadResolve{SetupRef: input.SetupRef, PermitSHA256: input.PermitSHA256}, principal.CorrelationRef)
	if err != nil {
		return result, err
	}
	if row.digest != input.SnapshotDigest || row.input.ContentDigest != input.Key.ContentSHA256 {
		return result, errs.ErrConflict
	}
	info, err := readSSHRecoveryEligibility(ctx, tx, row)
	if err != nil {
		return result, err
	}
	mode, err := info.mode(row)
	if err != nil {
		return result, err
	}
	if mode == enum.SSHIdentityUploadObserve && row.authenticationDigest != input.Key.AuthenticationSHA256 {
		return result, errs.ErrConflict
	}
	provision, ok := (managedssh.IdentityPreparation{IdentityRevisionRef: row.input.IdentityRef, TargetRef: row.input.TargetRef, Revision: row.input.IdentityRevision, Namespace: row.input.Namespace, ContentSHA256: row.input.ContentDigest}).Provision(input.Key)
	if !ok {
		return result, errs.ErrConflict
	}
	attempt, err := newRef("ssa")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return result, errs.ErrUnavailable
	}
	defer clear(raw)
	fence := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(fence))
	var expires, created time.Time
	started := time.Now()
	if accessDeadlineExpired(row.accessUntil) {
		return result, errs.ErrForbidden
	}
	err = tx.QueryRow(ctx, querySSHSetupClaim, pgx.StrictNamedArgs{"setup_id": row.id, "organization_id": row.input.OrganizationID, "version": row.version, "authentication_digest": input.Key.AuthenticationSHA256, "attempt_ref": attempt, "generation": info.generation + 1, "mode": string(mode), "instance": input.WorkloadInstance, "fence_digest": hex.EncodeToString(hash[:]), "snapshot_digest": row.digest, "access_until": nullableAccessDeadline(row.accessUntil)}).Scan(&expires, &created)
	if err != nil {
		return result, mapWriteError(err)
	}
	if created.IsZero() || !expires.After(created) || !expires.After(row.now) || expires.After(row.input.ExpiresAt) {
		return result, errs.ErrUnavailable
	}
	until := earlierAccessDeadline(row.accessUntil, started.Add(expires.Sub(created)))
	if err := commitIdentitySetupAdmission(ctx, tx, until, expires); err != nil {
		return result, err
	}
	return entity.SSHIdentityUploadAdmission{SetupRef: row.input.SetupRef, SnapshotDigest: row.digest, AttemptRef: attempt, WorkloadInstance: input.WorkloadInstance, Generation: info.generation + 1, Fence: fence, ExpiresAt: earlierAccessDeadline(expires, until).UTC(), Mode: mode, Provision: provision}, nil
}
