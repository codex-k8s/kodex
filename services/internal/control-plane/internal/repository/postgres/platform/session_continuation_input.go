package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/session_continuation_input.sql
var querySessionContinuationInput string

// Вход наследуется только от доступного завершённого Run той же сессии.
// Новая опубликованная схема и полномочия повторно проверяются launchRun.
func readSessionContinuationInput(ctx context.Context, tx pgx.Tx, current scope, sessionRef, runRef string) (map[string]any, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, querySessionContinuationInput, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project_id": current.authorityProjectID, "session_ref": sessionRef, "run_ref": runRef,
	}).Scan(&raw); err != nil {
		return nil, continuationReadError(err)
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return nil, errs.ErrConflict
	}
	if _, err := runtimecontract.RuntimeBoundedInputDigest(values); err != nil {
		return nil, errs.ErrConflict
	}
	return values, nil
}
