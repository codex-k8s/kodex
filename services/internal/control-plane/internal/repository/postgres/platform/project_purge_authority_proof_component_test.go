package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// queryProjectPurgeAuthorityDriftTable restores an unsafe table grant only in a disposable schema.
	// queryProjectPurgeAuthorityDriftTable возвращает опасный table grant только в одноразовой схеме.
	//go:embed testdata/sql/project_purge_authority_drift_table.sql
	queryProjectPurgeAuthorityDriftTable string
	// queryProjectPurgeAuthorityDriftColumn proves that a column grant can outlive a table-level revoke.
	// queryProjectPurgeAuthorityDriftColumn проверяет column grant, не исчезающий при отзыве table-level прав.
	//go:embed testdata/sql/project_purge_authority_drift_column.sql
	queryProjectPurgeAuthorityDriftColumn string
	// queryProjectPurgeAuthorityDriftPublic probes an alternate effective privilege source in isolation.
	// queryProjectPurgeAuthorityDriftPublic проверяет другой источник effective privileges в изоляции.
	//go:embed testdata/sql/project_purge_authority_drift_public.sql
	queryProjectPurgeAuthorityDriftPublic string
	// queryProjectPurgeAuthorityDriftGuard keeps the function signature but removes its actual authority check.
	// queryProjectPurgeAuthorityDriftGuard сохраняет сигнатуру функции, но убирает проверку authority.
	//go:embed testdata/sql/project_purge_authority_drift_guard.sql
	queryProjectPurgeAuthorityDriftGuard string
	// queryProjectPurgeAuthorityDriftSetRole grants an alternate writer through SET without automatic inheritance.
	// queryProjectPurgeAuthorityDriftSetRole выдаёт альтернативную роль записи через SET без автоматического наследования.
	//go:embed testdata/sql/project_purge_authority_drift_set_role.sql
	queryProjectPurgeAuthorityDriftSetRole string
	// queryProjectPurgeAuthorityDriftAdminRole models permission to enable SET ROLE through ADMIN OPTION.
	// queryProjectPurgeAuthorityDriftAdminRole моделирует возможность разрешить SET ROLE через ADMIN OPTION.
	//go:embed testdata/sql/project_purge_authority_drift_admin_role.sql
	queryProjectPurgeAuthorityDriftAdminRole string
	// queryProjectPurgeAuthorityCleanupSetRole removes the disposable role and its memberships before database cleanup.
	// queryProjectPurgeAuthorityCleanupSetRole удаляет одноразовую роль и членство перед очисткой БД.
	//go:embed testdata/sql/project_purge_authority_cleanup_set_role.sql
	queryProjectPurgeAuthorityCleanupSetRole string
)

// TestProjectPurgeAuthorityMigrationProofComponent runs the shipped read-only release SQL against
// canonical and deliberately unsafe disposable schemas. It verifies the detector, not only its JSON shape.
// TestProjectPurgeAuthorityMigrationProofComponent исполняет поставляемый read-only release SQL на
// правильной и намеренно небезопасных одноразовых схемах, проверяя detector, а не только форму JSON.
func TestProjectPurgeAuthorityMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct {
		name, mutation, cleanup string
		rejected                []string
	}{
		{name: "canonical"},
		{name: "table-grant", mutation: queryProjectPurgeAuthorityDriftTable, rejected: []string{"runtimeMutationDenied", "runtimeColumnMutationDenied"}},
		{name: "column-grant", mutation: queryProjectPurgeAuthorityDriftColumn, rejected: []string{"runtimeColumnMutationDenied"}},
		{name: "public-grant", mutation: queryProjectPurgeAuthorityDriftPublic, rejected: []string{"runtimeMutationDenied", "runtimeColumnMutationDenied", "publicPrivilegesRevoked"}},
		{name: "relaxed-authorizer", mutation: queryProjectPurgeAuthorityDriftGuard, rejected: []string{"authorizationFunction"}},
		{name: "set-without-inherit", mutation: queryProjectPurgeAuthorityDriftSetRole, cleanup: queryProjectPurgeAuthorityCleanupSetRole, rejected: []string{"runtimeMutationDenied", "runtimeColumnMutationDenied"}},
		{name: "admin-without-set", mutation: queryProjectPurgeAuthorityDriftAdminRole, cleanup: queryProjectPurgeAuthorityCleanupSetRole, rejected: []string{"runtimeRolesUnprivileged"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
			if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
				t.Fatal("invalid disposable purge proof database")
			}
			admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
			if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
				t.Fatal("invalid disposable purge proof administrator")
			}
			admin.Database = runtime.Database
			conn, err := pgx.ConnectConfig(ctx, admin)
			if err != nil {
				t.Fatal("connect disposable purge proof administrator")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				_ = conn.Close(cleanup)
			}()
			if fixture.mutation != "" {
				if fixture.cleanup != "" {
					defer func() {
						cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
						defer stop()
						if _, err := conn.Exec(cleanup, fixture.cleanup); err != nil {
							t.Error("clean disposable purge authority role")
						}
					}()
				}
				if _, err := conn.Exec(ctx, fixture.mutation); err != nil {
					t.Fatal("apply disposable purge authority drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/project-purge-authority-migration-postcondition.sql")
			if err != nil {
				t.Fatal("read shipped purge authority proof")
			}
			results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
			if err != nil {
				t.Fatal("execute read-only purge authority proof")
			}
			var proof map[string]any
			for _, result := range results {
				if result.Err != nil {
					t.Fatal("purge authority proof statement failed")
				}
				if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
					if json.Unmarshal(result.Rows[0][0], &proof) != nil {
						t.Fatal("decode purge authority proof")
					}
				}
			}
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261026001000) ||
				proof["tables"] != float64(2) || proof["runtimeRoles"] != float64(2) || proof["runtimeLogins"] != float64(1) {
				t.Fatal("purge authority proof identity mismatch")
			}
			for _, field := range []string{"tableOwners", "runtimeRolesUnprivileged", "runtimeRead", "runtimeMutationDenied", "runtimeColumnMutationDenied",
				"runtimeGrantOptionDenied", "publicPrivilegesRevoked", "ownerWrite", "authorizationFunction", "contextsEmpty"} {
				if proof[field] != !slices.Contains(fixture.rejected, field) {
					t.Errorf("purge authority proof mismatch: field=%s", field)
				}
			}
		})
	}
}
