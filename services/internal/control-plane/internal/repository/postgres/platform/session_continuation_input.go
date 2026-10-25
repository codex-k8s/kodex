package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// querySessionContinuationInput embeds latest visible SUCCEEDED run input for an ACTIVE session
// under exact organization/session, matching session target and optional run/project pins. The SQL
// applies run.view for the actor before descending created-at/ref selection. It selects stored
// data, not a new published schema or execution permission.
// querySessionContinuationInput встраивает input последнего видимого SUCCEEDED run ACTIVE-сессии
// внутри точных organization/session с совпадающей session target и необязательными run/project
// pins. SQL применяет run.view субъекта до выбора по убыванию created-at/ref. Выбирает сохранённые
// данные, а не новую published schema или execution permission.
//
//go:embed sql/session_continuation_input.sql
var querySessionContinuationInput string

// readSessionContinuationInput reads inherited input through the caller transaction and trusted
// scope, preserving continuationReadError mappings. Malformed stored JSON or a failed runtime
// bounded-input digest is Conflict; success returns the decoded map without normalizing it. The
// launch path must independently recheck the new published schema and authority; this read neither
// creates a continuation nor persists the digest.
// readSessionContinuationInput читает наследуемый input через транзакцию вызывающего и доверенный
// scope с сохранением отображения continuationReadError. Повреждённый сохранённый JSON или отказ
// runtime bounded-input digest даёт Conflict; успех возвращает разобранную map без нормализации.
// Launch path обязан отдельно повторно проверить новую published schema и полномочия; чтение не
// создаёт continuation и не сохраняет digest.
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
