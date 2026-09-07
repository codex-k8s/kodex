package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/proof_worker_grant_accept_generation.sql
	queryWorkerGrantAcceptGeneration string
	//go:embed sql/proof_worker_grant_accept_instance.sql
	queryWorkerGrantAcceptInstance string
)

func (repository *Repository) acceptWorkerGrantInstance(ctx context.Context, input platformrepo.WorkerGrantInput) error {
	instance, err := uuid.Parse(input.InstanceID)
	digest, digestErr := hex.DecodeString(input.EnvelopeSHA256)
	if err != nil || instance == uuid.Nil || instance.String() != input.InstanceID ||
		digestErr != nil || len(digest) != 32 || strings.ToLower(input.EnvelopeSHA256) != input.EnvelopeSHA256 ||
		input.IssuedAt.Unix() <= 0 || input.Revision != uint64(input.IssuedAt.Unix()) {
		return errs.ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	// Общая строка блокирует также прежних v1 consumers. Новый instance не
	// обходит generation floor; его revision не инвалидирует соседние Pod.
	var generation uint64
	err = tx.QueryRow(ctx, queryWorkerGrantAcceptGeneration, input.WorkloadID,
		input.CredentialGeneration, input.IssuedAt.UTC(), input.ExpiresAt.UTC()).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if generation != input.CredentialGeneration {
		return errs.ErrConflict
	}
	var accepted uint64
	err = tx.QueryRow(ctx, queryWorkerGrantAcceptInstance, input.WorkloadID, instance,
		input.CredentialGeneration, input.Revision, input.IssuedAt.UTC(), input.ExpiresAt.UTC(), input.EnvelopeSHA256).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if accepted != input.Revision {
		return errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
