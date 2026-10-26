package platform

import (
	"context"
	_ "embed"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowVisibilityProbe exercises the actual migrated provenance and canonical eligibility
// functions using only real completed capture rows; fault coordinates never mutate business state.
// queryWorkflowVisibilityProbe проверяет настоящие migrated provenance и canonical eligibility
// functions по реальным completed capture rows; fault coordinates не меняют бизнес-состояние.
//
//go:embed testdata/sql/workflow_visibility_probe.sql
var queryWorkflowVisibilityProbe string

// testWorkflowCaptureVisibility checks own-capture membership and denial of other tenants, nodes,
// revisions, unassigned producers and mismatching actor/project/agent coordinates. Full fresh-RR
// catalog, dependency and retention lifecycle acceptance belongs to the server dispatcher suite.
// testWorkflowCaptureVisibility проверяет own-capture membership и отказы для чужих tenant/node,
// revision, неназначенного producer и несовпадающих actor/project/agent. Сквозная приёмка fresh-RR
// catalog, dependencies и retention lifecycle принадлежит suite серверного планировщика.
func testWorkflowCaptureVisibility(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef string, captured int) {
	t.Helper()
	for _, fault := range []string{"own", "tenant", "node", "revision", "unassigned", "actor", "project", "agent"} {
		t.Run("capture-visibility-"+fault, func(t *testing.T) {
			var count int64
			if err := pool.QueryRow(ctx, queryWorkflowVisibilityProbe, pgx.StrictNamedArgs{"node_ref": nodeRef, "fault": fault}).Scan(&count); err != nil {
				t.Fatal("workflow visibility probe failed", completionOutcomeClass(err))
			}
			expected := int64(0)
			if fault == "own" {
				expected = int64(captured)
			}
			if count != expected {
				t.Fatal("workflow visibility returned incorrect provenance membership")
			}
		})
	}
}
