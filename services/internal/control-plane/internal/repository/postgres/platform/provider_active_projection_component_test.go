package platform

import (
	"context"
	_ "embed"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/provider_active_projection_pins.sql
var queryProviderActiveProjectionPins string

//go:embed testdata/sql/provider_active_projection_deleting.sql
var queryProviderActiveProjectionDeleting string

//go:embed testdata/sql/provider_stt_deleting.sql
var queryProviderSTTDeleting string

func testDeletingAccountRetainsExactActiveProjection(t *testing.T, ctx context.Context, repository *Repository, leaseRef string) {
	t.Helper()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var input platformrepo.RuntimeCredentialProjectionInput
	err = tx.QueryRow(ctx, queryProviderActiveProjectionPins, leaseRef).Scan(&input.Authority.TenantID, &input.Authority.ActorID, &input.Authority.ProjectID,
		&input.LeaseRef, &input.WorkloadInstance, &input.Generation, &input.RuntimeRevisionRef, &input.RuntimeRevisionDigest,
		&input.Attempt, &input.InputDigest, &input.SessionRef, &input.TurnRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, queryProviderActiveProjectionDeleting, leaseRef); err != nil {
		t.Fatal(err)
	}
	args := pgx.StrictNamedArgs{"organization_id": input.Authority.TenantID, "actor_id": input.Authority.ActorID, "project_id": input.Authority.ProjectID,
		"lease_ref": input.LeaseRef, "system_assistant": false, "workload_instance": input.WorkloadInstance, "generation": input.Generation, "fence": "",
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_digest": input.RuntimeRevisionDigest, "attempt": input.Attempt, "input_digest": input.InputDigest, "session_ref": input.SessionRef, "turn_ref": input.TurnRef}
	rows, err := tx.Query(ctx, queryCredentialProjectionResolveRuntime, args)
	if err != nil {
		t.Fatal(err)
	}
	found := rows.Next()
	rows.Close()
	if !found || rows.Err() != nil {
		t.Fatalf("deleting account revoked exact active projection: %v", rows.Err())
	}
	args["generation"] = input.Generation + 1
	rows, err = tx.Query(ctx, queryCredentialProjectionResolveRuntime, args)
	if err != nil {
		t.Fatal(err)
	}
	found = rows.Next()
	rows.Close()
	if found || rows.Err() != nil {
		t.Fatalf("deleting account accepted another generation: %v", rows.Err())
	}
}
