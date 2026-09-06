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

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/runtime_files_mutate_frozen.sql
var fixtureRuntimeFilesMutateFrozen string

//go:embed testdata/sql/runtime_files_diagnose_capture.sql
var fixtureRuntimeFilesDiagnoseCapture string

func diagnoseRuntimeFiles(t *testing.T, ctx context.Context, repository *Repository, ref string) {
	t.Helper()
	var total, entries int64
	var permissions, sourceStates []string
	if err := repository.pool.QueryRow(ctx, fixtureRuntimeFilesDiagnoseCapture, ref).Scan(&total, &entries, &permissions, &sourceStates); err != nil {
		t.Logf("catalog diagnostic failed: %v", err)
		return
	}
	t.Logf("catalog counts total=%d entries=%d permissions=%v states=%v", total, entries, permissions, sourceStates)
}

func runtimeFilesTestPrincipal(t *testing.T, ctx context.Context, repository *Repository, operation string) value.Principal {
	t.Helper()
	return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.files." + operation,
	}, "runtime-controller")
}

func runtimeFilesTestContext(t *testing.T, lease map[string]any, purpose string) query.ExecutionFileContext {
	t.Helper()
	catalog, ok := lease["fileCatalog"].(runtimecontract.RuntimeFileCatalog)
	if !ok || catalog.Validate() != nil {
		t.Fatal("runtime claim omitted exact file catalog descriptor")
	}
	return query.ExecutionFileContext{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"),
		CatalogRef: catalog.Ref, CatalogDigest: catalog.Digest, Purpose: purpose}
}

func testRuntimeFileQueries(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, lease map[string]any) {
	t.Helper()
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeWorkspaceInput)
	searcher := runtimeFilesTestPrincipal(t, ctx, repository, "search")
	reader := runtimeFilesTestPrincipal(t, ctx, repository, "metadata")
	previewer := runtimeFilesTestPrincipal(t, ctx, repository, "preview")
	manifestReader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	first, err := service.SearchExecutionFiles(ctx, searcher, execution, "", query.Page{Size: 1})
	if err != nil || first.Total != 2 || len(first.Items) != 1 || first.Next == "" {
		diagnoseRuntimeFiles(t, ctx, repository, execution.CatalogRef)
		t.Fatalf("search exact input catalog: count=%d total=%d err=%v", len(first.Items), first.Total, err)
	}
	second, err := service.SearchExecutionFiles(ctx, searcher, execution, "", query.Page{Size: 1, Token: first.Next})
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Next != "" || second.Items[0].EntryRef == first.Items[0].EntryRef {
		t.Fatalf("search second input page: count=%d total=%d err=%v", len(second.Items), second.Total, err)
	}
	if _, err := service.SearchExecutionFiles(ctx, searcher, execution, "different query", query.Page{Size: 1, Token: first.Next}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("query-substituted cursor accepted: %v", err)
	}
	empty, err := service.SearchExecutionFiles(ctx, searcher, execution, "%", query.Page{Size: 1})
	if err != nil || empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("literal wildcard changed search scope: count=%d err=%v", len(empty.Items), err)
	}
	manifest, err := service.GetExecutionFileManifest(ctx, manifestReader, execution, query.Page{Size: 100})
	if err != nil || manifest.Total != first.Total || len(manifest.Items) != 2 || manifest.Catalog.Digest != first.Catalog.Digest {
		t.Fatalf("manifest differs from authorized search: count=%d err=%v", len(manifest.Items), err)
	}
	file := first.Items[0]
	exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
	metadata, err := service.GetExecutionFileMetadata(ctx, reader, execution, exact)
	if err != nil || metadata.File != file || metadata.Catalog.Digest != execution.CatalogDigest {
		t.Fatalf("exact metadata mismatch: %v", err)
	}
	preview, err := service.PreviewExecutionFile(ctx, previewer, execution, exact, 16384)
	if err != nil || preview.Truncated || int64(len(preview.Text)) != file.SizeBytes {
		t.Fatalf("bounded text preview failed: %v", err)
	}
	hash := sha256.Sum256([]byte(preview.Text))
	if preview.Digest != "sha256:"+hex.EncodeToString(hash[:]) || preview.Digest != file.Digest {
		t.Fatal("preview content commitment differs")
	}
	prefix, err := service.PreviewExecutionFile(ctx, previewer, execution, exact, 3)
	if err != nil || !prefix.Truncated || len(prefix.Text) > 3 {
		t.Fatalf("bounded prefix failed: %v", err)
	}
	for _, alter := range []func(*query.ExecutionFileContext){
		func(context *query.ExecutionFileContext) { context.Fence = "wrong-fence" },
		func(context *query.ExecutionFileContext) { context.Generation++ },
		func(context *query.ExecutionFileContext) { context.CatalogDigest = strings.Repeat("0", 64) },
		func(context *query.ExecutionFileContext) { context.CatalogRef = "vfc_0000000000000000" },
	} {
		wrong := execution
		alter(&wrong)
		if _, err := service.GetExecutionFileMetadata(ctx, reader, wrong, exact); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("substituted execution pin accepted: %v", err)
		}
	}
	wrongFile := exact
	wrongFile.Revision++
	if _, err := service.GetExecutionFileMetadata(ctx, reader, execution, wrongFile); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("substituted file revision accepted: %v", err)
	}
	if _, err := service.GetExecutionFileMetadata(ctx, previewer, execution, exact); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("wrong exact permission accepted: %v", err)
	}
	if _, err := service.PreviewExecutionFile(ctx, previewer, execution, exact, 16385); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("oversize preview accepted: %v", err)
	}
	// Временная попытка изменения должна отклоняться реальным immutable trigger.
	if _, err := repository.pool.Exec(ctx, fixtureRuntimeFilesMutateFrozen, execution.CatalogRef); err == nil {
		t.Fatal("frozen runtime catalog accepted mutation")
	}
	activity := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.tool-call.record",
	}, "runtime-controller")
	projection := command.RunToolCallInput{LeaseRef: execution.LeaseRef, Fence: execution.Fence, Generation: execution.Generation,
		CallRef: "tcl_filefixture1", Tool: runtimecontract.FileToolSearch, GrantRef: execution.CatalogRef,
		SafeParameters: map[string]any{"purpose": execution.Purpose}, State: "SUCCEEDED", DurationMS: 1, SafeResult: "completed"}
	recorded, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: activity,
		Mutation: value.Mutation{IdempotencyKey: "runtime-files-activity-1"}, Payload: projection})
	if err != nil || recorded.Event == nil || recorded.Event.ToolCall == nil || recorded.Event.ToolCall.CapabilityRef != "" || recorded.Event.ToolCall.GrantRef != execution.CatalogRef {
		t.Fatalf("read-only catalog tool activity failed: %v", err)
	}
	projection.SafeParameters = map[string]any{"purpose": execution.Purpose, "query": "private query must not be retained"}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: activity,
		Mutation: value.Mutation{IdempotencyKey: "runtime-files-activity-sensitive"}, Payload: projection}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("file query was allowed into activity projection: %v", err)
	}
	projection.SafeParameters = map[string]any{"purpose": execution.Purpose}
	projection.CapabilityRef = runtimecontract.ArtifactCapability
	if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: activity,
		Mutation: value.Mutation{IdempotencyKey: "runtime-files-activity-write-capability"}, Payload: projection}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("read-only catalog was treated as write capability: %v", err)
	}
	testRuntimeProjectFiles(t, ctx, repository, service, owner, lease)
}

