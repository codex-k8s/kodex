package platform

import (
	"context"
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testRunCancellationLockOrder(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.cancel",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "cancel-lock-project"},
		Payload:  command.ProjectInput{Name: "Cancel lock order", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create cancellation project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "cancel-lock-agent", "Cancellation lock agent")
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "cancel-lock-run"},
		Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
			Task: "Remain available while lock ordering is checked."},
	})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch cancellation run: run=%#v err=%v", launched.Run, err)
	}

	runtimeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtimeTx.Rollback(ctx) }()
	var rootRunID, nodeID string
	if err := runtimeTx.QueryRow(ctx, `SELECT root_run_id::text FROM control_plane.runs WHERE ref=$1`, launched.Run.Ref).Scan(&rootRunID); err != nil {
		t.Fatal(err)
	}
	if err := runtimeTx.QueryRow(ctx, `SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid ORDER BY id LIMIT 1 FOR UPDATE`, rootRunID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}

	type cancelResult struct {
		result command.Result
		err    error
	}
	cancelled := make(chan cancelResult, 1)
	go func() {
		version := launched.Run.Version
		result, executeErr := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "cancel-lock-command", ExpectedVersion: &version},
			Payload:  command.RunCommandInput{RunRef: launched.Run.Ref, Reason: "Lock ordering fixture"},
		})
		cancelled <- cancelResult{result: result, err: executeErr}
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()
	for {
		var waiting bool
		err = pool.QueryRow(waitCtx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE query LIKE '%commands_changerun_lock_run_nodes%'
			  AND wait_event_type='Lock'
		)`).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe waiting cancellation: %v", err)
		}
		if waiting {
			break
		}
		select {
		case outcome := <-cancelled:
			t.Fatalf("cancellation did not wait for runtime node lock: run=%#v err=%v", outcome.result.Run, outcome.err)
		case <-waitCtx.Done():
			t.Fatal("cancellation did not reach the runtime node lock")
		case <-time.After(10 * time.Millisecond):
		}
	}

	var lockedRoot string
	if err := runtimeTx.QueryRow(ctx, `SELECT id::text FROM control_plane.runs WHERE id=$1::uuid FOR KEY SHARE`, rootRunID).Scan(&lockedRoot); err != nil {
		t.Fatalf("runtime node-to-run lock order deadlocked: %v", err)
	}
	if nodeID == "" || lockedRoot != rootRunID {
		t.Fatal("runtime lock fixture did not resolve exact node and root run")
	}
	if err := runtimeTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-cancelled:
		if outcome.err != nil || outcome.result.Run == nil || outcome.result.Run.State != "CANCELLED" {
			t.Fatalf("cancel after runtime transaction: run=%#v err=%v", outcome.result.Run, outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation remained blocked after runtime transaction")
	}
}
