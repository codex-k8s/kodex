package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHInvocationLocator resolves a tenant-bound Connection before acquiring the owner lock; the locator grants no eligibility.
// querySSHInvocationLocator разрешает tenant-bound Connection до owner lock; locator не выдаёт eligibility.
//
//go:embed sql/ssh_invocation__locator.sql
var querySSHInvocationLocator string

// querySSHInvocationSource rereads the immutable invocation pin against current custody, actual health and quarantine state without selecting a successor identity.
// querySSHInvocationSource перечитывает immutable invocation pin относительно current custody, actual health и quarantine без выбора successor identity.
//
//go:embed sql/ssh_invocation__source.sql
var querySSHInvocationSource string

// querySSHInvocationCursor serializes one organization's bounded fair scan before taking Connection and invocation locks.
// querySSHInvocationCursor сериализует bounded fair scan организации до Connection и invocation locks.
//
//go:embed sql/ssh_invocation__cursor.sql
var querySSHInvocationCursor string

// querySSHInvocationCandidates discovers at most thirty-two original intents without assigning authority or touching a lease.
// querySSHInvocationCandidates обнаруживает не более тридцати двух original intents без authority либо изменения lease.
//
//go:embed sql/ssh_invocation__candidates.sql
var querySSHInvocationCandidates string

// querySSHInvocationCursorAdvance retains forward progress even when an ineligible prefix exceeds the requested batch.
// querySSHInvocationCursorAdvance сохраняет progress, даже когда ineligible prefix превышает requested batch.
//
//go:embed sql/ssh_invocation__cursor_advance.sql
var querySSHInvocationCursorAdvance string

// querySSHInvocationDenyUnstarted settles only a never-leased original SSH intent with no dispatch or open Gate; it cannot classify an uncertain external effect.
// querySSHInvocationDenyUnstarted завершает лишь never-leased original SSH intent без dispatch либо open Gate; uncertain external effect не классифицируется.
//
//go:embed sql/ssh_invocation__deny_unstarted.sql
var querySSHInvocationDenyUnstarted string

// sshInvocationSource contains only the exact already-pinned private material and finite owner deadline; it cannot redirect an invocation to another identity.
// sshInvocationSource содержит лишь exact already-pinned private material и finite owner deadline; перенаправить invocation на другую identity нельзя.
type sshInvocationSource struct {
	identityID, connectionRef, subjectDigest string
	identity                                 entity.IntegrationBrokerIdentityRevision
	material                                 managedssh.IdentityLocator
	mode                                     managedssh.TransportMode
	validUntil                               time.Time
}

// currentSSHInvocationSourceTx locks the originating Connection before checking all custody pins, preserving infrastructure failure instead of converting it into denial or selecting a new credential.
// currentSSHInvocationSourceTx блокирует originating Connection до проверки custody pins, сохраняя infrastructure failure вместо denial либо выбора нового credential.
func (repository *Repository) currentSSHInvocationSourceTx(ctx context.Context, tx pgx.Tx, organizationID, ref string) (sshInvocationSource, error) {
	var source sshInvocationSource
	args := pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_ref": ref}
	err := tx.QueryRow(ctx, querySSHInvocationLocator, args).Scan(&source.connectionRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, errs.ErrNotFound
	}
	if err != nil {
		return source, errs.ErrUnavailable
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, organizationID, source.connectionRef); err != nil {
		return source, err
	}
	err = tx.QueryRow(ctx, querySSHInvocationSource, args).Scan(&source.identityID, &source.identity.Ref, &source.identity.Revision,
		&source.identity.TargetRef, &source.mode, &source.subjectDigest, &source.validUntil,
		&source.material.Namespace, &source.material.SecretName, &source.material.SecretUID, &source.material.SecretResourceVersion,
		&source.material.ContentSHA256, &source.material.AuthenticationSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, errs.ErrForbidden
	}
	if err != nil {
		return source, errs.ErrUnavailable
	}
	source.identity.Purpose = entity.IntegrationBrokerIdentitySSH
	source.material.Purpose, source.material.IdentityRevisionRef = managedssh.IdentityPurpose, source.identity.Ref
	source.material.Revision, source.material.TargetRef = source.identity.Revision, source.identity.TargetRef
	if !source.material.Valid() || !managedssh.Digest(source.subjectDigest) ||
		(source.mode != managedssh.ManagedTransport && source.mode != managedssh.ExecuteTransport) {
		return source, errs.ErrUnavailable
	}
	return source, nil
}

