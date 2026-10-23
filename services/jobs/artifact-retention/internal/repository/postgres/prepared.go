package postgres

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/prepared"
	"github.com/jackc/pgx/v5"
)

var (
	// queryPreparedClaim leases prepared-content rows under owner and generation fencing.
	// RU: queryPreparedClaim арендует записи prepared-контента с защитой владельцем и поколением.
	//go:embed sql/prepared_claim.sql
	queryPreparedClaim string
	// queryPreparedFinish records a confirmed cleanup outcome for the current claim.
	// RU: queryPreparedFinish фиксирует подтверждённый исход очистки текущего claim.
	//go:embed sql/prepared_finish.sql
	queryPreparedFinish string
	// queryPreparedCheck verifies that the required prepared-content schema exists.
	// RU: queryPreparedCheck проверяет наличие необходимой схемы prepared-контента.
	//go:embed sql/prepared_check.sql
	queryPreparedCheck string
	// queryPreparedRecordReceipt fences the exact object receipt before external deletion.
	// RU: queryPreparedRecordReceipt закрепляет точный receipt объекта до внешнего удаления.
	//go:embed sql/prepared_record_receipt.sql
	queryPreparedRecordReceipt string
)

// CheckPrepared fails startup or readiness when the prepared-content schema is unavailable.
// RU: CheckPrepared отклоняет startup или readiness, если схема prepared-контента недоступна.
func (repository *Repository) CheckPrepared(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, queryPreparedCheck).Scan(&ready); err != nil || !ready {
		return errors.New("prepared content cleanup schema is unavailable")
	}
	return nil
}

// ClaimPrepared reads a bounded lease batch and maps exact ledger fields into immutable claims.
// RU: ClaimPrepared читает ограниченную пачку аренд и переносит точные поля ledger в неизменяемые claims.
func (repository *Repository) ClaimPrepared(ctx context.Context, owner string, batch int, lease int64) ([]prepared.Claim, error) {
	rows, err := repository.pool.Query(ctx, queryPreparedClaim, pgx.StrictNamedArgs{"owner": owner, "batch": batch, "lease": lease})
	if err != nil {
		return nil, errors.New("claim prepared content cleanup failed")
	}
	defer rows.Close()
	claims := make([]prepared.Claim, 0, batch)
	for rows.Next() {
		var claim prepared.Claim
		if err := rows.Scan(&claim.LedgerID, &claim.ObjectKey, &claim.ObjectVersion, &claim.ObjectETag,
			&claim.Digest, &claim.SizeBytes, &claim.Generation, &claim.Uncertain); err != nil {
			return nil, errors.New("read prepared content cleanup claim failed")
		}
		claims = append(claims, claim)
	}
	if rows.Err() != nil {
		return nil, errors.New("read prepared content cleanup claims failed")
	}
	return claims, nil
}

// FinishPrepared records a fenced terminal outcome and reports lost ownership instead of accepting it.
// RU: FinishPrepared фиксирует защищённый конечный исход и сообщает об утрате владения вместо его принятия.
func (repository *Repository) FinishPrepared(ctx context.Context, claim prepared.Claim, owner string, success bool, receipt objectstorage.Receipt) error {
	var accepted bool
	if err := repository.pool.QueryRow(ctx, queryPreparedFinish, pgx.StrictNamedArgs{
		"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "success": success,
		"version": receipt.VersionID, "etag": receipt.ETag,
	}).Scan(&accepted); err != nil {
		return errors.New("finish prepared content cleanup failed")
	}
	if !accepted {
		return prepared.ErrLostClaim
	}
	return nil
}

// RecordPreparedReceipt persists the exact version, ETag, digest and size under the current fence.
// RU: RecordPreparedReceipt сохраняет точные версию, ETag, дайджест и размер под текущим fence.
func (repository *Repository) RecordPreparedReceipt(ctx context.Context, claim prepared.Claim, owner string, receipt objectstorage.Receipt) error {
	var accepted bool
	if err := repository.pool.QueryRow(ctx, queryPreparedRecordReceipt, pgx.StrictNamedArgs{
		"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "key": receipt.Key,
		"version": receipt.VersionID, "etag": receipt.ETag, "digest": receipt.Digest, "size": receipt.SizeBytes,
	}).Scan(&accepted); err != nil {
		return errors.New("record prepared cleanup receipt failed")
	}
	if !accepted {
		return prepared.ErrLostClaim
	}
	return nil
}
