package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestServiceCredentialColdStartComponent uses an isolated migrated PostgreSQL clone to prove registered cold identities, shared floors, disabled admission and runtime write denial. Grant inputs represent the already verified owner port; cryptographic verification remains covered by authorityproof tests.
// TestServiceCredentialColdStartComponent проверяет на изолированной мигрированной PostgreSQL холодные регистрации, общие floors, запрет disabled и SQL-записи runtime. Grant inputs относятся к уже проверенному owner port; криптографию проверяют authorityproof tests.
func TestServiceCredentialColdStartComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository := &Repository{pool: pool}
	for _, workload := range []string{"runtime-controller", "session-archive", "image-admission-controller"} {
		var watermarks int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.worker_grant_high_watermarks WHERE workload_id=$1", workload).Scan(&watermarks); err != nil || watermarks != 0 {
			t.Fatal("cold fixture contains an accepted signed grant")
		}
		for _, profile := range []string{"", transportprofile.TrustedCluster} {
			if err := repository.ConfigureRPCProfile(profile); err != nil {
				t.Fatal(err)
			}
			if generation, err := repository.ResolveServiceCredentialGeneration(ctx, workload); err != nil || generation != 1 {
				t.Fatalf("registered cold workload rejected: workload=%s generation=%d err=%v", workload, generation, err)
			}
		}
	}
	if _, err := repository.ResolveServiceCredentialGeneration(ctx, "unknown-workload"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unknown registration used an implicit generation")
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.trusted_workload_generations SET enabled=false WHERE workload_id='runtime-controller'"); err == nil {
		t.Fatal("runtime changed owner registration")
	} else {
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
			t.Fatal("runtime registration write did not fail with insufficient privilege")
		}
	}

	ownerConfig := pool.Config().ConnConfig.Copy()
	if !strings.HasPrefix(ownerConfig.Database, "kodex_assistant_test_") || ownerConfig.Port < 1024 ||
		(ownerConfig.Host != "127.0.0.1" && ownerConfig.Host != "localhost") {
		t.Fatal("owner counterfactual requires the exact isolated loopback clone")
	}
	ownerConfig.User, ownerConfig.Password = "postgres", ""
	owner, err := pgx.ConnectConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal("connect isolated fixture owner")
	}
	defer func() { _ = owner.Close(context.WithoutCancel(ctx)) }()
	if _, err := owner.Exec(ctx, "SET ROLE control_plane_owner"); err != nil {
		t.Fatal("select isolated registration owner")
	}
	now := time.Now().UTC().Truncate(time.Second)
	grant := platformrepo.WorkerGrantInput{WorkloadID: "runtime-controller", InstanceID: uuid.NewString(),
		EnvelopeSHA256: strings.Repeat("a", 64), CredentialGeneration: 7, Revision: uint64(now.Unix()),
		IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := repository.AcceptWorkerGrant(ctx, grant); err != nil {
		t.Fatal("accept verified fixture grant")
	}
	for _, profile := range []string{"", transportprofile.TrustedCluster} {
		restarted := &Repository{pool: pool}
		if err := restarted.ConfigureRPCProfile(profile); err != nil {
			t.Fatal(err)
		}
		if generation, err := restarted.ResolveServiceCredentialGeneration(ctx, grant.WorkloadID); err != nil || generation != 7 {
			t.Fatal("restart/profile switch regressed below accepted signed floor")
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE control_plane.trusted_workload_generations SET enabled=false WHERE workload_id=$1", grant.WorkloadID); err != nil {
		t.Fatal("disable isolated registration")
	}
	for _, profile := range []string{"", transportprofile.TrustedCluster} {
		if err := repository.ConfigureRPCProfile(profile); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ResolveServiceCredentialGeneration(ctx, grant.WorkloadID); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("disabled service registration was accepted")
		}
	}
	if err := repository.AcceptWorkerGrant(ctx, grant); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("signed grant bypassed disabled registration")
	}
	if _, err := owner.Exec(ctx, "UPDATE control_plane.trusted_workload_generations SET enabled=true WHERE workload_id=$1", grant.WorkloadID); err == nil {
		t.Fatal("re-enabled registration without advancing generation")
	}
	if _, err := owner.Exec(ctx, "UPDATE control_plane.trusted_workload_generations SET enabled=true,credential_generation=8 WHERE workload_id=$1", grant.WorkloadID); err != nil {
		t.Fatal("advance and re-enable isolated registration")
	}
	if err := repository.AcceptWorkerGrant(ctx, grant); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("signed grant bypassed advanced registration floor")
	}
	advanced := grant
	advanced.CredentialGeneration, advanced.InstanceID, advanced.EnvelopeSHA256 = 8, uuid.NewString(), strings.Repeat("b", 64)
	if err := repository.AcceptWorkerGrant(ctx, advanced); err != nil {
		t.Fatal("registered current generation was rejected")
	}
	expired := advanced
	expired.InstanceID, expired.CredentialGeneration = uuid.NewString(), 9
	expired.IssuedAt, expired.ExpiresAt = now.Add(-5*time.Minute), now.Add(-time.Minute)
	expired.Revision = uint64(expired.IssuedAt.Unix())
	if err := repository.AcceptWorkerGrant(ctx, expired); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired grant advanced generation")
	}
	if generation, err := (&Repository{pool: pool}).ResolveServiceCredentialGeneration(ctx, grant.WorkloadID); err != nil || generation != 8 {
		t.Fatal("closed admission changed committed owner floor")
	}
}
