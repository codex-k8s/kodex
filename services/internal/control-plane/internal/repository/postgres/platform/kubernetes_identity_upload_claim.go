package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesSetupExpireAttempts closes expired grants without inventing an ACK classification.
// Only explicit worker completion can classify a joined transport loss as observation-eligible.
// queryKubernetesSetupExpireAttempts закрывает expired grants без фиктивной ACK classification.
// Только явный worker completion классифицирует joined transport loss для допуска observation.
//
//go:embed sql/kubernetes_setup__expire_attempts.sql
var queryKubernetesSetupExpireAttempts string

// queryKubernetesSetupAttemptBoundary distinguishes active credit, permanent write and reported outcomes.
// queryKubernetesSetupAttemptBoundary отличает active credit, permanent write и reported outcomes.
//
//go:embed sql/kubernetes_setup__attempt_boundary.sql
var queryKubernetesSetupAttemptBoundary string

// queryKubernetesSetupClaimAttempt allocates one bounded generation under the locked setup.
// queryKubernetesSetupClaimAttempt выделяет одну bounded generation под locked setup.
//
//go:embed sql/kubernetes_setup__claim_attempt.sql
var queryKubernetesSetupClaimAttempt string

// queryKubernetesSetupWriteAdmitted records the unique WRITE admission in the same attempt transaction.
// queryKubernetesSetupWriteAdmitted сохраняет unique WRITE admission в той же attempt transaction.
//
//go:embed sql/kubernetes_setup__write_admitted.sql
var queryKubernetesSetupWriteAdmitted string

