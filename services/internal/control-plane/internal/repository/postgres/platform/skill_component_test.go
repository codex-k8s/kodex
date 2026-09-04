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
	invoke := func(kind command.Kind, key string, version *int64, payload command.SkillBundleInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	created, err := invoke(command.CreateSkillBundleDraft, "skill-create", nil, command.SkillBundleInput{ProjectRef: project.Project.Ref, Specification: spec})
	if err != nil || created.SkillBundle == nil {
		t.Fatalf("create draft: %v", err)
	}
	bundle := created.SkillBundle
	if bundle.CurrentRevision != nil || bundle.DraftRevision == nil || bundle.DraftRevision.ScanState != "PENDING" {
		t.Fatal("draft acquired implicit publication or scan")
	}
	if file := bundle.DraftRevision.Files[0]; file.Digest != artifact.Digest || file.SizeBytes != artifact.SizeBytes {
		t.Fatal("client assigned file provenance")
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
