package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// queryWorkflowJournalContext reads private execution coordinates only from disposable owner rows.
	// queryWorkflowJournalContext читает приватные координаты исполнения только из одноразовых owner rows.
	//go:embed testdata/sql/workflow_journal_context.sql
	queryWorkflowJournalContext string
	// queryWorkflowJournalMutate probes journal and input replacement through the actual runtime role.
	// queryWorkflowJournalMutate проверяет замену журнала и входов через настоящую runtime-роль.
	//go:embed testdata/sql/workflow_journal_mutate.sql
	queryWorkflowJournalMutate string
)

// workflowJournalFixture reads execution identities without accepting provenance from test payloads.
// workflowJournalFixture читает identity исполнения, не принимая происхождение из тестового payload.
func workflowJournalFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string) (scope, map[string]any, []entity.WorkflowAttemptResult) {
	t.Helper()
	var current scope
	var nodeID, rootID, revisionID, leaseID string
	var history []entity.WorkflowAttemptResult
	if err := pool.QueryRow(ctx, queryWorkflowJournalContext, pgx.StrictNamedArgs{"node_ref": nodeRef, "lease_ref": leaseRef}).Scan(
		&current.organizationID, &nodeID, &rootID, &revisionID, &leaseID, &history); err != nil {
		t.Fatal("read isolated workflow journal context")
	}
	return current, map[string]any{"nodeID": nodeID, "rootRunID": rootID, "runtimeRevisionID": revisionID, "leaseID": leaseID}, history
}

// rollbackWorkflowJournalProbe bounds cleanup independently from cancellation of the probe itself.
// rollbackWorkflowJournalProbe ограничивает cleanup независимо от отмены самой проверки.
func rollbackWorkflowJournalProbe(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil {
		t.Fatal("rollback isolated workflow journal probe")
	}
}

// rejectWorkflowJournalBeforeCompletion proves that a claimed lease cannot attest a business result.
// rejectWorkflowJournalBeforeCompletion доказывает, что claimed lease не удостоверяет бизнес-результат.
func rejectWorkflowJournalBeforeCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string) {
	t.Helper()
	current, lease, history := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	if len(history) != 0 {
		t.Fatal("unexpected journal before completion")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin pre-completion journal probe")
	}
	_, appendErr := appendWorkflowAttemptTx(ctx, tx, current, lease, false, "MISSING_RESULT", nil)
	rollbackWorkflowJournalProbe(t, ctx, tx)
	if !errors.Is(appendErr, errs.ErrConflict) {
		t.Fatal("claimed execution was allowed to append a workflow outcome")
	}
}

// testWorkflowJournalStorage verifies the committed RESULT_INVALID entry created by real completion,
// duplicate-append denial and immutable history. The plain summary is deliberately not a business
// declaration: a stored capture must not become acceptance. Initial-append field fault probes are
// excluded here because completion has already sealed the first entry; prefix denial would mask them.
// testWorkflowJournalStorage проверяет RESULT_INVALID, записанный настоящим completion, отказ
// повторного append и неизменяемость истории. Plain summary намеренно не является бизнес-декларацией:
// сохранённый capture не превращается в приёмку. Проверки полей первого append здесь исключены:
// completion уже закрепил запись, и отказ менять префикс скрыл бы причину такого probe.
func testWorkflowJournalStorage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef, runRef string, capture map[string]any) {
	t.Helper()
	current, lease, baseline := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	if len(baseline) != 1 {
		t.Fatal("completion did not append exactly one workflow outcome")
	}
	stored := baseline[0]
	if stored.Provenance == nil || stored.Attempt != 1 || stored.RunRef != runRef ||
		stored.Provenance.NodeID != lease["nodeID"] || stored.Provenance.OriginNodeID != lease["nodeID"] ||
		stored.Provenance.RootRunID != lease["rootRunID"] || stored.Provenance.LeaseID != lease["leaseID"] ||
		stored.RuntimeRevisionID != lease["runtimeRevisionID"] ||
		stored.Provenance.TurnID != capture["turnId"] ||
		stored.Provenance.Generation != int64(capture["generation"].(float64)) ||
		stored.Provenance.RuntimeAttempt != int32(capture["runtimeAttempt"].(float64)) ||
		stored.Accepted || stored.Reason != workflowDeclarationInvalid || len(stored.Results) != 0 {
		t.Fatal("completion journal lost exact provenance or accepted a plain summary")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin duplicate workflow journal append")
	}
	_, appendErr := appendWorkflowAttemptTx(ctx, tx, current, lease, stored.Accepted, stored.Reason, stored.Results)
	rollbackWorkflowJournalProbe(t, ctx, tx)
	_, _, afterDuplicate := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	if !errors.Is(appendErr, errs.ErrConflict) || !reflect.DeepEqual(afterDuplicate, baseline) {
		t.Fatal("duplicate workflow outcome was not rejected without changing history")
	}
	encoded, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal("encode isolated workflow journal baseline")
	}
	for _, fault := range []string{"clear", "replace-verdict", "replace-lease", "duplicate", "forged-second", "change-inputs"} {
		t.Run("journal-"+fault, func(t *testing.T) {
			var altered []map[string]any
			if json.Unmarshal(encoded, &altered) != nil {
				t.Fatal("decode independent journal mutation")
			}
			inputs := []any{}
			switch fault {
			case "clear":
				altered = []map[string]any{}
			case "replace-verdict":
				altered[0]["accepted"] = !stored.Accepted
			case "replace-lease":
				altered[0]["provenance"].(map[string]any)["leaseId"] = "00000000-0000-4000-8000-000000000001"
			case "duplicate":
				altered = append(altered, altered[0])
			case "forged-second":
				var forged map[string]any
				first, marshalErr := json.Marshal(altered[0])
				if marshalErr != nil || json.Unmarshal(first, &forged) != nil {
					t.Fatal("copy independent journal attempt")
				}
				forged["attempt"] = 2
				altered = append(altered, forged)
			case "change-inputs":
				inputs = []any{map[string]any{"stepKey": "capture", "sourceNodeId": lease["nodeID"], "receipt": altered[0]}}
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated journal mutation")
			}
			_, mutationErr := tx.Exec(ctx, queryWorkflowJournalMutate, pgx.StrictNamedArgs{"node_ref": nodeRef, "attempts": altered, "inputs": inputs})
			var failure *pgconn.PgError
			closed := errors.As(mutationErr, &failure) && failure.Code == "23514"
			rollbackWorkflowJournalProbe(t, ctx, tx)
			_, _, after := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
			if !closed || !reflect.DeepEqual(after, baseline) {
				t.Fatal("workflow journal mutation was not rejected atomically")
			}
		})
	}
}
