package platform

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func testVFSEligibility(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "vfs-eligibility-project"}, Payload: command.ProjectInput{Name: "VFS eligibility", Purpose: "Exact source versions and actions", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	project := created.Project.Ref
	root := "/projects/" + project
	nodes, total, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ProjectRef: project, ResourceRef: root})
	if err != nil || total != 0 || len(nodes) != 0 {
		t.Fatalf("inapplicable empty folders: total=%d err=%v", total, err)
	}
	files := make([]entity.Artifact, 0, 2)
	for _, name := range []string{"vfs-literal%_one.txt", "vfs-ordinary-two.txt"} {
		file, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: name}, platformrepo.ArtifactUpload{ProjectRef: project, FileName: name, MediaType: "text/plain", SizeBytes: 4, Reader: strings.NewReader("safe")})
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	filter := query.Filter{ProjectRef: project, ResourceRef: root + "/files", Page: query.Page{Size: 1}}
	nodes, total, next, err := service.ListVFSNodes(ctx, owner, filter)
	if err != nil || total != 2 || len(nodes) != 1 || next == "" {
		t.Fatalf("VFS owner page: total=%d err=%v", total, err)
	}
	first := nodes[0]
	if first.ResourceKind != "ARTIFACT" || first.Version < 1 || first.Revision != 1 || first.LifecycleState != "ACTIVE" || first.ScanState != "CLEAN" || !first.Selectable || first.SelectionReason != "AVAILABLE" || !slices.Contains(first.NextActions, "DELETE") {
		t.Fatal("exact artifact metadata or selection lost")
	}
	filter.Page.Token = next
	nodes, total, _, err = service.ListVFSNodes(ctx, owner, filter)
	if err != nil || total != 2 || len(nodes) != 1 || nodes[0].Ref == first.Ref {
		t.Fatalf("VFS next page: total=%d err=%v", total, err)
	}
	for _, mutate := range []func(*query.Filter){func(f *query.Filter) { f.State = "DELETED" }, func(f *query.Filter) { f.VFSKinds = []string{"INPUT"} }, func(f *query.Filter) { f.Query = "other" }} {
		changed := filter
		mutate(&changed)
		if _, _, _, err := service.ListVFSNodes(ctx, owner, changed); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("foreign VFS filter cursor: %v", err)
		}
	}
	nodes, total, _, err = service.SearchVFS(ctx, owner, query.Filter{ProjectRef: project, Query: "%_", VFSKinds: []string{"INPUT"}})
	if err != nil || total != 1 || len(nodes) != 1 || nodes[0].EntityRef != files[0].Ref {
		t.Fatalf("literal VFS query: total=%d err=%v", total, err)
	}
	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000006421", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "VFS exact reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.vfs.list"}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound VFS reader: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("VFS subject: %v", err)
	}
	bind := func(key string, permissions []string, target entity.AccessScope) entity.AccessBinding {
		t.Helper()
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "VFS authority fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: target}})
		if err != nil {
			t.Fatal(err)
		}
		return *result.AccessBinding
	}
	bind("vfs-project-read", []string{"project.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: project, ProjectRef: project})
	granted := bind("vfs-one-artifact", []string{"artifact.view", "artifact.download"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: files[0].Ref, ProjectRef: project})
	reader := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	assertResponseProjection := func(visible bool) {
		t.Helper()
		resolved, err := repository.ResolvePrincipal(ctx, reader)
		if err != nil {
			t.Fatalf("resolve verified projection principal: %v", err)
		}
		current, err := repository.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatalf("resolve projection actor (visible=%t): %v", visible, err)
		}
		tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		refs := []string{files[1].Ref, files[0].Ref}
		artifact := files[0]
		hidden := files[1]
		response := command.Result{
			Run:      &entity.Run{ArtifactRefs: slices.Clone(refs)},
			Graph:    &entity.RunGraph{Nodes: []entity.RunNode{{ArtifactRefs: slices.Clone(refs)}}},
			Artifact: &artifact,
			Event: &entity.RunEvent{ArtifactRef: files[0].Ref, Delta: entity.RunEventDelta{
				Artifact: &hidden, Run: &entity.RunDelta{ArtifactRefs: slices.Clone(refs)}, Node: &entity.RunNode{ArtifactRefs: slices.Clone(refs)},
			}},
		}
		if err := repository.applyResultActionPermissions(ctx, tx, current, &response, ""); err != nil {
			t.Fatalf("project read/receipt (visible=%t): %v", visible, err)
		}
		expected := []string{}
		if visible {
			expected = []string{files[0].Ref}
		}
		for _, actual := range [][]string{response.Run.ArtifactRefs, response.Graph.Nodes[0].ArtifactRefs, response.Event.Delta.Run.ArtifactRefs, response.Event.Delta.Node.ArtifactRefs} {
			if !slices.Equal(actual, expected) {
				t.Fatal("read or receipt projection retained hidden artifact references")
			}
		}
		if response.Event.Delta.Artifact != nil || (response.Artifact != nil) != visible || (response.Event.ArtifactRef != "") != visible {
			t.Fatal("artifact event or receipt retained revoked metadata")
		}
		if visible && !slices.Equal(response.Artifact.NextActions, []string{"DOWNLOAD"}) {
			t.Fatal("receipt expanded exact reader actions")
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertResponseProjection(true)
	download, err := service.DownloadArtifact(ctx, reader, files[0].Ref, "DOWNLOAD")
	if err != nil {
		t.Fatalf("exact resource download: %v", err)
	}
	n, err := io.Copy(io.Discard, download.Reader)
	_ = download.Reader.Close()
	if err != nil || n != 4 {
		t.Fatalf("exact download bytes: n=%d err=%v", n, err)
	}
	for _, reference := range []string{files[0].Ref, files[1].Ref} {
		file, err := service.GetArtifact(ctx, reader, reference)
		if reference == files[0].Ref {
			if err != nil || !slices.Equal(file.NextActions, []string{"DOWNLOAD"}) {
				t.Fatalf("exact artifact read role: %v", err)
			}
		} else if !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("hidden artifact single read: %v", err)
		}
	}
	nodes, total, _, err = service.ListVFSNodes(ctx, reader, query.Filter{ProjectRef: project, ResourceRef: root + "/files"})
	if err != nil || total != 1 || len(nodes) != 1 || nodes[0].EntityRef != files[0].Ref || nodes[0].Selectable || nodes[0].SelectionReason != "PERMISSION_REQUIRED" || !slices.Equal(nodes[0].NextActions, []string{"DOWNLOAD"}) {
		t.Fatalf("VFS exact read-only source: total=%d err=%v", total, err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "vfs-revoke-read", ExpectedVersion: &granted.Version}, Payload: command.AccessBindingInput{BindingRef: granted.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	assertResponseProjection(false)
	if _, err := service.GetArtifact(ctx, reader, files[0].Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked actor retained single read: %v", err)
	}
	if _, err := service.DownloadArtifact(ctx, reader, files[0].Ref, "DOWNLOAD"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked actor retained download: %v", err)
	}
	nodes, total, _, err = service.SearchVFS(ctx, reader, query.Filter{ProjectRef: project, Query: "vfs-"})
	if err != nil || total != 0 || len(nodes) != 0 {
		t.Fatalf("revoked actor retained search: total=%d err=%v", total, err)
	}
	impact, err := service.GetArtifactImpact(ctx, owner, files[0].Ref, "DELETE")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "vfs-delete", ExpectedVersion: &files[0].Version}, Payload: command.ArtifactLifecycleInput{ArtifactRef: files[0].Ref, ImpactDigest: impact.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	nodes, total, _, err = service.ListVFSNodes(ctx, owner, query.Filter{ProjectRef: project, ResourceRef: root + "/files", State: "DELETED"})
	if err != nil || total != 1 || len(nodes) != 1 || nodes[0].EntityRef != files[0].Ref || !nodes[0].Selectable || nodes[0].LifecycleState != "DELETED" || !slices.Equal(nodes[0].NextActions, []string{"RESTORE", "PURGE"}) {
		t.Fatalf("VFS trash eligibility: total=%d err=%v", total, err)
	}
}
