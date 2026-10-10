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
	//go:embed sql/prepared_claim.sql
	queryPreparedClaim string
	//go:embed sql/prepared_finish.sql
	queryPreparedFinish string
	//go:embed sql/prepared_check.sql
	queryPreparedCheck string
	//go:embed sql/prepared_record_receipt.sql
	queryPreparedRecordReceipt string
)

func (repository *Repository) CheckPrepared(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, queryPreparedCheck).Scan(&ready); err != nil || !ready {
		return errors.New("prepared content cleanup schema is unavailable")
	}
	return nil
}

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
