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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func contextProjectReader(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, kind string) value.Principal {
	t.Helper()
	externalID := "20000000-0000-4000-8000-000000008461"
	if kind == "SKILL" {
		externalID = "20000000-0000-4000-8000-000000008462"
	}
	input := platformrepo.ProofPrincipalInput{ExternalActorID: externalID, ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Context reader " + kind, CallerWorkload: "control-api-gateway", Operation: "platform.query.projects.get", ProjectRef: projectRef}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound context reader: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName, Page: query.Page{Size: 20}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("context reader subject: count=%d err=%v", len(subjects), err)
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "context-reader-role-" + kind}, Payload: command.AccessRoleInput{
			Name: input.ExternalDisplayName, PermissionKeys: []string{"project.view"}, AllowedScopes: []string{"PROJECT"}, ChangeComment: "Context eligibility fixture"}})
	if err != nil || role.AccessRole == nil {
		t.Fatalf("context reader role: %v", err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "context-reader-binding-" + kind}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef}}})
	if err != nil {
		t.Fatalf("context reader binding: %v", err)
	}
	return resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
}

func testContextVFS(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, resourceRef, kind, digest string, visible bool) {
	t.Helper()
	folder := "skills"
	if kind == "MEMORY" {
		folder = "memories"
	}
	parent := "/projects/" + projectRef + "/" + folder
	filter := query.Filter{ProjectRef: projectRef, ResourceRef: parent, Page: query.Page{Size: 1}}
	nodes, total, next, err := service.ListVFSNodes(ctx, owner, filter)
	want := 0
	if visible {
		want = 1
	}
	if err != nil || total != int64(want) || len(nodes) != want || next != "" {
		t.Fatalf("typed context VFS tree: count=%d total=%d next=%t err=%v", len(nodes), total, next != "", err)
	}
	if visible && (nodes[0].EntityRef != resourceRef || nodes[0].Kind != kind || nodes[0].Digest != digest || nodes[0].Path != parent+"/"+resourceRef) {
		t.Fatal("typed context VFS lost owner identity/digest")
	}
	nodes, total, next, err = service.SearchVFS(ctx, owner, query.Filter{Query: resourceRef, Page: query.Page{Size: 1}})
	if err != nil || total != int64(want) || len(nodes) != want || next != "" {
		t.Fatalf("typed context VFS global search: count=%d total=%d err=%v", len(nodes), total, err)
	}
}