// ClaimKubernetesIdentityUpload commits a fresh fence before permitting one private storage request.
// A lost Claim response never returns WRITE again: its grant remains active until an owner closure,
// then OBSERVE additionally requires every prior outcome to be durably reported. Unreported crash
// state cannot forget a captured rejected ACK. Instance and digest are locators, never user authority.
// ClaimKubernetesIdentityUpload фиксирует fresh fence до допуска одного private storage request.
// Lost Claim response никогда не возвращает WRITE повторно: grant активен до owner closure,
// После этого OBSERVE требует durable report каждого прежнего outcome. Unreported crash state
// не забывает captured rejected ACK. Instance и digest из request — locators, не user authority.
func (repository *Repository) ClaimKubernetesIdentityUpload(ctx context.Context, principal value.Principal, input command.KubernetesIdentityUploadClaim) (result entity.KubernetesIdentityUploadAdmission, resultErr error) {
	if err := kubernetesSetupBrokerCaller(principal, "platform.runtime.kubernetes.setup.claim"); err != nil {
		return result, err
	}
	if len(input.SetupRef) > 96 || !strings.HasPrefix(input.SetupRef, "ksu_") || !managedkubernetes.ValidLocator(input.SetupRef) ||
		!managedkubernetes.ValidDigest(input.PermitSHA256) || !managedkubernetes.ValidDigest(input.SnapshotDigest) ||
		!managedkubernetes.ValidDigest(input.ContentSHA256) || len(input.WorkloadInstance) > 96 || !managedkubernetes.ValidLocator(input.WorkloadInstance) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, principal); err != nil {
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
	args := pgx.StrictNamedArgs{"setup_ref": input.SetupRef, "permit_digest": input.PermitSHA256}
	row, err := scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, args))
	if err != nil {
		return result, err
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, row.input.OrganizationID, row.connectionRef); err != nil {
		return result, err
	}
	row, err = scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, args))
	if err != nil {
		return result, err
	}
	if row.state.Terminal() || !row.now.Before(row.input.ExpiresAt) || row.digest != input.SnapshotDigest || row.input.ContentDigest != input.ContentSHA256 {
		return result, errs.ErrConflict
	}
	current, _, accessUntil, err := repository.validateKubernetesUploadOwnerTx(ctx, tx, row, source, principal.CorrelationRef)
	if err != nil {
		return result, err
	}
	var lockedID string
	var lockedVersion int64
	var lockedState enum.KubernetesIdentitySetupState
	err = tx.QueryRow(ctx, queryKubernetesSetupLockOwned, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "actor_id": row.input.ActorID,
		"connection_ref": row.connectionRef, "setup_ref": row.input.SetupRef,
	}).Scan(&lockedID, &lockedVersion, &lockedState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) || lockedID != row.id || lockedVersion != row.version || lockedState != row.state {
		return result, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryKubernetesSetupExpireAttempts, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id,
	}); err != nil {
		return result, errs.ErrUnavailable
	}
	var active, wrote, reported bool
	if tx.QueryRow(ctx, queryKubernetesSetupAttemptBoundary, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id,
	}).Scan(&active, &wrote, &reported) != nil {
		return result, errs.ErrUnavailable
	}
	if active {
		return result, errs.ErrConflict
	}
	if wrote && !reported {
		// Preserve expired grant closure without manufacturing a transport outcome or fresh credit.
		// Сохраняем expired grant closure без вымышленного transport outcome либо fresh credit.
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return result, errs.ErrConflict
	}
	if !wrote && row.state != enum.KubernetesIdentitySetupPrepared || wrote && row.state == enum.KubernetesIdentitySetupPrepared {
		return result, errs.ErrUnavailable
	}
	known, err := kubernetesKnownMaterialTx(ctx, tx, row, source.registry.IdentityNamespace)
	if err != nil || !wrote && known != nil {
		return result, errs.ErrUnavailable
	}
	mode := "OBSERVE"
	if !wrote {
		mode = "WRITE"
	}
	attemptRef, err := newRef("ksa")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	fence, err := newRef("kuf")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	fenceHash := sha256.Sum256([]byte(fence))
	result.SetupRef, result.Fence, result.ReconcileOnly, result.KnownIdentity = row.input.SetupRef, fence, wrote, known
	if accessDeadlineExpired(accessUntil) {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrForbidden
	}
	var created time.Time
	started := time.Now()
	err = tx.QueryRow(ctx, queryKubernetesSetupClaimAttempt, pgx.StrictNamedArgs{
		"ref": attemptRef, "organization_id": row.input.OrganizationID, "setup_id": row.id, "mode": mode,
		"instance": input.WorkloadInstance, "fence_digest": hex.EncodeToString(fenceHash[:]), "access_until": nullableAccessDeadline(accessUntil),
	}).Scan(&result.AttemptRef, &result.Generation, &result.ExpiresAt, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrConflict
	}
	if err != nil {
		return entity.KubernetesIdentityUploadAdmission{}, mapWriteError(err)
	}
	if created.IsZero() || !result.ExpiresAt.After(created) || result.ExpiresAt.After(row.input.ExpiresAt) {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrUnavailable
	}
	if !wrote {
		tag, err := tx.Exec(ctx, queryKubernetesSetupWriteAdmitted, pgx.StrictNamedArgs{
			"organization_id": row.input.OrganizationID, "setup_id": row.id, "version": row.version,
		})
		if err != nil || tag.RowsAffected() != 1 {
			return entity.KubernetesIdentityUploadAdmission{}, errs.ErrUnavailable
		}
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef,
		current.organizationID, nil, current.actorID, "platform.runtime.kubernetes.setup.claim", "INTEGRATION_CONNECTION", row.connectionRef,
		"i18n:KUBERNETES_IDENTITY_UPLOAD_CLAIMED", current.correlationRef); err != nil {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrUnavailable
	}
	until := earlierAccessDeadline(accessUntil, started.Add(result.ExpiresAt.Sub(created)))
	if err := commitIdentitySetupAdmission(ctx, tx, until, result.ExpiresAt); err != nil {
		return entity.KubernetesIdentityUploadAdmission{}, err
	}
	result.ExpiresAt = earlierAccessDeadline(result.ExpiresAt, until).UTC()
	result.Provision = value.ManagedKubernetesIdentityProvision{IdentityRef: row.input.IdentityRef, TargetRef: row.input.TargetRef,
		Namespace: source.registry.IdentityNamespace, Revision: row.input.IdentityRevision, ContentSHA256: row.input.ContentDigest}
	return result, nil
}
