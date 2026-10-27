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

// TestServerWorkflowMigrationProofComponent preserves the complete SERVER proof under the current SSH successor and executes its release proof on
// isolated schema clones and proves that concrete DDL drift is rejected. It never
// changes business rows or connects to a live database.
// TestServerWorkflowMigrationProofComponent сохраняет полный SERVER proof под текущим SSH successor и исполняет его release proof на
// отдельных копиях схемы и проверяет обнаружение конкретных изменений DDL.
// Бизнес-строки и живая БД не изменяются.
func TestServerWorkflowMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct {
		name, rejectedField string
		expected            any
	}{
		{name: "canonical"},
		{name: "disabled-workflow-guard", rejectedField: "triggers", expected: false},
		{name: "missing-workflow-check", rejectedField: "constraints", expected: false},
		{name: "missing-workflow-index", rejectedField: "indexes", expected: false},
		{name: "public-workflow-execute", rejectedField: "functionACL", expected: false},
		{name: "disabled-bundle-guard", rejectedField: "bundleGuard", expected: false},
		{name: "missing-bundle-fk", rejectedField: "bundleForeignKeys", expected: float64(12)},
		{name: "disabled-ssh-snapshot-guard", rejectedField: "sshTriggers", expected: false},
		{name: "missing-ssh-purpose-check", rejectedField: "sshConstraints", expected: false},
		{name: "public-ssh-execute", rejectedField: "sshFunctions", expected: false},
		{name: "disabled-ssh-admission-guard", rejectedField: "sshTriggers", expected: false},
		{name: "missing-ssh-admission-check", rejectedField: "sshConstraints", expected: false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
			if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
				t.Fatal("invalid disposable workflow schema database")
			}
			admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
			if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
				t.Fatal("invalid disposable workflow schema administrator")
			}
			admin.Database = runtime.Database
			conn, err := pgx.ConnectConfig(ctx, admin)
			if err != nil {
				t.Fatal("connect disposable workflow schema administrator")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				_ = conn.Close(cleanup)
			}()
			if fixture.name != "canonical" {
				mutation, readErr := os.ReadFile("testdata/sql/workflow_schema_" + strings.ReplaceAll(fixture.name, "-", "_") + ".sql")
				if readErr != nil {
					t.Fatal("read disposable workflow schema drift")
				}
				if _, err := conn.Exec(ctx, string(mutation)); err != nil {
					t.Fatal("apply disposable workflow schema drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/ssh-compose-migration-postcondition.sql")
			if err != nil {
				t.Fatal("read shipped workflow schema proof")
			}
			results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
			if err != nil {
				t.Fatal("execute read-only workflow schema proof")
			}
			var proof map[string]any
			for _, result := range results {
				if result.Err != nil {
					t.Fatal("workflow schema proof statement failed")
				}
				if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
					if proof != nil || json.Unmarshal(result.Rows[0][0], &proof) != nil {
						t.Fatal("decode unique workflow schema proof")
					}
				}
			}
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261027000100) || proof["serverWorkflowProfile"] != "SERVER_DAG_V1" || proof["sshComposeProfile"] != "SSH_COMPOSE_V1" {
				t.Fatal("workflow schema proof identity mismatch")
			}
			if fixture.rejectedField != "" {
				if proof[fixture.rejectedField] != fixture.expected {
					t.Errorf("workflow schema drift was not detected: field=%s", fixture.rejectedField)
				}
				return
			}
			for _, field := range []string{
				"schemaOwner", "columns", "constraints", "indexes", "triggers", "functionSources", "functionMetadata", "functionACL",
				"visibilitySource", "retentionSource", "runtimeColumnAccess", "purgeGraphGuard", "bundleNoBodyColumns", "bundleGuard",
				"sshColumns", "sshConstraints", "sshFunctions", "sshTriggers", "sshRuntimeColumns",
				"tableOwners", "runtimeRolesUnprivileged", "runtimeRead", "runtimeMutationDenied", "runtimeColumnMutationDenied",
				"runtimeGrantOptionDenied", "publicPrivilegesRevoked", "ownerWrite", "authorizationFunction", "contextsEmpty",
			} {
				if proof[field] != true {
					t.Errorf("canonical workflow schema proof mismatch: field=%s", field)
				}
			}
			for field, expected := range map[string]any{
				"workflowColumns": float64(9), "workflowConstraints": float64(6), "workflowGuards": float64(6), "workflowHelpers": float64(4),
				"purgeNodes": float64(105), "purgeEdges": float64(284), "purgeBaselineEdges": float64(283),
				"bundleTableOwner": "control_plane_owner", "bundleForeignKeys": float64(13), "bundleDeferredForeignKeys": float64(13),
				"purgeNodeFingerprint": "f51a0de25bd942a250997775617eeaee", "purgeEdgeFingerprint": "6bf6ad76d8c7ae66e07f75ecf719fdd6",
				"purgeBaselineEdgeFingerprint": "be6e23d9e894687a69a19e9b5777ab09",
			} {
				if proof[field] != expected {
					t.Errorf("canonical workflow schema metadata mismatch: field=%s", field)
				}
			}
		})
	}
}