func testRuntimeProjectFiles(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, lease map[string]any) {
	t.Helper()
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeProject)
	searcher := runtimeFilesTestPrincipal(t, ctx, repository, "search")
	reader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read",
	}, "runtime-controller")
	page, err := service.SearchExecutionFiles(ctx, searcher, execution, "same.txt", query.Page{Size: 10})
	if err != nil || len(page.Items) != 1 || page.Total != 1 {
		t.Fatalf("project file snapshot is unavailable: count=%d err=%v", len(page.Items), err)
	}
	file := page.Items[0]
	download, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef)
	if err != nil {
		t.Fatalf("catalog project file body grant failed: %v", err)
	}
	body, readErr := io.ReadAll(download.Reader)
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "alpha" || download.Artifact.Digest != file.Digest {
		t.Fatal("project file body pin differs")
	}
	late, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "runtime-files-after-capture"}, platformrepo.ArtifactUpload{
		ProjectRef: stringMap(lease, "projectRef"), FileName: "after-runtime-catalog.txt", MediaType: "text/plain", SizeBytes: 4, Reader: strings.NewReader("late"),
	})
	if err != nil {
		t.Fatalf("create file after catalog capture: %v", err)
	}
	page, err = service.SearchExecutionFiles(ctx, searcher, execution, late.FileName, query.Page{Size: 10})
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("old runtime catalog included a new file: count=%d err=%v", len(page.Items), err)
	}
	if _, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, late.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("new file bypassed pinned body grant: %v", err)
	}
	impact, err := service.GetArtifactImpact(ctx, owner, file.ArtifactRef, "DELETE")
	if err != nil || !impact.Permitted {
		t.Fatalf("unbound project file delete impact: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-files-delete-visible", ExpectedVersion: &file.Version},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: file.ArtifactRef, ImpactDigest: impact.Digest}}); err != nil {
		t.Fatalf("delete common project file: %v", err)
	}
	page, err = service.SearchExecutionFiles(ctx, searcher, execution, "same.txt", query.Page{Size: 10})
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("deleted file remained in runtime search: count=%d err=%v", len(page.Items), err)
	}
	if _, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("deleted project file retained body grant: %v", err)
	}
}

func testRuntimeSkillFileManifest(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, lease map[string]any, available bool) {
	t.Helper()
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeSkill)
	reader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	result, err := service.GetExecutionFileManifest(ctx, reader, execution, query.Page{Size: 100})
	if err != nil {
		t.Fatalf("read Skill file manifest: %v", err)
	}
	if available && (result.Total < 1 || len(result.Items) < 1) || !available && (result.Total != 0 || len(result.Items) != 0) {
		diagnoseRuntimeFiles(t, ctx, repository, execution.CatalogRef)
		t.Fatalf("Skill binding eligibility differs: count=%d total=%d available=%t", len(result.Items), result.Total, available)
	}
}
