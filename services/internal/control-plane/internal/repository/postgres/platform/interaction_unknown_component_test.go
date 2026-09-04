package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testInteractionUnknownOutcome(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, tenant string
	err = tx.QueryRow(ctx, `INSERT INTO control_plane.interaction_deliveries
(ref,organization_id,project_id,connection_id,grant_id,root_run_id,capability_key,message_key,template_data,
state,attempt,generation,lease_ref,fence_digest,workload_instance,lease_expires_at)
SELECT 'interaction_unknown_fixture', run.organization_id,run.project_id,g.connection_id,g.id,run.root_run_id,
'mattermost.notifications','TEST_DELIVERY','{}','CLAIMED',1,1,'interaction_unknown_lease',repeat('a',64),'interaction-test',clock_timestamp()-interval '1 second'
FROM control_plane.integration_grants g
JOIN control_plane.agents agent ON g.target_kind='AGENT' AND g.target_ref=agent.ref
JOIN control_plane.runs run ON run.project_id=agent.project_id
WHERE run.project_id IS NOT NULL ORDER BY run.id LIMIT 1 RETURNING id::text,organization_id::text`).Scan(&id, &tenant)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, queryInteractionClaimDeliveries, pgx.StrictNamedArgs{"organization_id": tenant, "workload_instance": "interaction-test", "claim_limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	assertState := func(expected string) {
		t.Helper()
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM control_plane.interaction_deliveries WHERE id=$1::uuid`, id).Scan(&state); err != nil || state != expected {
			t.Fatalf("delivery state=%s, want %s: %v", state, expected, err)
		}
	}
	assertState("UNKNOWN_OUTCOME")
	rows, err = tx.Query(ctx, queryInteractionClaimDeliveries, pgx.StrictNamedArgs{"organization_id": tenant, "workload_instance": "interaction-test", "claim_limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	assertState("UNKNOWN_OUTCOME")
	for _, test := range []struct {
		success, noEffect bool
		state             string
	}{{false, false, "UNKNOWN_OUTCOME"}, {false, true, "FAILED"}, {true, false, "SUCCEEDED"}} {
		var ref, state string
		err := tx.QueryRow(ctx, queryInteractionCompleteDeliveryUpdate, pgx.StrictNamedArgs{
			"delivery_id": id, "success": test.success, "confirmed_no_effect": test.noEffect, "external_post_ref": "post-fixture",
			"external_thread_ref": "", "safe_error_code": "INTERACTION_UNAVAILABLE", "attempt": 1,
			"external_team_ref": "team-fixture", "external_channel_ref": "channel-fixture",
		}).Scan(&ref, &state)
		if err != nil || state != test.state {
			t.Fatalf("completion state=%s, want %s: %v", state, test.state, err)
		}
	}
	incident := projectInteractionIncident(entity.Incident{}, "UNKNOWN_OUTCOME", 1)
	if incident.State != "OPEN" || incident.SafeNextStep != "i18n:INTERACTION_DELIVERY_RECONCILIATION_REQUIRED" {
		t.Fatal("unknown outcome advertised automatic recovery")
	}
}
