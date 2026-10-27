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

// TestWorkflowUpstreamReworkMigrationProofComponent executes the shipped read-only013 proof on canonical and independently drifted disposable clones; it never prepares business rows.
// TestWorkflowUpstreamReworkMigrationProofComponent исполняет поставляемую read-only013 proof на canonical и отдельно изменённых disposable clones без подготовки business rows.
func TestWorkflowUpstreamReworkMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct{ name, rejected string }{
		{"canonical", ""}, {"public_execute", "upstreamFunctions"}, {"security_definer", "upstreamFunctions"},
		{"search_path", "upstreamFunctions"}, {"changed_source", "upstreamFunctions"},
		{"extra_overload", "upstreamExactFunctions"}, {"immediate_fk", "upstreamForeignKey"},
		{"disabled_trigger", "upstreamTriggers"}, {"missing_constraint", "upstreamConstraint"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
			if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
				t.Fatal("invalid disposable rework schema")
			}
			admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
			if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
				t.Fatal("invalid disposable rework administrator")
			}
			admin.Database = runtime.Database
			conn, err := pgx.ConnectConfig(ctx, admin)
			if err != nil {
				t.Fatal("connect disposable rework administrator")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				_ = conn.Close(cleanup)
			}()
			if fixture.rejected != "" {
				mutation, err := os.ReadFile("testdata/sql/workflow_upstream_schema_" + fixture.name + ".sql")
				if err != nil {
					t.Fatal("read disposable rework schema drift")
				}
				if _, err := conn.Exec(ctx, string(mutation)); err != nil {
					t.Fatal("apply disposable rework schema drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/workflow-upstream-rework-migration-postcondition.sql")
			if err != nil {
				t.Fatal("read shipped rework schema proof")
			}
			results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
			if err != nil {
				t.Fatal("execute shipped rework schema proof")
			}
			var proof map[string]any
			for _, result := range results {
				if result.Err != nil {
					t.Fatal("rework proof statement failed")
				}
				if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
					if proof != nil || json.Unmarshal(result.Rows[0][0], &proof) != nil {
						t.Fatal("decode unique rework schema proof")
					}
				}
			}
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261026001300) || proof["legacyReworkProfile"] != "LEGACY_OWNER_REWORK_V1" || proof["workflowUpstreamReworkProfile"] != "OWNER_APPROVED_UPSTREAM_V1" {
				t.Fatal("rework schema proof identity mismatch")
			}
			if fixture.rejected != "" {
				if proof[fixture.rejected] != false {
					t.Fatal("rework schema drift falsely passed", fixture.rejected)
				}
				return
			}
			for _, field := range []string{"schemaOwner", "columns", "constraints", "indexes", "triggers", "functionSources", "functionMetadata", "functionACL",
				"visibilitySource", "retentionSource", "runtimeColumnAccess", "purgeGraphGuard", "bundleNoBodyColumns", "bundleGuard",
				"tableOwners", "runtimeRolesUnprivileged", "runtimeRead", "runtimeMutationDenied", "runtimeColumnMutationDenied", "runtimeGrantOptionDenied",
				"publicPrivilegesRevoked", "ownerWrite", "authorizationFunction", "contextsEmpty", "legacyColumns", "legacyForeignKeys", "legacyConstraint",
				"legacyIndexes", "legacyTriggers", "legacyFunctions", "legacyColumnAccess", "legacyPurgeSource", "workflowContextFunctions", "workflowContextExactFunctions", "upstreamFunctions", "upstreamExactFunctions",
				"upstreamColumns", "upstreamForeignKey", "upstreamConstraint", "upstreamIndex", "upstreamTriggers", "upstreamColumnAccess"} {
				if proof[field] != true {
					t.Error("canonical rework proof rejected", field)
				}
			}
			if proof["purgeNodes"] != float64(105) || proof["purgeEdges"] != float64(287) || proof["serverPurgeEdges"] != float64(284) || proof["serverPurgeFingerprint"] != "6bf6ad76d8c7ae66e07f75ecf719fdd6" {
				t.Fatal("rework purge composition mismatch")
			}
		})
	}
}
