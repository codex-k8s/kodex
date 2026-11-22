package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureArtifacts returns immutable adopted revision metadata for one sealed source.
// queryKubernetesCaptureArtifacts возвращает immutable adopted revision metadata одного sealed source.
//
//go:embed sql/kubernetes_capture__artifacts.sql
var queryKubernetesCaptureArtifacts string

// queryKubernetesCaptureStreamLock preserves execution's root-before-stream serialization order.
// queryKubernetesCaptureStreamLock сохраняет execution serialization order root-before-stream.
//
//go:embed sql/kubernetes_capture__stream_lock.sql
var queryKubernetesCaptureStreamLock string

// queryKubernetesCaptureCurrent validates the task again after waiting for authoritative locks.
// queryKubernetesCaptureCurrent повторно проверяет task после authoritative lock waiting.
//
//go:embed sql/kubernetes_capture__current.sql
var queryKubernetesCaptureCurrent string

// queryKubernetesCaptureRecipe reads only the original validated child input inside its owner.
// queryKubernetesCaptureRecipe читает только original validated child input внутри owner.
//
//go:embed sql/kubernetes_capture__recipe.sql
var queryKubernetesCaptureRecipe string

// lockKubernetesCaptureOwnerTx validates original source eligibility separately from native grants.
// Root cancellation may stop effects while the owner's confirmed history remains publishable.
// The scope below is owner-derived historical provenance, never a fabricated transport principal.
// lockKubernetesCaptureOwnerTx проверяет original source eligibility отдельно от native grants.
// Root cancellation останавливает effects, но confirmed history владельца остаётся publishable.
// Scope ниже — owner-derived historical provenance, не поддельный transport principal.
func (repository *Repository) lockKubernetesCaptureOwnerTx(ctx context.Context, tx pgx.Tx, record kubernetesCaptureTaskRecord, binding value.ManagedKubernetesCaptureTaskBinding) (scope, error) {
	source := record.source
	current := scope{organizationID: record.organizationID, organizationRef: source.OrganizationRef, actorID: source.RootActorID, actorRef: source.RootActorRef}
	if _, err := lockOwnerGateRootTx(ctx, tx, source.RootRunID); err != nil {
		return scope{}, err
	}
	var id string
	if err := tx.QueryRow(ctx, queryKubernetesCaptureStreamLock, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID}).Scan(&id); err != nil || id != record.streamID {
		return scope{}, errs.ErrUnavailable
	}
	args, err := kubernetesCaptureTaskArgs(binding)
	if err != nil {
		return scope{}, err
	}
	if err := tx.QueryRow(ctx, queryKubernetesCaptureCurrent, args).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return scope{}, errs.ErrForbidden
	} else if err != nil {
		return scope{}, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: source.RootRunRef, ProjectRef: source.ProjectRef}); err != nil {
		return scope{}, err
	}
	return current, nil
}

// readKubernetesCaptureRecipe verifies the original input digest before any copy normalization.
// It does not expose the recipe through the task receipt or discover current target state.
// readKubernetesCaptureRecipe сверяет original input digest до copy normalization.
// Не выдаёт recipe через task receipt и не читает current target state.
func (repository *Repository) readKubernetesCaptureRecipe(ctx context.Context, record kubernetesCaptureTaskRecord) ([]byte, error) {
	var raw string
	err := repository.pool.QueryRow(ctx, queryKubernetesCaptureRecipe, pgx.StrictNamedArgs{"organization_id": record.organizationID, "capture_id": record.id, "operation": record.source.Operation, "child_input_digest": record.source.ChildInputDigest}).Scan(&raw)
	sum := sha256.Sum256([]byte(raw))
	if err != nil || len(raw) > 524288 || hex.EncodeToString(sum[:]) != record.source.ChildInputDigest {
		return nil, errs.ErrUnavailable
	}
	return []byte(raw), nil
}

// readKubernetesCaptureArtifacts attaches only safe persisted metadata; it returns no payload,
// object locator, scan bypass or renewed authority for reading an artifact.
// readKubernetesCaptureArtifacts добавляет только safe persisted metadata без payload,
// object locator, scan bypass либо renewed authority для чтения artifact.
func readKubernetesCaptureArtifacts(ctx context.Context, runner queryRunner, record *kubernetesCaptureTaskRecord) error {
	rows, err := runner.Query(ctx, queryKubernetesCaptureArtifacts, pgx.StrictNamedArgs{"organization_id": record.organizationID, "capture_id": record.id})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var artifact entity.ManagedKubernetesCapturedArtifact
		if rows.Scan(&artifact.Channel, &artifact.ArtifactRef, &artifact.RevisionRef, &artifact.Digest, &artifact.SizeBytes, &artifact.CaptureGeneration, &artifact.Partial) != nil || len(record.receipt.Artifacts) >= 5 {
			return errs.ErrUnavailable
		}
		record.receipt.Artifacts = append(record.receipt.Artifacts, artifact)
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}
