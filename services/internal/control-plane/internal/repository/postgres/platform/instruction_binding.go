package platform

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

//go:embed sql/instruction_binding_assign.sql
var queryInstructionBindingAssign string

// Назначение выполняется только из авторитетного lifecycle в той же транзакции.
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