// claimManagedSSHInvocationsTx adopts only original SSH_TARGET intents through the canonical owner candidate/lease pipeline in the caller's transaction; it neither commits nor remints the principal or forks the credential selector.
// claimManagedSSHInvocationsTx принимает лишь original SSH_TARGET intents через canonical owner candidate/lease pipeline в caller transaction; commit, remint principal и fork credential selector отсутствуют.
func (repository *Repository) claimManagedSSHInvocationsTx(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, instance string, limit int32, route string) ([]map[string]any, error) {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.runtime.integrations.claim" || principal.ProjectRef != "" ||
		!managedssh.OwnerReference(instance) || limit < 1 || limit > 4 || route != "MANAGED_MCP" {
		return nil, errs.ErrForbidden
	}
	var after time.Time
	var afterRef string
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID}
	if err := tx.QueryRow(ctx, querySSHInvocationCursor, args).Scan(&after, &afterRef); err != nil {
		return nil, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases, current.organizationID, principal.CallerWorkload); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, querySSHInvocationCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "after_created_at": after, "after_ref": afterRef})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// position retains discovery order only; eligibility always comes from a fresh locked owner read.
	// position хранит только discovery order; eligibility всегда приходит из fresh locked owner read.
	type position struct {
		ref     string
		created time.Time
	}
	positions := make([]position, 0, 32)
	for rows.Next() {
		var candidate position
		if rows.Scan(&candidate.ref, &candidate.created) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		positions = append(positions, candidate)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	claims := make([]map[string]any, 0, limit)
	processed := 0
	for _, discovered := range positions {
		after, afterRef = discovered.created, discovered.ref
		processed++
		source, err := repository.currentSSHInvocationSourceTx(ctx, tx, current.organizationID, discovered.ref)
		if accessInfrastructureFailure(err) {
			return nil, err
		}
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			if err := repository.denyUnstartedSSHInvocationTx(ctx, tx, current, discovered.ref); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var candidate integrationInvocationCandidate
		err = scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState,
			current.organizationID, 1, principal.CallerWorkload, route, false, discovered.ref, false, true, true), &candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		claim, err := repository.claimSSHInvocationCandidateTx(ctx, tx, current, principal, instance, candidate)
		if accessInfrastructureFailure(err) {
			return nil, err
		}
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			if err := repository.denyUnstartedSSHInvocationTx(ctx, tx, current, discovered.ref); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if claim != nil {
			claim["credentialDeliveryKind"], claim["brokerIdentityPurpose"] = "BROKER_IDENTITY", managedssh.IdentityPurpose
			claim["brokerIdentityRef"], claim["brokerIdentityRevision"] = source.identity.Ref, source.identity.Revision
			claims = append(claims, claim)
		}
		if len(claims) == int(limit) {
			break
		}
	}
	if len(positions) < 32 && processed == len(positions) {
		after, afterRef = time.Unix(0, 0).UTC(), ""
	}
	if _, err := tx.Exec(ctx, querySSHInvocationCursorAdvance, pgx.StrictNamedArgs{"organization_id": current.organizationID, "after_created_at": after, "after_ref": afterRef}); err != nil {
		return nil, errs.ErrUnavailable
	}
	return claims, nil
}

// claimSSHInvocationCandidateTx isolates canonical admission writes in a savepoint so a known actor denial can be settled without retaining a partial lease; infrastructure errors still abort the caller's transaction.
// claimSSHInvocationCandidateTx изолирует canonical admission writes в savepoint, позволяя завершить known actor denial без partial lease; infrastructure errors по-прежнему отменяют caller transaction.
func (repository *Repository) claimSSHInvocationCandidateTx(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, instance string, candidate integrationInvocationCandidate) (result map[string]any, resultErr error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, savepoint, &resultErr)
	claim, err := repository.claimIntegrationInvocationCandidateTx(ctx, savepoint, current, principal, instance, false, candidate)
	if err != nil || claim == nil {
		return nil, err
	}
	if err := savepoint.Commit(ctx); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return claim, nil
}

// denyUnstartedSSHInvocationTx records the canonical no-effect failure, continuation and event inside the same owner transaction; existing Gates and attempted executions remain untouched.
// denyUnstartedSSHInvocationTx сохраняет canonical no-effect failure, continuation и event в той же owner transaction; existing Gates и attempted executions не изменяются.
func (repository *Repository) denyUnstartedSSHInvocationTx(ctx context.Context, tx pgx.Tx, current scope, ref string) error {
	var id, project, root, node string
	err := tx.QueryRow(ctx, querySSHInvocationDenyUnstarted, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": ref}).Scan(&id, &project, &root, &node)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, id, project); err != nil {
		return err
	}
	_, err = repository.emitIntegrationActionEvent(ctx, tx, current, project, root, ref, node, "FAILED")
	return err
}
