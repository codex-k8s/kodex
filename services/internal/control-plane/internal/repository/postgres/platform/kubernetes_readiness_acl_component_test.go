package platform

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestKubernetesReadinessACLOwnerComponent executes the actual readiness SQL as a separate nonowner
// session. SELECT alone reproduces the old ANY-privilege error; removing each required privilege
// independently must fail, and restoring it must recover readiness without changing schema or data.
// TestKubernetesReadinessACLOwnerComponent выполняет настоящий readiness SQL отдельной сессией без
// прав владельца. Один SELECT воспроизводит прежнюю ошибку ANY; отсутствие каждого необходимого
// права должно закрывать readiness, а восстановление — возвращать её без изменения схемы и данных.
func TestKubernetesReadinessACLOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	fixture := newKubernetesSetupOwnerFixture(t, ctx)
	admin, probe := newReadinessPrivilegeProbe(t, ctx, fixture.pool.Config().ConnConfig)
	role := pgx.Identifier{probe.Config().User}.Sanitize()
	if _, err := admin.Exec(ctx, "GRANT SELECT ON control_plane.kubernetes_operator_current TO "+role); err != nil {
		t.Fatal("grant readiness operator visibility")
	}
	for _, profile := range []struct {
		name, sql string
		tables    map[string][]string
		function  string
	}{
		{"delivery", queryManagedIntegrationDeliveryContract, map[string][]string{
			"integration_invocations":     {"SELECT", "UPDATE"},
			"integration_effect_receipts": {"SELECT", "INSERT"},
		}, ""},
		{"execution", queryKubernetesExecutionReadiness, map[string][]string{
			"kubernetes_execution_plans":             {"SELECT", "INSERT"},
			"kubernetes_execution_children":          {"SELECT", "INSERT"},
			"kubernetes_execution_effects":           {"SELECT", "INSERT", "UPDATE"},
			"kubernetes_execution_streams":           {"SELECT", "INSERT", "UPDATE"},
			"kubernetes_execution_frames":            {"SELECT", "INSERT", "UPDATE"},
			"kubernetes_execution_completions":       {"SELECT", "INSERT"},
			"kubernetes_identity_health_attempts":    {"SELECT", "INSERT"},
			"kubernetes_identity_health_completions": {"SELECT", "INSERT"},
			"integration_connection_tests":           {"SELECT", "UPDATE"},
		}, "control_plane.kubernetes_execution_child_grant_current(uuid,uuid)"},
	} {
		t.Run(profile.name, func(t *testing.T) {
			// check reads only the production query; no SET ROLE or owner identity masks missing ACLs.
			// check читает только production query; SET ROLE и identity владельца не скрывают недостающие ACL.
			check := func(t *testing.T, want bool) {
				t.Helper()
				var ready bool
				if err := probe.QueryRow(ctx, profile.sql).Scan(&ready); err != nil || ready != want {
					t.Fatalf("readiness ACL mismatch: expected=%t actual=%t query_failed=%t", want, ready, err != nil)
				}
			}
			if profile.function != "" {
				if _, err := admin.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+profile.function+" TO "+role); err != nil {
					t.Fatal("grant exact readiness function")
				}
			}
			for table := range profile.tables {
				if _, err := admin.Exec(ctx, "GRANT SELECT ON "+pgx.Identifier{"control_plane", table}.Sanitize()+" TO "+role); err != nil {
					t.Fatal("grant isolated SELECT baseline")
				}
				var oldAny bool
				if probe.QueryRow(ctx, "SELECT has_table_privilege(session_user,$1,'SELECT,INSERT,UPDATE')", "control_plane."+table).Scan(&oldAny) != nil || !oldAny {
					t.Fatal("counterfactual did not reproduce PostgreSQL ANY semantics")
				}
			}
			check(t, false)
			for table, privileges := range profile.tables {
				for _, privilege := range privileges {
					if _, err := admin.Exec(ctx, "GRANT "+privilege+" ON "+pgx.Identifier{"control_plane", table}.Sanitize()+" TO "+role); err != nil {
						t.Fatal("grant exact readiness privilege")
					}
				}
			}
			check(t, true)
			for table, privileges := range profile.tables {
				for _, privilege := range privileges {
					t.Run(table+"/without_"+privilege, func(t *testing.T) {
						object := pgx.Identifier{"control_plane", table}.Sanitize()
						if _, err := admin.Exec(ctx, "REVOKE "+privilege+" ON "+object+" FROM "+role); err != nil {
							t.Fatal("revoke one readiness privilege")
						}
						check(t, false)
						if _, err := admin.Exec(ctx, "GRANT "+privilege+" ON "+object+" TO "+role); err != nil {
							t.Fatal("restore one readiness privilege")
						}
						check(t, true)
					})
				}
			}
			if profile.function != "" {
				if _, err := admin.Exec(ctx, "REVOKE EXECUTE ON FUNCTION "+profile.function+" FROM "+role); err != nil {
					t.Fatal("revoke exact readiness function")
				}
				check(t, false)
				if _, err := admin.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+profile.function+" TO "+role); err != nil {
					t.Fatal("restore exact readiness function")
				}
				check(t, true)
			}
		})
	}
}
