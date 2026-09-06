package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/workflow_readiness_disable_agent.sql
var queryWorkflowReadinessDisableAgent string

func testWorkflowLaunchReadiness(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, workflow *entity.Workflow) {
	t.Helper()
	if workflow.LaunchReadiness == nil || !workflow.LaunchReadiness.AllowedToSubmit || workflow.LaunchReadiness.Reason != "READY" {
		t.Fatal("published Workflow has no authoritative launch readiness")
	}
	actor, subject := previewAuthorityActor(t, ctx, r, s, owner, "workflow-readiness-viewer", []string{"workflow.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: workflow.ProjectRef, ResourceKind: "WORKFLOW", ResourceRef: workflow.Ref})
	read, err := s.GetWorkflow(ctx, actor, workflow.Ref)
	if err != nil || read.LaunchReadiness == nil || read.LaunchReadiness.AllowedToSubmit || read.LaunchReadiness.Reason != "PERMISSION_REQUIRED" {
		t.Fatalf("Workflow viewer reason: %+v %v", read.LaunchReadiness, err)
	}
	if _, err := s.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "workflow-readiness-denied"}, Payload: command.LaunchRunInput{ProjectRef: workflow.ProjectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "No effect", Input: map[string]any{"record": "fixture"}}}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("readiness/launch permission parity: %v", err)
	}
	previewAuthorityGrant(t, ctx, s, owner, "workflow-readiness-launch", subject, []string{"workflow.launch"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: workflow.ProjectRef, ResourceKind: "WORKFLOW", ResourceRef: workflow.Ref})
	read, err = s.GetWorkflow(ctx, actor, workflow.Ref)
	if err != nil || !read.LaunchReadiness.AllowedToSubmit {
		t.Fatalf("Workflow launch requires unrelated file permission: %+v %v", read.LaunchReadiness, err)
	}
	items, _, err := s.ListWorkflows(ctx, actor, query.Filter{ProjectRef: workflow.ProjectRef, Query: workflow.Name})
	if err != nil || len(items) != 1 || items[0].LaunchReadiness == nil || items[0].LaunchReadiness.ContextDigest != read.LaunchReadiness.ContextDigest {
		t.Fatalf("Workflow single/list parity: count=%d err=%v", len(items), err)
	}
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve Workflow fixture owner: %v", err)
	}
	current, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("scope Workflow fixture owner: %v", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resolvedActor, err := r.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatalf("resolve Workflow launch actor: %v", err)
	}
	actorScope, err := r.resolveScope(ctx, resolvedActor)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: workflow.ProjectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Permission parity", Input: map[string]any{"record": "fixture"}}}
	if err := r.authorizeCommand(ctx, probe, actorScope, launch); err != nil {
		t.Fatalf("Workflow-only launch authority: %v", err)
	}
	if _, err := r.launchRun(ctx, probe, actorScope, launch); err != nil {
		t.Fatalf("Workflow-only actual launch: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if tag, err := tx.Exec(ctx, queryWorkflowReadinessDisableAgent, current.organizationID, workflow.CoordinatorAgentRef); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("disable dependency fixture: %v", err)
	}
	copy := *workflow
	if err := r.projectWorkflowLaunchReadiness(ctx, tx, current, []*entity.Workflow{&copy}); err != nil || copy.LaunchReadiness.AllowedToSubmit || copy.LaunchReadiness.Reason != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("dependency readiness: %+v %v", copy.LaunchReadiness, err)
	}
	if _, err := r.launchRun(ctx, tx, current, command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: workflow.ProjectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "No effect", Input: map[string]any{"record": "fixture"}}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("dependency launch parity: %v", err)
	}
}
