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

// TestLegacyWorkflowReworkMigrationProofComponent executes the shipped read-only011 proof on canonical and independently drifted disposable clones; it never prepares business rows.
// TestLegacyWorkflowReworkMigrationProofComponent исполняет поставляемую read-only011 proof на canonical и отдельно изменённых disposable clones без подготовки business rows.
func TestLegacyWorkflowReworkMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct{ name, rejected string }{
		{"canonical", ""}, {"partial_trigger", "legacyTriggers"}, {"wrong_fk_column", "legacyForeignKeys"},
		{"public_execute", "legacyFunctions"}, {"wrong_index", "legacyIndexes"},
		{"missing_check", "legacyConstraint"}, {"changed_function", "legacyFunctions"}, {"changed_purge_body", "legacyPurgeSource"},
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
				mutation, err := os.ReadFile("testdata/sql/legacy_rework_schema_" + fixture.name + ".sql")
				if err != nil {
					t.Fatal("read disposable rework schema drift")
				}
				if _, err := conn.Exec(ctx, string(mutation)); err != nil {
					t.Fatal("apply disposable rework schema drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/workflow-exception-context-migration-postcondition.sql")
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
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261026001200) || proof["legacyReworkProfile"] != "LEGACY_OWNER_REWORK_V1" {
				t.Fatal("rework schema proof identity mismatch")
			}
			if fixture.rejected != "" {
				if fixture.name == "changed_purge_body" && proof["purgeGraphGuard"] != true {
					t.Fatal("purge body drift probe changed graph instead of testing exact source")
				}
				if proof[fixture.rejected] != false {
					t.Fatal("rework schema drift falsely passed", fixture.rejected)
				}
				return
			}
			for _, field := range []string{"schemaOwner", "columns", "constraints", "indexes", "triggers", "functionSources", "functionMetadata", "functionACL",
				"visibilitySource", "retentionSource", "runtimeColumnAccess", "purgeGraphGuard", "bundleNoBodyColumns", "bundleGuard",
				"tableOwners", "runtimeRolesUnprivileged", "runtimeRead", "runtimeMutationDenied", "runtimeColumnMutationDenied", "runtimeGrantOptionDenied",
				"publicPrivilegesRevoked", "ownerWrite", "authorizationFunction", "contextsEmpty", "legacyColumns", "legacyForeignKeys", "legacyConstraint",
				"legacyIndexes", "legacyTriggers", "legacyFunctions", "legacyColumnAccess", "legacyPurgeSource"} {
				if proof[field] != true {
					t.Error("canonical rework proof rejected", field)
				}
			}
			if proof["purgeNodes"] != float64(105) || proof["purgeEdges"] != float64(286) || proof["serverPurgeEdges"] != float64(284) || proof["serverPurgeFingerprint"] != "6bf6ad76d8c7ae66e07f75ecf719fdd6" {
				t.Fatal("rework purge composition mismatch")
			}
		})
	}
}
