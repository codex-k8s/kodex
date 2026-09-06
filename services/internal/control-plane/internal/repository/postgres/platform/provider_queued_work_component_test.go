package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/provider_queued_account.sql
var queryProviderQueuedFixtureAccount string

func testProviderQueuedWorkSnapshot(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.queued-work.cancel",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "queued-cleanup-project"}, Payload: command.ProjectInput{Name: "Queued cleanup", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create queued project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "queued-cleanup-agent", "Queued cleanup")
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "queued-cleanup-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Remain queued until explicitly cancelled."}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch queued run: %v", err)
	}
	var accountRef string
	if err := repository.pool.QueryRow(ctx, queryProviderQueuedFixtureAccount, launched.Run.Ref).Scan(&accountRef); err != nil {
		t.Fatal(err)
	}
	input := query.ProviderAccountBlockers{AccountRef: accountRef, Page: query.Page{Size: 100}}
	all, err := service.ListProviderAccountBlockers(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Kind = "QUEUED_TURN"
	input.Query = launched.Run.Title
	filtered, err := service.ListProviderAccountBlockers(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.ContextDigest != all.ContextDigest {
		t.Fatal("filter changed the authoritative blockers snapshot digest")
	}
	found := false
	for _, item := range filtered.Items {
		if item.Ref == launched.Run.Ref && item.CanCancel {
			found = true
		}
	}
	if !found {
		t.Fatal("exact cancellable queued run is absent")
	}
	cancel := command.Command{Kind: command.CancelProviderAccountQueuedWork, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "queued-cleanup-exact", ExpectedVersion: &all.AccountVersion}, Payload: command.ProviderAccountInput{AccountRef: accountRef, SelectedRunRefs: []string{launched.Run.Ref}, BlockersDigest: all.ContextDigest}}
	result, err := service.Execute(ctx, cancel)
	if err != nil || len(result.ProviderQueuedWorkResults) != 1 || result.ProviderQueuedWorkResults[0].Outcome != "CANCELLED" {
		t.Fatalf("cancel queued result: %#v %v", result.ProviderQueuedWorkResults, err)
	}
	replay, err := service.Execute(ctx, cancel)
	if err != nil || len(replay.ProviderQueuedWorkResults) != 1 || replay.ProviderQueuedWorkResults[0].Outcome != "CANCELLED" {
		t.Fatalf("exact cancellation receipt: %#v %v", replay.ProviderQueuedWorkResults, err)
	}
	current, err := service.GetRun(ctx, owner, launched.Run.Ref)
	if err != nil || current.State != "CANCELLED" {
		t.Fatalf("cancelled graph readback: %s %v", current.State, err)
	}
	after, err := service.ListProviderAccountBlockers(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	if after.ContextDigest == all.ContextDigest {
		t.Fatal("cancelled run retained the old blockers digest")
	}
	cancel.Mutation.IdempotencyKey = "queued-cleanup-stale"
	if _, err := service.Execute(ctx, cancel); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale blockers command: %v", err)
	}
}
