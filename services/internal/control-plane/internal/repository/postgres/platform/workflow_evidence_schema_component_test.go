package platform

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestWorkflowReviewQAEvidenceMigrationProofComponent executes the shipped read-only014 proof on canonical and independently drifted disposable clones; it never prepares business rows.
// TestWorkflowReviewQAEvidenceMigrationProofComponent исполняет поставляемую read-only014 proof на canonical и отдельно изменённых disposable clones без подготовки business rows.
func TestWorkflowReviewQAEvidenceMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct{ name, rejected string }{
		{"canonical", ""}, {"partial_trigger", "triggers"}, {"public_execute", "functionACL"},
		{"changed_function", "functionSources"}, {"definer", "functionMetadata"},
		{"changed_purge_body", "purgeGuard"}, {"missing_function", "functionSources"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
			if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
				t.Fatal("invalid disposable review QA schema")
			}
			admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
			if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
				t.Fatal("invalid disposable review QA administrator")
			}
			admin.Database = runtime.Database
			conn, err := pgx.ConnectConfig(ctx, admin)
			if err != nil {
				t.Fatal("connect disposable review QA administrator")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				_ = conn.Close(cleanup)
			}()
			if fixture.rejected != "" {
				mutation, err := os.ReadFile("testdata/sql/workflow_evidence_schema_" + fixture.name + ".sql")
				if err != nil {
					t.Fatal("read disposable review QA schema drift")
				}
				if _, err := conn.Exec(ctx, string(mutation)); err != nil {
					t.Fatal("apply disposable review QA schema drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/workflow-review-qa-evidence-migration-postcondition.sql")
			if err != nil {
				t.Fatal("read shipped review QA schema proof")
			}
			results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
			if err != nil {
				t.Fatal("execute shipped review QA schema proof")
			}
			var proof map[string]any
			for _, result := range results {
				if result.Err != nil {
					t.Fatal("review QA proof statement failed")
				}
				if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
					if proof != nil || json.Unmarshal(result.Rows[0][0], &proof) != nil {
						t.Fatal("decode unique review QA schema proof")
					}
				}
			}
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261026001400) || proof["reviewQAEvidenceProfile"] != "GITHUB_REVIEW_QA_V1" {
				t.Fatal("review QA schema proof identity mismatch")
			}
			if fixture.rejected != "" {
				if proof[fixture.rejected] != false {
					t.Fatal("review QA schema drift falsely passed", fixture.rejected)
				}
				return
			}
			for _, field := range []string{"schemaOwner", "functionSources", "functionMetadata", "functionACL", "runtimeRolesUnprivileged", "triggers", "purgeGuard"} {
				if proof[field] != true {
					t.Error("canonical review QA proof rejected", field)
				}
			}
			if proof["functionCount"] != float64(6) || proof["purgeNodes"] != float64(105) || proof["purgeEdges"] != float64(287) {
				t.Fatal("review QA schema composition mismatch")
			}
		})
	}
}
