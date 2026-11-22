package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPersonalCredentialTestAccessSubject resolves only the immutable job's original active subject within its tenant, never the worker or a caller-selected actor.
// queryPersonalCredentialTestAccessSubject разрешает лишь исходного active subject immutable job внутри tenant, а не worker либо actor из payload.
//
//go:embed sql/personal_credential_test__access_subject.sql
var queryPersonalCredentialTestAccessSubject string

// personalCredentialProbeAccessUntil preserves the existing integration.view rule and derives its finite bound through the canonical access evaluator.
// personalCredentialProbeAccessUntil сохраняет прежнее правило integration.view и выводит finite bound каноническим access evaluator.
func (repository *Repository) personalCredentialProbeAccessUntil(ctx context.Context, tx pgx.Tx, organizationID string, job personalCredentialWorkJob) (time.Time, error) {
	owner := scope{organizationID: organizationID, actorID: job.subjectID}
	err := tx.QueryRow(ctx, queryPersonalCredentialTestAccessSubject, pgx.StrictNamedArgs{"organization_id": organizationID, "subject_id": job.subjectID}).Scan(&owner.actorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errs.ErrNotFound
	}
	if err != nil {
		return time.Time{}, errs.ErrUnavailable
	}
	return repository.requireAccessUntil(ctx, tx, owner, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: job.connectionRef})
}
