package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestArtifactOptionalAntivirus exercises explicit opt-in, truthful receipt encoding, exact byte
// binding, cancellation and required-only paths without a daemon or external credentials.
// TestArtifactOptionalAntivirus проверяет явный opt-in, честный receipt, точные байты, отмену и
// обязательные AV пути без daemon либо внешних credentials.
func TestArtifactOptionalAntivirus(t *testing.T) {
	hash := sha256.Sum256([]byte("abc"))
	digest := "sha256:" + hex.EncodeToString(hash[:])
	r := &Repository{}
	if _, err := r.scanArtifact(t.Context(), strings.NewReader("abc"), digest, 3); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("required mode did not fail closed: %v", err)
	}
	if err := r.ConfigureArtifactAntivirusMode("disabled"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureArtifactAntivirusMode("auto"); err == nil || !r.artifactAntivirusDisabled {
		t.Fatal("unknown mode mutated policy")
	}
	reader := strings.NewReader("abc")
	report, err := r.scanArtifact(t.Context(), reader, digest, 3)
	if err != nil || !report.NotScanned || report.Infected || !report.ScannedAt.IsZero() || report.CheckedAt.IsZero() {
		t.Fatalf("explicit no-AV report: %+v %v", report, err)
	}
	if position, _ := reader.Seek(0, io.SeekCurrent); position != 0 {
		t.Fatal("reader was not rewound")
	}
	raw, err := artifactAntivirusJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if json.Unmarshal(raw, &receipt) != nil || receipt["verdict"] != "NOT_SCANNED" || receipt["scannedAt"] != nil || receipt["checkedAt"] == nil {
		t.Fatal("no-AV receipt invented scan attestation")
	}
	decoded, err := decodeArtifactAntivirus(raw, digest, 3)
	if err != nil || !decoded.NotScanned || artifactAdmissionState("CLEAN", decoded) != "NOT_SCANNED" || artifactAdmissionState("QUARANTINED", decoded) != "QUARANTINED" {
		t.Fatalf("receipt roundtrip or format quarantine: %v", err)
	}
	receipt["verdict"] = "CLEAN"
	forged, _ := json.Marshal(receipt)
	if _, err := decodeArtifactAntivirus(forged, digest, 3); err == nil {
		t.Fatal("no-AV proof promoted to CLEAN")
	}
	if _, err := r.scanArtifact(t.Context(), strings.NewReader("abcd"), digest, 3); err == nil {
		t.Fatal("oversized body admitted")
	}
	if _, err := r.scanArtifact(t.Context(), strings.NewReader("xyz"), digest, 3); err == nil {
		t.Fatal("different body admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.scanArtifact(ctx, reader, digest, 3); err == nil {
		t.Fatal("cancelled admission succeeded")
	}
	if _, err := r.scanRequiredArtifact(t.Context(), reader, digest, 3); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("executable/AV-required path used ordinary-file exception")
	}
	if err := r.ConfigureArtifactAntivirusMode("required"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.scanArtifact(t.Context(), reader, digest, 3); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("return to required did not close new admissions")
	}
}

