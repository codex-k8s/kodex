package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/service_credential_generation.sql
	queryServiceCredentialGeneration string
	//go:embed sql/trusted_workload__generation.sql
	queryTrustedWorkloadGeneration string
	//go:embed sql/proof_worker_grant_accept_generation.sql
	queryWorkerGrantAcceptGeneration string
	//go:embed sql/proof_worker_grant_accept_instance.sql
	queryWorkerGrantAcceptInstance string
)

// ConfigureRPCProfile вызывается composition root до публикации repository.
// Профиль не берётся из запроса и не использует fallback после отказа допуска.
func (repository *Repository) ConfigureRPCProfile(profile string) error {
	switch profile {
	case "":
		repository.trustedCluster = false
	case transportprofile.TrustedCluster:
		repository.trustedCluster = true
	default:
		return errors.New("repository RPC profile rejected")
	}
	return nil
}

// ResolveServiceCredentialGeneration не принимает поколение от caller и не
// меняет историю grants. Срок допуска проверяется по сертификату; bounded
// emergency revoke ускоренного MVP-профиля вынесен в #1527.
// сохранённое поколение используется для прежних domain lease fences.
func (repository *Repository) ResolveServiceCredentialGeneration(ctx context.Context, workload string) (uint64, error) {
	if workload == "" {
		return 0, errs.ErrForbidden
	}
	var generation uint64
	var err error
	if repository.trustedCluster {
		err = repository.pool.QueryRow(ctx, queryTrustedWorkloadGeneration,
			pgx.StrictNamedArgs{"workload_id": workload}).Scan(&generation)
	} else {
		err = repository.pool.QueryRow(ctx, queryServiceCredentialGeneration, workload).Scan(&generation)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errs.ErrForbidden
	}
	if err != nil {
		return 0, errs.ErrUnavailable
	}
	if generation == 0 || generation > 9007199254740991 {
		return 0, errs.ErrForbidden
	}
	return generation, nil
}

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
