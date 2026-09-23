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

func testProjectTrashCancelsRuns(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID:  "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload:   "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-trash-run-fixture"},
		Payload:  command.ProjectInput{Name: "Project trash run fixture", Language: "en"},
	})
	if err != nil || created.Project == nil {
		t.Fatalf("create run fixture project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, created.Project.Ref, "project-trash-run-agent", "Project trash runner")
	launched, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-trash-active-run"},
		Payload: command.LaunchRunInput{ProjectRef: created.Project.Ref, Title: "Project trash active run",
			Task: "Verify terminal cancellation", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}},
	})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch run fixture: %v", err)
	}
	trashed, err := service.Execute(ctx, command.Command{
		Kind: command.TrashProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-trash-active-run-delete", ExpectedVersion: &created.Project.Version},
		Payload:  command.ProjectLifecycleInput{Ref: created.Project.Ref},
	})
	if err != nil || trashed.Project == nil || trashed.Project.Lifecycle != "TRASHED" ||
		trashed.Project.DeletedAt == nil || trashed.Project.PurgeAfter == nil ||
		trashed.Project.PurgeAfter.Sub(*trashed.Project.DeletedAt) != 30*24*time.Hour {
		t.Fatalf("trash project with active run: project=%#v err=%v", trashed.Project, err)
	}
	var runState string
	var openNodes, openGates, claimedLeases int
	err = pool.QueryRow(ctx, `SELECT run.state,
		(SELECT count(*) FROM control_plane.run_nodes node WHERE node.root_run_id=run.root_run_id AND node.state IN ('PLANNED','QUEUED','RUNNING','WAITING')),
		(SELECT count(*) FROM control_plane.owner_gates gate WHERE gate.root_run_id=run.root_run_id AND gate.state='OPEN'),
		(SELECT count(*) FROM control_plane.runtime_leases lease WHERE lease.run_id=run.id AND lease.state='CLAIMED')
		FROM control_plane.runs run WHERE run.ref=$1`, launched.Run.Ref).Scan(&runState, &openNodes, &openGates, &claimedLeases)
	if err != nil || runState != "CANCELLED" || openNodes != 0 || openGates != 0 || claimedLeases != 0 {
		t.Fatalf("run graph survived project trash: state=%s nodes=%d gates=%d leases=%d err=%v", runState, openNodes, openGates, claimedLeases, err)
	}
	restored, err := service.Execute(ctx, command.Command{
		Kind: command.RestoreProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-trash-active-run-restore", ExpectedVersion: &trashed.Project.Version},
		Payload:  command.ProjectLifecycleInput{Ref: created.Project.Ref},
	})
	if err != nil || restored.Project == nil || restored.Project.Lifecycle != "ACTIVE" {
		t.Fatalf("restore project with cancelled run: project=%#v err=%v", restored.Project, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.runs WHERE ref=$1`, launched.Run.Ref).Scan(&runState); err != nil || runState != "CANCELLED" {
		t.Fatalf("restore resumed cancelled run: state=%s err=%v", runState, err)
	}
}
