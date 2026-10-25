package platform

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

// queryInstructionBindingAssign embeds same-tenant/agent assignment of a PUBLISHED instruction
// with published_at set, inserting a binding or advancing its version. It returns owner binding
// identity; caller authorization and transaction are required.
// queryInstructionBindingAssign встраивает назначение PUBLISHED instruction с published_at того же
// tenant/agent, вставляя binding либо продвигая её версию. Возвращает идентичность owner binding;
// требуются авторизация и транзакция caller.
//
//go:embed sql/instruction_binding_assign.sql
var queryInstructionBindingAssign string

// assignInstructionBinding generates a candidate binding ref and upserts the exact published
// instruction in the supplied owner transaction, returning persisted ref/version and raw query
// error. It does not authorize a rebind, publish instructions or commit; an existing binding keeps
// its original ref.
// assignInstructionBinding создаёт ref-кандидат binding и выполняет upsert точной published
// instruction в переданной owner transaction, возвращая сохранённые ref/version и исходную ошибку
// query. Не разрешает rebind, не публикует instructions и не делает Commit; прежняя binding
// сохраняет исходный ref.
func assignInstructionBinding(ctx context.Context, tx pgx.Tx, organizationID, agentID, instructionRef string) (string, int64, error) {
	ref, err := newRef("inb")
	if err != nil {
		return "", 0, err
	}
	var version int64
	err = tx.QueryRow(ctx, queryInstructionBindingAssign, pgx.StrictNamedArgs{
		"binding_ref": ref, "organization_id": organizationID, "agent_id": agentID, "instruction_ref": instructionRef,
	}).Scan(&ref, &version)
	return ref, version, err
}
