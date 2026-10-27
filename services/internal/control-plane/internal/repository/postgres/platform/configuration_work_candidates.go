package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// readConfigurationWorkCandidates reads bounded tenant-scoped references, closing the rows before
// returning them for independent locking and eligibility checks by the caller.
// readConfigurationWorkCandidates читает ограниченные ссылки tenant и закрывает rows до их
// отдельной блокировки и проверки допустимости вызывающим кодом.
func readConfigurationWorkCandidates(ctx context.Context, tx pgx.Tx, query, organizationID string, limit int32) ([]string, error) {
	rows, err := tx.Query(ctx, query, organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return refs, nil
}
