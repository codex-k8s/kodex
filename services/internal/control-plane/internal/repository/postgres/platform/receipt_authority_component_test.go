package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func receiptFreshPrincipal(t *testing.T, ctx context.Context, repository *Repository, projectRef string) value.Principal {
	t.Helper()
	operation := "platform.query.projects.get"
	if projectRef == "" {
		operation = "platform.query.bootstrap.get"
	}
	authority, err := repository.ResolveProofAuthority(ctx, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000008461", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: operation, ProjectRef: projectRef})
	if err != nil {
		t.Fatalf("fresh retained-access proof: %v", err)
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, ProjectRef: authority.ProjectID, Permission: operation, CorrelationRef: "receipt-authority-component", CallerWorkload: "control-api-gateway", CredentialRevision: 1}
}

func receiptAccessBinding(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, actorRef, key string, permissions []string, target entity.AccessScope) entity.AccessBinding {
	t.Helper()
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{target.Kind}, ChangeComment: "Receipt authority fixture"}})
	if err != nil || role.AccessRole == nil {
		t.Fatalf("receipt role: %v", err)
	}
	bound, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: actorRef, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: target}})
	if err != nil || bound.AccessBinding == nil {
		t.Fatalf("receipt binding: %v", err)
	}
	return *bound.AccessBinding
}

func prepareDeletedReceiptAuthorityFixture(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, input *command.Command, kind, ref, projectRef, permission string) func() {
	t.Helper()
	key := "delete-receipt-" + kind
	if projectRef == "" {
		project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-project"}, Payload: command.ProjectInput{Name: key, Language: "en"}})
		if err != nil || project.Project == nil {
			t.Fatal(err)
		}
		projectRef = project.Project.Ref
	}
	reader := contextProjectReader(t, ctx, repository, service, owner, projectRef, key)
	actor, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: kind, ResourceRef: ref, ProjectRef: projectRef}
	proofProject := projectRef
	if kind == "INTEGRATION" {
		target.ProjectRef = ""
		proofProject = ""
		receiptAccessBinding(t, ctx, service, owner, actor.ActorID, key+"-org", []string{"organization.view"}, entity.AccessScope{Kind: "ORGANIZATION"})
	}
	binding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, key, []string{permission, visibilityPermission(kind)}, target)
	fresh := func() value.Principal {
		return receiptFreshPrincipal(t, ctx, repository, proofProject)
	}
	input.Principal = fresh()
	return func() {
		t.Helper()
		foreign, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-foreign-project"}, Payload: command.ProjectInput{Name: key + " foreign", Language: "en"}})
		if err != nil || foreign.Project == nil {
			t.Fatal(err)
		}
		receiptAccessBinding(t, ctx, service, owner, actor.ActorID, key+"-foreign-view", []string{"project.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: foreign.Project.Ref})
		foreignReplay := *input
		foreignReplay.Principal = receiptFreshPrincipal(t, ctx, repository, foreign.Project.Ref)
		if result, err := service.Execute(ctx, foreignReplay); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) || result.Connection != nil || result.Schedule != nil || result.RuntimeEnvironment != nil {
			t.Fatalf("foreign signed project exposed tombstone: %v", err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-revoke", ExpectedVersion: &binding.Version}, Payload: command.AccessBindingInput{BindingRef: binding.Ref}})
		if err != nil {
			t.Fatal(err)
		}
		denied := *input
		denied.Principal = fresh()
		for _, stale := range []bool{false, true} {
			if stale {
				version := int64(1)
				denied.Mutation.ExpectedVersion = &version
			}
			result, err := service.Execute(ctx, denied)
			if !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) || result.Connection != nil || result.Schedule != nil || result.RuntimeEnvironment != nil {
				t.Fatalf("revoked terminal receipt exposed snapshot: %v", err)
			}
		}
		// Следующие проверки неизвестного receipt выполняются первоначальным owner.
		input.Principal = owner
	}
}
