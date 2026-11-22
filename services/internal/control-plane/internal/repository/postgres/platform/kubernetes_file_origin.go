package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFilePinOrigin freezes the exact catalog entry in the first invocation transaction.
// Later preparation never selects the latest RuntimeRevision, artifact head or caller storage path.
// queryKubernetesFilePinOrigin фиксирует exact catalog entry в первой invocation transaction.
// Поздняя подготовка не выбирает latest RuntimeRevision, artifact head либо caller storage path.
//
//go:embed sql/kubernetes_file__pin_origin.sql
var queryKubernetesFilePinOrigin string

// pinKubernetesFileInvocationOriginTx admits only the already validated cp.upload source recipe.
// It must run only after a newly assigned invocation ref won insertion in this same transaction;
// idempotent rejoin must retain the original row instead of repinning a later catalog.
// pinKubernetesFileInvocationOriginTx допускает лишь уже проверенный cp.upload source recipe.
// Вызывается лишь после вставки нового invocation ref в этой же transaction; idempotent rejoin
// сохраняет original row и не перепривязывает более поздний catalog.
func (repository *Repository) pinKubernetesFileInvocationOriginTx(ctx context.Context, tx pgx.Tx, organizationID, invocationRef string, raw []byte) error {
	input, _, err := managedkubernetes.DecodeOperationInput("kubernetes.cp.upload", raw)
	if err != nil {
		return errs.ErrInvalid
	}
	profile, err := managedkubernetes.DecodeCopyProfile("kubernetes.cp.upload", input)
	if err != nil {
		return errs.ErrInvalid
	}
	var id string
	err = tx.QueryRow(ctx, queryKubernetesFilePinOrigin, pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_ref": invocationRef, "artifact_ref": profile.ArtifactRef, "artifact_digest": "sha256:" + profile.ArtifactSHA256}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if id == "" {
		return errs.ErrUnavailable
	}
	return nil
}
