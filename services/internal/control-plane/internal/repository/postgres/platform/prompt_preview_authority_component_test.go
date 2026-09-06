package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func previewAuthorityActor(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, key string, permissions []string, target entity.AccessScope) (value.Principal, string) {
	t.Helper()
	identity := platformrepo.ProofPrincipalInput{ExternalActorID: uuid.NewString(), ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: key, CallerWorkload: "control-api-gateway", Operation: "platform.query.schedules.preview"}
	if _, err := r.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound preview actor: %v", err)
	}
	subjects, _, err := s.ListAccessSubjects(ctx, owner, query.Filter{Query: key}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("preview actor: %v", err)
	}
	previewAuthorityGrant(t, ctx, s, owner, key, subjects[0].Ref, permissions, target)
	return resolvedTestPrincipal(t, ctx, r, identity, "control-api-gateway"), subjects[0].Ref
}

func previewAuthorityGrant(t *testing.T, ctx context.Context, s *platformservice.Service, owner value.Principal, key, subject string, permissions []string, target entity.AccessScope) *entity.AccessBinding {
	t.Helper()
	role, err := s.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{target.Kind}, ChangeComment: "Scoped preview fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := s.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subject, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: target}})
	if err != nil {
		t.Fatal(err)
	}
	return bound.AccessBinding
}

func testSchedulePreviewAuthority(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, input command.ScheduleInput, current entity.Schedule) {
	t.Helper()
	when := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	actor, subject := previewAuthorityActor(t, ctx, r, s, owner, "automation-preview-editor", []string{"project.view", "project.manage", "agent.view", "workflow.view", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: input.ProjectRef})
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, actor, input, "", 0, when, "", false, "DRAFT"); err != nil {
		t.Fatalf("project manager new preview: %v", err)
	}
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, actor, input, current.Ref, current.Version+1, when, "", false, "DRAFT"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("missing exact schedule manage before OCC: %v", err)
	}
	previewAuthorityGrant(t, ctx, s, owner, "automation-preview-exact", subject, []string{"schedule.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "SCHEDULE", ResourceRef: current.Ref})
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, actor, input, current.Ref, current.Version, when, "", false, "DRAFT"); err != nil {
		t.Fatalf("exact schedule manager preview: %v", err)
	}
	_, savedPin, _, err := s.PreviewScheduleMaterialization(ctx, actor, input, current.Ref, current.Version, when, "", false, "CURRENT_REVISION")
	_, draftPin, _, draftErr := s.PreviewScheduleMaterialization(ctx, actor, input, current.Ref, current.Version, when, "", false, "DRAFT")
	if err != nil || draftErr != nil || savedPin.ExecutionActorRef == draftPin.ExecutionActorRef || draftPin.ExecutionActorRef != subject || savedPin.BaseRevisionRef != current.CurrentRevision.Ref {
		t.Fatalf("viewer substituted revision author: saved=%+v draft=%+v err=%v/%v", savedPin, draftPin, err, draftErr)
	}
	otherInput := input
	otherInput.Name = "Other preview schedule"
	other, err := s.Execute(ctx, command.Command{Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "automation-preview-other"}, Payload: otherInput})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, actor, otherInput, other.Schedule.Ref, other.Schedule.Version+1, when, "", false, "DRAFT"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("other schedule inherited permission: %v", err)
	}
	previewAuthorityGrant(t, ctx, s, owner, "automation-preview-other-manage", subject, []string{"schedule.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "SCHEDULE", ResourceRef: other.Schedule.Ref})
	launchBinding := previewAuthorityGrant(t, ctx, s, owner, "automation-preview-launch", subject, []string{"agent.launch"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "AGENT", ResourceRef: input.Target.Ref})
	otherInput.Ref = other.Schedule.Ref
	updated, err := s.Execute(ctx, command.Command{Kind: command.UpdateSchedule, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "automation-preview-new-author", ExpectedVersion: &other.Schedule.Version}, Payload: otherInput})
	if err != nil {
		t.Fatalf("save same specification with new author: %v", err)
	}
	viewer := owner
	viewer.Permission = "platform.query.schedules.preview"
	_, authorPin, _, err := s.PreviewScheduleMaterialization(ctx, viewer, otherInput, updated.Schedule.Ref, updated.Schedule.Version, when, "", false, "CURRENT_REVISION")
	if err != nil || authorPin.ExecutionActorRef != subject || authorPin.RevisionRef == other.Schedule.CurrentRevision.Ref {
		t.Fatalf("saved author pin: %+v %v", authorPin, err)
	}
	if _, err := s.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "automation-preview-revoke-author", ExpectedVersion: &launchBinding.Version}, Payload: command.AccessBindingInput{BindingRef: launchBinding.Ref}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, viewer, otherInput, updated.Schedule.Ref, updated.Schedule.Version, when, "", false, "CURRENT_REVISION"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked author inherited viewer launch: %v", err)
	}
	foreignProject, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "automation-preview-foreign-project"}, Payload: command.ProjectInput{Name: "Foreign preview project", Purpose: "Signed boundary", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := owner
	foreign.Permission = "platform.query.schedules.preview"
	foreign.ProjectRef = gateTestProjectID(t, ctx, r, owner, foreignProject.Project.Ref)
	if _, _, _, err := s.PreviewScheduleMaterialization(ctx, foreign, input, current.Ref, current.Version+1, when, "", false, "DRAFT"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign signed scope before OCC: %v", err)
	}
}
