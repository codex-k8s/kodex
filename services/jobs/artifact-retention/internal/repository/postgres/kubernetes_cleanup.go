package postgres

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/prepared"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCleanupClaim reads only owner-admitted versioned stage cleanup work.
// queryKubernetesCleanupClaim читает только owner-admitted versioned stage cleanup work.
//
//go:embed sql/kubernetes_cleanup_claim.sql
var queryKubernetesCleanupClaim string

// queryKubernetesCleanupRecord fences a positive exact object receipt before deletion.
// queryKubernetesCleanupRecord закрепляет positive exact object receipt до удаления.
//
//go:embed sql/kubernetes_cleanup_record.sql
var queryKubernetesCleanupRecord string

// queryKubernetesCleanupFinish records confirmed exact-version deletion under the same claim.
// queryKubernetesCleanupFinish фиксирует подтверждённое exact-version deletion под same claim.
//
//go:embed sql/kubernetes_cleanup_finish.sql
var queryKubernetesCleanupFinish string

// KubernetesCleanup adapts the existing prepared deletion processor to closed typed stage sources.
// It has no artifact publication, native delivery or direct table mutation authority.
// KubernetesCleanup адаптирует existing prepared deletion processor к closed typed stage sources.
// Artifact publication, native delivery либо direct table mutation authority не получает.
type KubernetesCleanup struct{ repository *Repository }

// NewKubernetesCleanup binds the caller-owned database pool without starting a worker.
// NewKubernetesCleanup связывает caller-owned database pool без запуска worker.
func NewKubernetesCleanup(repository *Repository) *KubernetesCleanup {
	return &KubernetesCleanup{repository: repository}
}

// ClaimPrepared reads one bounded batch, preserving the source kind and immutable object tuple.
// ClaimPrepared читает bounded batch, сохраняя source kind и immutable object tuple.
func (repository *KubernetesCleanup) ClaimPrepared(ctx context.Context, owner string, batch int, lease int64) ([]prepared.Claim, error) {
	rows, err := repository.repository.pool.Query(ctx, queryKubernetesCleanupClaim, pgx.StrictNamedArgs{"owner": owner, "batch": batch, "lease": lease})
	if err != nil {
		return nil, errors.New("claim Kubernetes stage cleanup failed")
	}
	defer rows.Close()
	result := make([]prepared.Claim, 0, batch)
	for rows.Next() {
		var claim prepared.Claim
		if rows.Scan(&claim.SourceKind, &claim.LedgerID, &claim.ObjectKey, &claim.ObjectVersion, &claim.ObjectETag, &claim.Digest, &claim.SizeBytes, &claim.Generation, &claim.Uncertain) != nil {
			return nil, errors.New("read Kubernetes stage cleanup claim failed")
		}
		result = append(result, claim)
	}
	if rows.Err() != nil {
		return nil, errors.New("read Kubernetes stage cleanup claims failed")
	}
	return result, nil
}

// RecordPreparedReceipt records only the same positive version, hash and size; absence is no proof.
// RecordPreparedReceipt фиксирует только same positive version/hash/size; отсутствие не доказательство.
func (repository *KubernetesCleanup) RecordPreparedReceipt(ctx context.Context, claim prepared.Claim, owner string, receipt objectstorage.Receipt) error {
	if claim.SourceKind == "KUBERNETES_PREPARED" {
		return repository.repository.RecordPreparedReceipt(ctx, claim, owner, receipt)
	}
	var accepted bool
	if repository.repository.pool.QueryRow(ctx, queryKubernetesCleanupRecord, pgx.StrictNamedArgs{"id": claim.LedgerID, "kind": claim.SourceKind, "owner": owner, "generation": claim.Generation, "key": receipt.Key, "version": receipt.VersionID, "etag": receipt.ETag, "digest": receipt.Digest, "size": receipt.SizeBytes}).Scan(&accepted) != nil {
		return errors.New("record Kubernetes stage cleanup receipt failed")
	}
	if !accepted {
		return prepared.ErrLostClaim
	}
	return nil
}

// FinishPrepared closes only the current deletion claim and preserves the original stage receipt.
// FinishPrepared закрывает current deletion claim, сохраняя original stage receipt.
func (repository *KubernetesCleanup) FinishPrepared(ctx context.Context, claim prepared.Claim, owner string, success bool, receipt objectstorage.Receipt) error {
	if claim.SourceKind == "KUBERNETES_PREPARED" {
		return repository.repository.FinishPrepared(ctx, claim, owner, success, receipt)
	}
	var accepted bool
	if repository.repository.pool.QueryRow(ctx, queryKubernetesCleanupFinish, pgx.StrictNamedArgs{"id": claim.LedgerID, "kind": claim.SourceKind, "owner": owner, "generation": claim.Generation, "version": receipt.VersionID, "etag": receipt.ETag, "success": success}).Scan(&accepted) != nil {
		return errors.New("finish Kubernetes stage cleanup failed")
	}
	if !accepted {
		return prepared.ErrLostClaim
	}
	return nil
}
