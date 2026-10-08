package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// План реального запроса доказывает число вычислений, не зависит от скорости хоста.
// Fixture и EXPLAIN исполняются только в disposable компонентной базе.
func assertAssistantContextProjectionReused(t *testing.T, ctx context.Context, repository *Repository, principal value.Principal, projectRef string, expectedLoops int) {
	t.Helper()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatal("resolve repeated-context fixture authority")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin repeated-context fixture snapshot")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "project_ref": projectRef,
		"authority_project": current.authorityProjectID, "assistant_scope": "", "assistant_ref": "",
		"query": "", "match_localized_default_title": false, "state": "ACTIVE", "evaluated_at": time.Now().UTC(),
		"cursor_at": time.Time{}, "cursor_ref": "", "page_size": 100,
	}).Scan(&raw)
	if err != nil {
		t.Fatal("explain repeated-context fixture")
	}
	type node struct {
		Function string  `json:"Function Name"`
		Loops    float64 `json:"Actual Loops"`
		Children []node  `json:"Plans"`
	}
	var explained []struct {
		Plan node `json:"Plan"`
	}
	if json.Unmarshal(raw, &explained) != nil || len(explained) != 1 {
		t.Fatal("decode repeated-context execution plan")
	}
	loops, scans := 0, 0
	var visit func(node)
	visit = func(current node) {
		if current.Function == "assistant_context_projection_v2" {
			loops += int(current.Loops)
			scans++
		}
		for _, child := range current.Children {
			visit(child)
		}
	}
	visit(explained[0].Plan)
	if scans != 1 || loops != expectedLoops {
		t.Fatalf("exact context computed repeatedly: scans=%d loops=%d want=%d", scans, loops, expectedLoops)
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("finish repeated-context fixture snapshot")
	}
}
