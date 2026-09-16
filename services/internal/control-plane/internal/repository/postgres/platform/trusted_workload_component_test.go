package platform

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTrustedWorkloadGenerationComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository := &Repository{pool: pool}
	if err := repository.ConfigureRPCProfile("implicit"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if _, err := repository.ResolveServiceCredentialGeneration(ctx, "secret-broker"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("protected reader used trusted registration")
	}
	if err := repository.ConfigureRPCProfile(transportprofile.TrustedCluster); err != nil {
		t.Fatal(err)
	}
	generation, err := repository.ResolveServiceCredentialGeneration(ctx, "secret-broker")
	if err != nil || generation != 1 {
		t.Fatalf("fresh trusted registration: generation=%d error=%v", generation, err)
	}
	if _, err := repository.ResolveServiceCredentialGeneration(ctx, "unknown-workload"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unknown trusted workload accepted")
	}
	for _, statement := range []string{
		"UPDATE control_plane.trusted_workload_generations SET credential_generation = 99 WHERE workload_id = 'secret-broker'",
		"DELETE FROM control_plane.trusted_workload_generations WHERE workload_id = 'secret-broker'",
	} {
		_, err := pool.Exec(ctx, statement)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
			t.Fatal("runtime changed the trusted registry")
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := repository.AcceptWorkerGrant(ctx, platformrepo.WorkerGrantInput{
		WorkloadID: "secret-broker", CredentialGeneration: 7,
		Revision: uint64(now.Unix()), IssuedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("establish existing protected generation: %v", err)
	}
	generation, err = repository.ResolveServiceCredentialGeneration(ctx, "secret-broker")
	if err != nil || generation != 7 {
		t.Fatal("trusted reader regressed below the durable protected floor")
	}
	restarted := &Repository{pool: pool}
	if err := restarted.ConfigureRPCProfile(transportprofile.TrustedCluster); err != nil {
		t.Fatal(err)
	}
	generation, err = restarted.ResolveServiceCredentialGeneration(ctx, "secret-broker")
	if err != nil || generation != 7 {
		t.Fatal("repository restart lost durable generation")
	}
}
