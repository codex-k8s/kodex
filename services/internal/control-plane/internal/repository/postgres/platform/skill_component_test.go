package platform

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testSkillBundleDraft(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.skill-bundle-drafts.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "skill-project"}, Payload: command.ProjectInput{Name: "Skill lifecycle", Purpose: "Skill owner test", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: Documentation\ndescription: Read approved documentation\n---\nFollow the referenced design documents.\n")
	artifact, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "skill-upload"}, platformrepo.ArtifactUpload{ProjectRef: project.Project.Ref, FileName: "SKILL.md", MediaType: "text/markdown", SizeBytes: int64(len(body)), Reader: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	spec := entity.SkillBundleSpecification{Name: "Documentation", Description: "Read approved documentation", Files: []entity.SkillBundleFile{{Path: "SKILL.md", ArtifactRef: artifact.Ref, ArtifactRevision: artifact.Revision, Digest: "untrusted-client-value", SizeBytes: 1}}}
	for _, path := range []string{"references/first.md", "references/second.md"} {
		file, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "skill-support-" + path}, platformrepo.ArtifactUpload{ProjectRef: project.Project.Ref, FileName: "support.md", MediaType: "text/markdown", SizeBytes: int64(len(body)), Reader: bytes.NewReader(body)})
		if err != nil {
			t.Fatal(err)
		}
		spec.Files = append(spec.Files, entity.SkillBundleFile{Path: path, ArtifactRef: file.Ref, ArtifactRevision: file.Revision})
	}
	invoke := func(kind command.Kind, key string, version *int64, payload command.SkillBundleInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	created, err := invoke(command.CreateSkillBundleDraft, "skill-create", nil, command.SkillBundleInput{ProjectRef: project.Project.Ref, Specification: spec})
	if err != nil || created.SkillBundle == nil {
		t.Fatalf("create draft: %v", err)
	}
	bundle := created.SkillBundle
	testContextVFS(t, ctx, service, owner, project.Project.Ref, bundle.Ref, "SKILL", bundle.DraftRevision.Digest, true)
	rootPath := "/projects/" + project.Project.Ref + "/skills/" + bundle.Ref
	nodes, total, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ResourceRef: rootPath})
	if err != nil || total != 2 || len(nodes) != 2 {
		t.Fatalf("skill files and distinct supporting directory: total=%d err=%v", total, err)
	}
	for _, node := range nodes {
		if node.Name == "SKILL.md" && (node.EntityRef != artifact.Ref || node.Selectable || node.SelectionReason != "IMMUTABLE_CONTEXT" || node.Revision != artifact.Revision) {
			t.Fatal("skill manifest file lost exact read-only source")
		}
	}
	nodes, total, _, err = service.ListVFSNodes(ctx, owner, query.Filter{ResourceRef: rootPath + "/references"})
	if err != nil || total != 2 || len(nodes) != 2 || nodes[0].Ref == nodes[1].Ref {
		t.Fatalf("supporting files exact page: total=%d err=%v", total, err)
	}
	if bundle.CurrentRevision != nil || bundle.DraftRevision == nil || bundle.DraftRevision.ScanState != "PENDING" {
		t.Fatal("draft acquired implicit publication or scan")
	}
	if file := bundle.DraftRevision.Files[0]; file.Digest != artifact.Digest || file.SizeBytes != artifact.SizeBytes {
		t.Fatal("client assigned file provenance")
	}
	impact, err := service.GetArtifactImpact(ctx, owner, artifact.Ref, "DELETE")
	if err != nil || impact.Permitted || !containsString(impact.Blockers, "ARTIFACT_USED_BY_SKILL") {
		t.Fatalf("skill artifact retention impact: permitted=%t blockers=%v err=%v", impact.Permitted, impact.Blockers, err)
	}
	files, fileTotal, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, ResourceRef: "/projects/" + project.Project.Ref + "/files", Query: "SKILL.md"})
	if err != nil || fileTotal != 1 || len(files) != 1 || files[0].EntityRef != artifact.Ref || files[0].Selectable || files[0].SelectionReason != "ARTIFACT_USED_BY_SKILL" || containsString(files[0].NextActions, "DELETE") {
		t.Fatalf("VFS offered deletion of retained skill source: total=%d err=%v", fileTotal, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "skill-retained-artifact-delete", ExpectedVersion: &artifact.Version},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: artifact.Ref, ImpactDigest: impact.Digest}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("skill artifact deletion did not fail closed: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.artifacts SET lifecycle_state='DELETED',version=version+1 WHERE ref=$1`, artifact.Ref); err == nil {
		t.Fatal("direct lifecycle update bypassed skill artifact retention")
	}
	if _, err := invoke(command.CreateSkillBundleDraft, "skill-double-draft", &bundle.Version, command.SkillBundleInput{ProjectRef: project.Project.Ref, BundleRef: bundle.Ref, Specification: spec}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("second draft: %v", err)
	}
	spec.Description = "Updated documentation description"
	updated, err := invoke(command.SaveSkillBundleDraft, "skill-save", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, Specification: spec})
	if err != nil || updated.SkillBundle == nil {
		t.Fatalf("save draft: %v", err)
	}
	if updated.SkillBundle.Version != bundle.Version+1 || updated.SkillBundle.DraftRevision.Digest == bundle.DraftRevision.Digest || updated.SkillBundle.DraftRevision.ScanState != "PENDING" {
		t.Fatal("save did not update digest/version")
	}
	if _, err := invoke(command.SaveSkillBundleDraft, "skill-stale", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, Specification: spec}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale save: %v", err)
	}
	readback, err := service.GetSkillBundle(ctx, owner, bundle.Ref)
	if err != nil || readback.DraftRevision == nil || readback.DraftRevision.Digest != updated.SkillBundle.DraftRevision.Digest {
		t.Fatalf("readback: %v", err)
	}
	bundle = &readback
	spec.Description = "Read approved documentation"
	saved, err := invoke(command.SaveSkillBundleDraft, "skill-align-manifest", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	bundle = saved.SkillBundle
	validate := func(key string) command.Result {
		result, err := invoke(command.ValidateSkillBundleDraft, key, &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, ExpectedDigest: bundle.DraftRevision.Digest})
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		return result
	}
	invalid := validate("skill-no-scanner")
	if invalid.SkillBundle.DraftRevision.State != "INVALID" || invalid.SkillBundle.DraftRevision.ScanState != "ERROR" {
		t.Fatal("missing malware scanner failed open")
	}
	bundle = invalid.SkillBundle
	previousScanner := repository.skillScanner
	defer func() { repository.skillScanner = previousScanner }()
	if err := repository.ConfigureSkillScanner(componentSkillScanner{}); err != nil {
		t.Fatal(err)
	}
	valid := validate("skill-scanner-valid")
	if valid.SkillBundle.DraftRevision.State != "VALIDATED" || valid.SkillBundle.DraftRevision.ScanState != "CLEAN" {
		t.Fatalf("validated fixture: %#v", valid.SkillBundle.DraftRevision)
	}
	bundle = valid.SkillBundle
	if _, err := invoke(command.PublishSkillBundleDraft, "skill-unreviewed-publish", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, ExpectedDigest: bundle.DraftRevision.Digest}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("unreviewed publication: %v", err)
	}
	approved, err := invoke(command.ReviewSkillBundleDraft, "skill-review", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, ExpectedDigest: bundle.DraftRevision.Digest, Decision: "APPROVE"})
	if err != nil {
		t.Fatal(err)
	}
	bundle = approved.SkillBundle
	published, err := invoke(command.PublishSkillBundleDraft, "skill-publish", &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref, RevisionRef: bundle.DraftRevision.Ref, ExpectedDigest: bundle.DraftRevision.Digest})
	if err != nil || published.SkillBundle.CurrentRevision == nil || published.SkillBundle.DraftRevision != nil {
		t.Fatalf("publication: %v", err)
	}
	bundle = published.SkillBundle
	testContextVFS(t, ctx, service, owner, project.Project.Ref, bundle.Ref, "SKILL", bundle.CurrentRevision.Digest, true)
	reader := contextProjectReader(t, ctx, repository, service, owner, project.Project.Ref, "SKILL")
	testContextVFS(t, ctx, service, reader, project.Project.Ref, bundle.Ref, "SKILL", bundle.CurrentRevision.Digest, false)
	testContextBinding(t, ctx, repository, service, owner, project.Project.Ref, bundle.Ref, bundle.CurrentRevision.Ref, "skill-context", false)
	listed, total, _, err := service.ListSkillBundles(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 1}})
	if err != nil || total != 1 || len(listed) != 1 || listed[0].Ref != bundle.Ref {
		t.Fatalf("skill catalog: %d %d %v", total, len(listed), err)
	}
	history, total, _, err := service.ListSkillBundleRevisions(ctx, owner, bundle.Ref, query.Page{Size: 1})
	if err != nil || total != 1 || len(history) != 1 || history[0].Ref != bundle.CurrentRevision.Ref {
		t.Fatalf("skill history: %d %d %v", total, len(history), err)
	}
	for _, step := range []struct {
		kind command.Kind
		key  string
	}{{command.ArchiveSkillBundle, "skill-archive"}, {command.RestoreSkillBundle, "skill-restore"}, {command.ArchiveSkillBundle, "skill-rearchive"}, {command.PurgeSkillBundle, "skill-purge"}} {
		result, err := invoke(step.kind, step.key, &bundle.Version, command.SkillBundleInput{BundleRef: bundle.Ref})
		if err != nil {
			t.Fatalf("%s: %v", step.key, err)
		}
		bundle = result.SkillBundle
		testContextVFS(t, ctx, service, owner, project.Project.Ref, bundle.Ref, "SKILL", bundle.CurrentRevision.Digest, bundle.State == "ACTIVE")
		impact, err := service.GetArtifactImpact(ctx, owner, artifact.Ref, "DELETE")
		if err != nil || impact.Permitted != (bundle.State == "PURGED") {
			t.Fatalf("skill history retention %s: permitted=%t err=%v", bundle.State, impact.Permitted, err)
		}
	}
	if bundle.State != "PURGED" || len(bundle.CurrentRevision.Files) != 0 {
		t.Fatal("purged skill files remain visible")
	}
	history, total, _, err = service.ListSkillBundleRevisions(ctx, owner, bundle.Ref, query.Page{Size: 1})
	if err != nil || total != 1 || len(history) != 1 || len(history[0].Files) != 0 {
		t.Fatalf("purged skill history: %v", err)
	}
}

type componentSkillScanner struct{}

func (componentSkillScanner) Scan(context.Context, []byte) (skillpolicy.ScanVerdict, error) {
	return skillpolicy.ScanVerdict{Engine: "component-scanner-fixed-revision"}, nil
}
