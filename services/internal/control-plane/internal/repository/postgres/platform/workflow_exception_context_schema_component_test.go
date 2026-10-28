package platform

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestWorkflowExceptionContextMigrationProofComponent checks context invariants through the explicit inverse014 projection of the current017+SSH proof.
// Actual schema version and graph remain separately checked; all original context counterfactuals execute in isolated clones without business rows.
// TestWorkflowExceptionContextMigrationProofComponent проверяет context invariants через явную inverse014 projection текущего017+SSH proof.
// Actual schema version и graph проверяются отдельно; все прежние context counterfactuals исполняются в isolated clones без business rows.
func TestWorkflowExceptionContextMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct{ name, rejected string }{
		{"canonical", ""}, {"public_execute", "workflowContextFunctions"}, {"security_definer", "workflowContextFunctions"},
		{"search_path", "workflowContextFunctions"}, {"changed_source", "workflowContextFunctions"},
		{"extra_overload", "workflowContextExactFunctions"}, {"changed_union", "functionSources"},
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
				mutation, err := os.ReadFile("testdata/sql/workflow_exception_context_schema_" + fixture.name + ".sql")
				if err != nil {
					t.Fatal("read disposable rework schema drift")
				}
				if _, err := conn.Exec(ctx, string(mutation)); err != nil {
					t.Fatal("apply disposable rework schema drift")
				}
			}
			actual, proof := readWorkspaceVolumeSchemaProof(t, ctx, conn)
			if proof["legacyReworkProfile"] != "LEGACY_OWNER_REWORK_V1" || proof["workflowExceptionContextProfile"] != "SERVER_EXCEPTION_CONTEXT_V1" {
				t.Fatal("rework schema proof identity mismatch")
			}
			if fixture.rejected != "" {
				if proof[fixture.rejected] != false {
					t.Fatal("rework schema drift falsely passed", fixture.rejected)
				}
				return
			}
			assertWorkspaceVolumeSchemaCanonical(t, actual)
			for _, field := range []string{"schemaOwner", "columns", "constraints", "indexes", "triggers", "functionSources", "functionMetadata", "functionACL",
				"visibilitySource", "retentionSource", "runtimeColumnAccess", "purgeGraphGuard", "bundleNoBodyColumns", "bundleGuard",
				"tableOwners", "runtimeRolesUnprivileged", "runtimeRead", "runtimeMutationDenied", "runtimeColumnMutationDenied", "runtimeGrantOptionDenied",
				"publicPrivilegesRevoked", "ownerWrite", "authorizationFunction", "contextsEmpty", "legacyColumns", "legacyForeignKeys", "legacyConstraint",
				"legacyIndexes", "legacyTriggers", "legacyFunctions", "legacyColumnAccess", "legacyPurgeSource", "workflowContextFunctions", "workflowContextExactFunctions"} {
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
