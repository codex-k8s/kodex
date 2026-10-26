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

// testWorkflowJournalStorage checks owner-built provenance, one append, immutable prefixes and sealed
// empty inputs using real completed capture rows. The fixture supplies an evaluated result directly;
// it proves storage constraints only, not result evaluation, dispatch or a complete PDLC.
// testWorkflowJournalStorage проверяет owner provenance, одно добавление, неизменяемый префикс и пустые
// входы по настоящим completed capture rows. Оснастка передаёт проверенный исход напрямую: это проверка
// хранения, а не evaluator, планировщика или полного PDLC.
func testWorkflowJournalStorage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string, capture map[string]any) {
	t.Helper()
	current, lease, history := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	if len(history) != 0 {
		t.Fatal("journal unexpectedly exists before owner append")
	}
	accepted, reason := false, "MISSING_RESULT"
	results := []entity.WorkflowAcceptedResult{}
	entries := capture["artifacts"].([]any)
	if len(entries) == 1 {
		entry := entries[0].(map[string]any)
		accepted, reason = true, ""
		results = append(results, entity.WorkflowAcceptedResult{Key: "report", Kind: "ARTIFACT",
			ArtifactRef: entry["ref"].(string), ArtifactRevision: entry["revisionRef"].(string), Digest: entry["digest"].(string)[7:]})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin isolated workflow journal append")
	}
	stored, appendErr := appendWorkflowAttemptTx(ctx, tx, current, lease, accepted, reason, results)
	if appendErr != nil {
		rollbackWorkflowJournalProbe(t, ctx, tx)
		t.Fatal("append evaluated workflow outcome", completionOutcomeClass(appendErr))
	}
	rollbackWorkflowJournalProbe(t, ctx, tx)
	rejectWorkflowJournalFirstAppend(t, ctx, pool, nodeRef, leaseRef, stored)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin committed workflow journal append")
	}
	stored, appendErr = appendWorkflowAttemptTx(ctx, tx, current, lease, accepted, reason, results)
	if appendErr != nil {
		rollbackWorkflowJournalProbe(t, ctx, tx)
		t.Fatal("append after rollback-only journal probes", completionOutcomeClass(appendErr))
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit isolated workflow journal append")
	}
	_, _, baseline := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	if len(baseline) != 1 || !reflect.DeepEqual(baseline[0], stored) || stored.Provenance == nil || stored.Attempt != 1 ||
		stored.Provenance.NodeID != lease["nodeID"] || stored.Provenance.OriginNodeID != lease["nodeID"] ||
		stored.Provenance.RootRunID != lease["rootRunID"] || stored.Provenance.LeaseID != lease["leaseID"] ||
		stored.RuntimeRevisionID != lease["runtimeRevisionID"] || stored.Accepted != accepted {
		t.Fatal("workflow journal lost exact completed provenance")
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
				altered[0]["accepted"] = !accepted
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

// rejectWorkflowJournalFirstAppend probes forged initial provenance before an immutable prefix can
// mask a missing exact-pin check. Each case starts from empty committed history and rolls back.
// rejectWorkflowJournalFirstAppend проверяет подмену первого происхождения до того, как защита
// префикса сможет скрыть отсутствие exact-pin проверки. Каждый случай начинается с пустой истории.
func rejectWorkflowJournalFirstAppend(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string, candidate entity.WorkflowAttemptResult) {
	t.Helper()
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal("encode candidate workflow journal receipt")
	}
	for _, field := range []string{"rootRunId", "originNodeId", "nodeId", "leaseId", "turnId", "generation", "runtimeAttempt", "runtimeRevisionId", "runRef", "attempt", "reason", "unknown"} {
		t.Run("first-journal-"+field, func(t *testing.T) {
			var altered map[string]any
			if json.Unmarshal(encoded, &altered) != nil {
				t.Fatal("decode independent first journal mutation")
			}
			proof := altered["provenance"].(map[string]any)
			switch field {
			case "generation", "runtimeAttempt":
				proof[field] = proof[field].(float64) + 1
			case "runtimeRevisionId", "runRef":
				altered[field] = "00000000-0000-4000-8000-000000000001"
			case "attempt":
				altered[field] = 2
			case "reason":
				if candidate.Accepted {
					altered[field] = "MISSING_RESULT"
				} else {
					altered[field] = ""
				}
			case "unknown":
				altered["authority"] = "OWNER"
			default:
				proof[field] = "00000000-0000-4000-8000-000000000001"
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin first journal provenance probe")
			}
			_, mutationErr := tx.Exec(ctx, queryWorkflowJournalMutate, pgx.StrictNamedArgs{
				"node_ref": nodeRef, "attempts": []any{altered}, "inputs": []any{},
			})
			var failure *pgconn.PgError
			closed := errors.As(mutationErr, &failure) && failure.Code == "23514"
			rollbackWorkflowJournalProbe(t, ctx, tx)
			_, _, after := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
			if !closed || len(after) != 0 {
				t.Fatal("forged first journal provenance was not rejected atomically")
			}
		})
	}
}
