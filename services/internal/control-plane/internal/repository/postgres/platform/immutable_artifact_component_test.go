package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed testdata/sql/immutable_artifact_next_revision.sql
	immutableNextRevision string
	//go:embed testdata/sql/immutable_artifact_next_content.sql
	immutableNextContent string
	//go:embed testdata/sql/immutable_artifact_advance.sql
	immutableAdvance string
	//go:embed testdata/sql/immutable_artifact_pinned_read.sql
	immutablePinnedRead string
	//go:embed testdata/sql/immutable_artifact_graph.sql
	immutableGraph string
	//go:embed testdata/sql/immutable_artifact_terminal_readback.sql
	immutableTerminalReadback string
	//go:embed testdata/sql/immutable_artifact_wrong_update.sql
	immutableWrongUpdate string
	//go:embed testdata/sql/immutable_artifact_wrong_delete.sql
	immutableWrongDelete string
	//go:embed testdata/sql/immutable_artifact_frozen_readback.sql
	immutableFrozenReadback string
)

// Проверяет настоящие PG guards и exact versioned bytes в безопасном Store.
// Runtime/attachment snapshot не обновляется после смены current pointer.
func TestImmutableArtifactRevisionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects := objectstoragetest.New()
	repository, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	var nodes, edges int
	var nodeHash, edgeHash string
	if err := pool.QueryRow(ctx, immutableGraph).Scan(&nodes, &nodeHash, &edges, &edgeHash); err != nil {
		t.Fatal(err)
	}
	t.Logf("purge graph nodes=%d/%s edges=%d/%s", nodes, nodeHash, edges, edgeHash)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.projects.create"}, "control-api-gateway")
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-project"}, Payload: command.ProjectInput{Name: "Immutable body fixture", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("project: %v", err)
	}
	oldBody := "---\nname: Documentation\ndescription: Read approved documentation\n---\nRead the original immutable documentation.\n"
	old, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "immutable-upload"}, platformrepo.ArtifactUpload{ProjectRef: project.Project.Ref, FileName: "SKILL.md", MediaType: "text/markdown", SizeBytes: int64(len(oldBody)), Reader: strings.NewReader(oldBody)})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	attachment := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref, "RUN_INPUT", "immutable-input", old.Ref)
	oldDownload, err := service.DownloadArtifact(ctx, owner, old.Ref, "DOWNLOAD")
	if err != nil {
		t.Fatalf("download before advance: %v", err)
	}
	oldDownload.Reader.Close()
	if err := repository.ConfigureSkillScanner(componentSkillScanner{}); err != nil {
		t.Fatal(err)
	}
	createdSkill, err := service.Execute(ctx, command.Command{Kind: command.CreateSkillBundleDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-skill-create"}, Payload: command.SkillBundleInput{ProjectRef: project.Project.Ref, Specification: entity.SkillBundleSpecification{Name: "Documentation", Description: "Read approved documentation", Files: []entity.SkillBundleFile{{Path: "SKILL.md", ArtifactRef: old.Ref, ArtifactRevision: old.Revision}}}}})
	if err != nil || createdSkill.SkillBundle == nil {
		t.Fatalf("create pinned skill: %v", err)
	}
	skill := createdSkill.SkillBundle
	for _, stage := range []struct {
		kind command.Kind
		key  string
	}{{command.ValidateSkillBundleDraft, "validate"}, {command.ReviewSkillBundleDraft, "review"}, {command.PublishSkillBundleDraft, "publish"}} {
		result, err := service.Execute(ctx, command.Command{Kind: stage.kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-skill-" + stage.key, ExpectedVersion: &skill.Version}, Payload: command.SkillBundleInput{BundleRef: skill.Ref, RevisionRef: skill.DraftRevision.Ref, ExpectedDigest: skill.DraftRevision.Digest, Decision: "APPROVE"}})
		if err != nil || result.SkillBundle == nil {
			t.Fatalf("publish pinned skill %s: %v", stage.key, err)
		}
		skill = result.SkillBundle
	}
	prepareObservedWarmFixture(t, ctx, repository)
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "immutable-agent", "Immutable reader")
	capability, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-capability", ExpectedVersion: &agent.Version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}})
	if err != nil || capability.Agent == nil {
		t.Fatalf("capability: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeArtifactBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-binding", ExpectedVersion: &old.Version}, Payload: command.ArtifactBindingInput{ArtifactRef: old.Ref, AgentRef: agent.Ref, Enabled: true}}); err != nil {
		t.Fatalf("binding: %v", err)
	}
	contextAgent, err := service.GetAgent(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.BindAgentSkillBundle, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-skill-bind", ExpectedVersion: &contextAgent.Version}, Payload: command.AgentContextBindingInput{AgentRef: agent.Ref, ResourceRef: skill.Ref, RevisionRef: skill.CurrentRevision.Ref}}); err != nil {
		t.Fatalf("bind pinned skill: %v", err)
	}
	launch, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-launch"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Pinned read", Task: "Read immutable input", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, AttachmentSetRef: attachment}})
	if err != nil || launch.Run == nil {
		t.Fatalf("launch: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "immutable-claim"}, Payload: command.LeaseInput{WorkloadInstance: "immutable-fixture", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim: %v", err)
	}
	lease := claimed.RuntimeItems[0]
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeWorkspaceInput)
	manifestReader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	manifest, err := service.GetExecutionFileManifest(ctx, manifestReader, execution, query.Page{Size: 100})
	if err != nil || len(manifest.Items) != 1 {
		t.Fatalf("manifest: %v", err)
	}
	file := manifest.Items[0]
	var snapshotBefore, catalogBefore, entriesBefore string
	if err := pool.QueryRow(ctx, immutableFrozenReadback, stringMap(lease, "leaseRef")).Scan(&snapshotBefore, &catalogBefore, &entriesBefore); err != nil {
		t.Fatal(err)
	}
	newBody := "completely different new body"
	sum := sha256.Sum256([]byte(newBody))
	newDigest := "sha256:" + hex.EncodeToString(sum[:])
	newObject, err := objects.Put(ctx, objectstorage.PutInput{Key: "disposable/immutable/new", MediaType: "text/plain", Digest: newDigest, SizeBytes: int64(len(newBody)), Body: strings.NewReader(newBody)})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var nextID string
	if err := tx.QueryRow(ctx, immutableNextRevision, old.Ref, int64(len(newBody)), newDigest).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, immutableNextContent, nextID, newObject.Key, newObject.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, immutableAdvance, old.Ref, nextID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var snapshotAfter, catalogAfter, entriesAfter string
	if err := pool.QueryRow(ctx, immutableFrozenReadback, stringMap(lease, "leaseRef")).Scan(&snapshotAfter, &catalogAfter, &entriesAfter); err != nil {
		t.Fatal(err)
	}
	if snapshotBefore != snapshotAfter || catalogBefore != catalogAfter || entriesBefore != entriesAfter {
		t.Fatal("head advance rewrote frozen runtime pins")
	}
	for _, path := range []string{"/projects/" + project.Project.Ref + "/runs/" + launch.Run.Ref + "/workspace/inputs", "/projects/" + project.Project.Ref + "/agents/" + agent.Ref + "/workspace/inputs"} {
		nodes, total, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, ResourceRef: path})
		if err != nil || total != 1 || len(nodes) != 1 || nodes[0].Digest != old.Digest || nodes[0].Revision != old.Revision || nodes[0].RevisionRef != old.CurrentRevisionRef {
			t.Fatalf("historical VFS pin: total=%d err=%v", total, err)
		}
	}
	exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
	metadata, err := service.GetExecutionFileMetadata(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "metadata"), execution, exact)
	if err != nil || metadata.File != file {
		t.Fatalf("old catalog metadata after advance: %v", err)
	}
	preview, err := service.PreviewExecutionFile(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "preview"), execution, exact, 16384)
	if err != nil || preview.Text != oldBody {
		t.Fatalf("old catalog bytes after advance: %v", err)
	}
	skillExecution := execution
	skillExecution.Purpose = runtimecontract.FilePurposeSkill
	skillManifest, err := service.GetExecutionFileManifest(ctx, manifestReader, skillExecution, query.Page{Size: 100})
	if err != nil || len(skillManifest.Items) != 1 || skillManifest.Items[0].Revision != old.Revision || skillManifest.Items[0].Digest != old.Digest {
		t.Fatalf("old skill catalog pin: %v", err)
	}
	skillFile := skillManifest.Items[0]
	skillPreview, err := service.PreviewExecutionFile(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "preview"), skillExecution, query.ExecutionFileRef{EntryRef: skillFile.EntryRef, ArtifactRef: skillFile.ArtifactRef, Revision: skillFile.Revision, Digest: skillFile.Digest}, 16384)
	if err != nil || skillPreview.Text != oldBody {
		t.Fatalf("old skill bytes: %v", err)
	}
	wrongExact := exact
	wrongExact.Revision++
	if _, err := service.GetExecutionFileMetadata(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "metadata"), execution, wrongExact); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("wrong catalog revision accepted: %v", err)
	}
	runtimeReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	runtimeDownload, err := service.ReadExecutionArtifact(ctx, runtimeReader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), runtimeRevisionMapInt64(lease, "generation"), old.Ref)
	if err != nil {
		t.Fatalf("old runtime body: %v", err)
	}
	runtimeRaw, err := io.ReadAll(runtimeDownload.Reader)
	runtimeDownload.Reader.Close()
	if err != nil || string(runtimeRaw) != oldBody || runtimeDownload.Artifact.Version != old.Version {
		t.Fatal("runtime read changed old bytes or ArtifactVersion")
	}
	activeImpact, err := service.GetArtifactImpact(ctx, owner, old.Ref, "PURGE")
	if err != nil || activeImpact.ActiveRuntimeCount < 1 || activeImpact.Permitted {
		t.Fatalf("historical runtime pin not retained: %v", err)
	}
	var key, version, name, digest string
	var frozenVersion int64
	if err := pool.QueryRow(ctx, immutablePinnedRead, attachment, old.Revision, old.Digest).Scan(&key, &version, &name, &digest, &frozenVersion); err != nil {
		t.Fatalf("historical pin after advance: %v", err)
	}
	body, err := objects.Get(ctx, key, version)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(body.Body)
	body.Body.Close()
	if err != nil || string(raw) != oldBody || name != old.FileName || digest != old.Digest || frozenVersion != old.Version {
		t.Fatal("historical body or frozen metadata changed")
	}
	for _, wrong := range []struct {
		revision int64
		digest   string
	}{{old.Revision + 1, old.Digest}, {old.Revision, newDigest}} {
		if err := pool.QueryRow(ctx, immutablePinnedRead, attachment, wrong.revision, wrong.digest).Scan(&key, &version, &name, &digest, &frozenVersion); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("substituted pin did not fail closed")
		}
	}
	for _, statement := range []string{immutableWrongUpdate, immutableWrongDelete} {
		if _, err := pool.Exec(ctx, statement, old.Ref); err == nil {
			t.Fatal("immutable metadata mutation accepted")
		}
	}
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "immutable-complete"}, Payload: command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Synthetic immutable read complete"}})
	if err != nil || completed.Run == nil || completed.Run.State != "SUCCEEDED" {
		t.Fatalf("complete: %v", err)
	}
	current, err := service.GetArtifact(ctx, owner, old.Ref)
	if err != nil {
		t.Fatal(err)
	}
	skillImpact, err := service.GetArtifactImpact(ctx, owner, old.Ref, "DELETE")
	if err != nil || skillImpact.Permitted || !contains(skillImpact.Blockers, "ARTIFACT_USED_BY_SKILL") {
		t.Fatalf("historical skill stopped retaining old body: %v", err)
	}
	for _, stage := range []struct {
		kind command.Kind
		key  string
	}{{command.ArchiveSkillBundle, "archive"}, {command.PurgeSkillBundle, "purge"}} {
		result, err := service.Execute(ctx, command.Command{Kind: stage.kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-skill-" + stage.key, ExpectedVersion: &skill.Version}, Payload: command.SkillBundleInput{BundleRef: skill.Ref}})
		if err != nil || result.SkillBundle == nil {
			t.Fatalf("close retained skill %s: %v", stage.key, err)
		}
		skill = result.SkillBundle
	}
	impact, err := service.GetArtifactImpact(ctx, owner, old.Ref, "DELETE")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-delete", ExpectedVersion: &current.Version}, Payload: command.ArtifactLifecycleInput{ArtifactRef: old.Ref, ImpactDigest: impact.Digest}})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	_ = deleted
	current, err = service.GetArtifact(ctx, owner, old.Ref)
	if err != nil {
		t.Fatal(err)
	}
	impact, err = service.GetArtifactImpact(ctx, owner, old.Ref, "PURGE")
	if err != nil {
		t.Fatal(err)
	}
	if impact.Permitted || impact.BindingCount != 1 {
		t.Fatal("active authoritative binding did not block purge")
	}
	activeAgent, err := service.GetAgent(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ArchiveAgent, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "immutable-archive", ExpectedVersion: &activeAgent.Version}, Payload: command.AgentInput{Ref: agent.Ref}}); err != nil {
		t.Fatalf("archive exact binding target: %v", err)
	}
	impact, err = service.GetArtifactImpact(ctx, owner, old.Ref, "PURGE")
	if err != nil || !impact.Permitted || impact.BindingCount != 1 {
		t.Fatalf("historical binding remained blocker: %v", err)
	}
	mutation := value.Mutation{IdempotencyKey: "immutable-purge", ExpectedVersion: &current.Version}
	if state, err := service.PurgeArtifact(ctx, owner, mutation, old.Ref, impact.Digest); err != nil || state != "PURGED" {
		t.Fatalf("all revision purge: state=%s err=%v", state, err)
	}
	if _, err := objects.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("old exact object survived purge")
	}
	if _, err := objects.Head(ctx, newObject.Key, newObject.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("new exact object survived purge")
	}
	var state, terminalName, terminalDigest string
	var pointerNull bool
	var revisions, grants, items, bindings int64
	if err := pool.QueryRow(ctx, immutableTerminalReadback, old.Ref, old.CurrentRevisionRef).Scan(&state, &pointerNull, &revisions, &grants, &items, &bindings, &terminalName, &terminalDigest); err != nil {
		t.Fatal(err)
	}
	if state != "PURGED" || !pointerNull || revisions != 0 || grants != 1 || items != 2 || bindings != 1 || terminalName == old.FileName || terminalDigest == old.Digest {
		t.Fatalf("terminal minimal/lineage: state=%s null=%v revisions=%d grants=%d items=%d", state, pointerNull, revisions, grants, items)
	}
	if _, err := service.DownloadArtifactRevision(ctx, owner, old.Ref, old.CurrentRevisionRef, "DOWNLOAD"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("purged historical grant reopened: %v", err)
	}
	if state, err := service.PurgeArtifact(ctx, owner, mutation, old.Ref, impact.Digest); err != nil || state != "PURGED" {
		t.Fatalf("closed terminal retry: %s/%v", state, err)
	}
}
