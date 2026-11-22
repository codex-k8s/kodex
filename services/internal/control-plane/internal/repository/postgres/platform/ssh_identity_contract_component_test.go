package platform

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestSSHIdentitySchemaContractOwnerComponent exercises the exact purpose readiness SQL under a separate nonowner session without a readiness override. It verifies ACL/trigger availability, not SSH host authority or execution admission.
// TestSSHIdentitySchemaContractOwnerComponent выполняет exact purpose readiness SQL отдельной nonowner session без readiness override. Проверяются ACL/trigger availability, не host authority либо execution admission.
func TestSSHIdentitySchemaContractOwnerComponent(t *testing.T) {
	fixture := newSSHSetupNativeFixture(t)
	var runtimeReady bool
	if err := fixture.pool.QueryRow(fixture.ctx, querySSHExecutionContract).Scan(&runtimeReady); err != nil || !runtimeReady {
		t.Fatalf("migrated runtime SSH contract failed: ready=%t query_failed=%t", runtimeReady, err != nil)
	}
	admin, probe := newReadinessPrivilegeProbe(t, fixture.ctx, fixture.pool.Config().ConnConfig)
	role := pgx.Identifier{probe.Config().User}.Sanitize()
	// check uses the production query as-is, including its actual session_user; no SET ROLE or readiness override is accepted.
	// check использует production query без изменений с actual session_user; SET ROLE либо readiness override не принимаются.
	check := func(t *testing.T, want bool) {
		t.Helper()
		var ready bool
		if err := probe.QueryRow(fixture.ctx, querySSHExecutionContract).Scan(&ready); err != nil || ready != want {
			t.Fatalf("SSH schema contract mismatch: expected=%t actual=%t query_failed=%t", want, ready, err != nil)
		}
	}
	// currentFunction is the exact composite-argument owner function registered by the local schema; it grants no caller-selectable SQL.
	// currentFunction задаёт exact composite-argument owner function local schema, без caller-selectable SQL.
	const currentFunction = "control_plane.ssh_invocation_identity_current(control_plane.ssh_invocation_identity_bindings)"
	if _, err := admin.Exec(fixture.ctx, "GRANT EXECUTE ON FUNCTION "+currentFunction+" TO "+role); err != nil {
		t.Fatal("grant exact SSH schema function")
	}
	required := map[string][]string{
		"ssh_identity_setups":              {"SELECT", "INSERT", "UPDATE"},
		"ssh_identity_upload_attempts":     {"SELECT", "INSERT", "UPDATE"},
		"ssh_identity_upload_observations": {"SELECT", "INSERT"},
		"ssh_identity_material_revisions":  {"SELECT", "INSERT"},
		"ssh_identity_ack_rejections":      {"SELECT", "INSERT"},
		"ssh_identity_quarantines":         {"SELECT", "INSERT"},
		"ssh_identity_health_probes":       {"SELECT", "INSERT", "UPDATE"},
		"ssh_invocation_identity_bindings": {"SELECT", "INSERT"},
		"ssh_invocation_claim_cursors":     {"SELECT", "INSERT", "UPDATE"},
		"ssh_execution_intents":            {"SELECT", "INSERT"},
		"ssh_execution_observations":       {"SELECT", "INSERT"},
	}
	for table := range required {
		if _, err := admin.Exec(fixture.ctx, "GRANT SELECT ON "+pgx.Identifier{"control_plane", table}.Sanitize()+" TO "+role); err != nil {
			t.Fatal("grant isolated SSH SELECT baseline")
		}
	}
	var oldAny bool
	if probe.QueryRow(fixture.ctx, "SELECT has_table_privilege(session_user,$1,'SELECT,INSERT,UPDATE')", "control_plane.ssh_execution_intents").Scan(&oldAny) != nil || !oldAny {
		t.Fatal("SQL privilege counterfactual did not reproduce ANY semantics")
	}
	check(t, false)
	for table, privileges := range required {
		for _, privilege := range privileges {
			if _, err := admin.Exec(fixture.ctx, "GRANT "+privilege+" ON "+pgx.Identifier{"control_plane", table}.Sanitize()+" TO "+role); err != nil {
				t.Fatal("grant exact SSH owner privilege")
			}
		}
	}
	check(t, true)
	for table, privileges := range required {
		for _, privilege := range privileges {
			t.Run(table+"/without_"+privilege, func(t *testing.T) {
				object := pgx.Identifier{"control_plane", table}.Sanitize()
				if _, err := admin.Exec(fixture.ctx, "REVOKE "+privilege+" ON "+object+" FROM "+role); err != nil {
					t.Fatal("revoke exact SSH owner privilege")
				}
				check(t, false)
				if _, err := admin.Exec(fixture.ctx, "GRANT "+privilege+" ON "+object+" TO "+role); err != nil {
					t.Fatal("restore exact SSH owner privilege")
				}
				check(t, true)
			})
		}
	}
	if _, err := admin.Exec(fixture.ctx, "REVOKE EXECUTE ON FUNCTION "+currentFunction+" FROM "+role); err != nil {
		t.Fatal("revoke SSH source function")
	}
	check(t, false)
	if _, err := admin.Exec(fixture.ctx, "GRANT EXECUTE ON FUNCTION "+currentFunction+" TO "+role); err != nil {
		t.Fatal("restore SSH source function")
	}
	check(t, true)
	for _, item := range []struct{ table, privilege string }{
		{"ssh_identity_setups", "DELETE"},
		{"ssh_identity_upload_attempts", "TRUNCATE"},
		{"ssh_execution_intents", "UPDATE"},
		{"ssh_execution_observations", "REFERENCES"},
	} {
		t.Run(item.table+"/with_"+item.privilege, func(t *testing.T) {
			table := pgx.Identifier{"control_plane", item.table}.Sanitize()
			if _, err := admin.Exec(fixture.ctx, "GRANT "+item.privilege+" ON "+table+" TO "+role); err != nil {
				t.Fatal("introduce excess disposable SSH privilege")
			}
			check(t, false)
			if _, err := admin.Exec(fixture.ctx, "REVOKE "+item.privilege+" ON "+table+" FROM "+role); err != nil {
				t.Fatal("revoke excess disposable SSH privilege")
			}
			check(t, true)
		})
	}
	guards := []struct{ table, trigger string }{
		{"ssh_execution_intents", "ssh_execution_intent_owner"},
		{"ssh_invocation_identity_bindings", "ssh_invocation_identity_immutable"},
		{"ssh_invocation_identity_bindings", "ssh_invocation_identity_purge_only"},
		{"ssh_execution_intents", "ssh_execution_intent_immutable"},
		{"ssh_execution_intents", "ssh_execution_intent_purge_only"},
		{"ssh_execution_observations", "ssh_execution_observation_immutable"},
		{"ssh_execution_observations", "ssh_execution_observation_purge_only"},
	}
	for _, guard := range guards {
		t.Run(guard.trigger, func(t *testing.T) {
			table := pgx.Identifier{"control_plane", guard.table}.Sanitize()
			trigger := pgx.Identifier{guard.trigger}.Sanitize()
			if _, err := admin.Exec(fixture.ctx, "ALTER TABLE "+table+" DISABLE TRIGGER "+trigger); err != nil {
				t.Fatal("disable exact disposable SSH guard")
			}
			check(t, false)
			if _, err := admin.Exec(fixture.ctx, "ALTER TABLE "+table+" ENABLE TRIGGER "+trigger); err != nil {
				t.Fatal("restore exact disposable SSH guard")
			}
			check(t, true)
		})
	}
}