// TestArtifactOptionalRuntimeComponent completes an actual owner execution with a result file,
// then launches another execution with that immutable file as input. No scanner is configured;
// capture, replay and lease-bound read must retain NOT_SCANNED and exact bytes.
// TestArtifactOptionalRuntimeComponent завершает настоящее owner execution с файлом результата,
// затем запускает другое execution с этим immutable файлом. Scanner отсутствует; capture, replay
// и lease-bound чтение обязаны сохранить NOT_SCANNED и точные байты.
func TestArtifactOptionalRuntimeComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository, service, owner, worker, err := newRunComponentFixture(t, ctx, pool, true)
	if err != nil {
		t.Fatal(err)
	}
	repository.artifactScanner = nil
	if err := repository.ConfigureArtifactAntivirusMode("disabled"); err != nil {
		t.Fatal(err)
	}
	reportReadyUsingWorkerFixture(t, ctx, service, owner, worker)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "optional-artifact-project"},
		Payload:  command.ProjectInput{Name: "Ordinary artifact fixture", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatal("create ordinary artifact project", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "optional-artifact-agent", "File author")
	_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "optional-artifact-capability", ExpectedVersion: &agent.Version},
		Payload:  command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}})
	if err != nil {
		t.Fatal("grant artifact capability", err)
	}
	launch := func(key, attachmentSetRef, targetRef string) entity.Run {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref,
				Target: entity.RunTarget{Type: "AGENT", Ref: targetRef}, Task: "Read and write an ordinary text file", AttachmentSetRef: attachmentSetRef}})
		if err != nil || result.Run == nil {
			t.Fatal("launch ordinary artifact execution", err)
		}
		return *result.Run
	}
	first := launch("optional-artifact-first", "", agent.Ref)
	lease := claimComponentExecution(t, ctx, service, worker, first.Ref, "optional-artifact-first")
	completed := completeClaimedExecution(t, ctx, service, worker, lease, "optional-artifact-first", true)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" || len(completed.Run.ArtifactRefs) != 1 {
		t.Fatal("ordinary artifact completion did not succeed")
	}
	ref := completed.Run.ArtifactRefs[0]
	artifact, err := service.GetArtifact(ctx, owner, ref)
	if err != nil || artifact.ScanState != "NOT_SCANNED" {
		t.Fatal("captured file lost no-AV status", err)
	}
	replay := completeClaimedExecution(t, ctx, service, worker, lease, "optional-artifact-first", true)
	if replay.Run == nil || len(replay.Run.ArtifactRefs) != 1 || replay.Run.ArtifactRefs[0] != ref {
		t.Fatal("completion replay created another result")
	}
	set := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref, "RUN_INPUT", "optional-artifact-input", ref)
	consumer := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "optional-artifact-consumer", "File reader")
	_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "optional-artifact-consumer-capability", ExpectedVersion: &consumer.Version},
		Payload:  command.AgentBindingInput{AgentRef: consumer.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}})
	if err != nil {
		t.Fatal("grant consumer file capability", err)
	}
	second := launch("optional-artifact-second", set, consumer.Ref)
	secondLease := claimComponentExecution(t, ctx, service, worker, second.Ref, "optional-artifact-second")
	reader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller",
		Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	download, err := service.ReadExecutionArtifact(ctx, reader, stringMap(secondLease, "leaseRef"), stringMap(secondLease, "fence"), runtimeRevisionMapInt64(secondLease, "generation"), ref)
	if err != nil {
		t.Fatal("materialize explicit no-AV input", err)
	}
	body, readErr := io.ReadAll(download.Reader)
	download.Reader.Close()
	if readErr != nil || string(body) != "Customer response is ready.\n" {
		t.Fatal("materialized content differs from captured bytes")
	}
	completeClaimedExecution(t, ctx, service, worker, secondLease, "optional-artifact-second", false)
	if stale, err := service.ReadExecutionArtifact(ctx, reader, stringMap(secondLease, "leaseRef"), stringMap(secondLease, "fence"), runtimeRevisionMapInt64(secondLease, "generation"), ref); err == nil {
		stale.Reader.Close()
		t.Fatal("terminal lease still materialized content")
	}
}

// testArtifactOptionalAntivirus checks actual owner upload, truthful projection, exact download
// and idempotent replay in the existing disposable fixture, restoring required mode on return.
// testArtifactOptionalAntivirus проверяет настоящий owner upload, честную проекцию, точный download
// и idempotent replay в существующей disposable fixture, затем возвращает required mode.
func testArtifactOptionalAntivirus(t *testing.T, ctx context.Context, repository *Repository, owner value.Principal, service *platformservice.Service) {
	t.Helper()
	if err := repository.ConfigureArtifactAntivirusMode("disabled"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.ConfigureArtifactAntivirusMode("required") }()
	input := func() platformrepo.ArtifactUpload {
		return platformrepo.ArtifactUpload{FileName: "ordinary-unscanned.txt", MediaType: "text/plain", SizeBytes: 3, Reader: strings.NewReader("abc")}
	}
	mutation := value.Mutation{IdempotencyKey: "ordinary-explicit-no-av"}
	item, err := service.UploadArtifact(ctx, owner, mutation, input())
	if err != nil || item.ScanState != "NOT_SCANNED" {
		t.Fatalf("ordinary opt-in upload: %s %v", item.ScanState, err)
	}
	read, err := service.GetArtifact(ctx, owner, item.Ref)
	if err != nil || read.ScanState != "NOT_SCANNED" || !contains(read.NextActions, "DOWNLOAD") {
		t.Fatalf("ordinary opt-in projection: %+v %v", read, err)
	}
	download, err := service.DownloadArtifact(ctx, owner, item.Ref, "DOWNLOAD")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(download.Reader)
	download.Reader.Close()
	if readErr != nil || string(body) != "abc" {
		t.Fatal("ordinary opt-in download changed body")
	}
	replay, err := service.UploadArtifact(ctx, owner, mutation, input())
	if err != nil || replay.Ref != item.Ref || replay.CurrentRevisionRef != item.CurrentRevisionRef || replay.ScanState != "NOT_SCANNED" {
		t.Fatalf("ordinary opt-in replay: %+v %v", replay, err)
	}
}
