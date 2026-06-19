package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	runtimekubernetes "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/kubernetes"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func TestBuildContextMaterializerWorkerReportsReadyContext(t *testing.T) {
	t.Parallel()

	buildContext := testRuntimeBuildContext()
	sourceRef, sourceDigest := runtimekubernetes.BuildContextSourceSnapshot(buildContext)
	service := &fakeBuildContextLifecycle{current: buildContext}
	materializer := fakeBuildContextMaterializer{result: runtimekubernetes.BuildContextMaterializationResult{
		Succeeded:             true,
		SourceSnapshotRef:     sourceRef,
		SourceSnapshotDigest:  sourceDigest,
		BuildContextRef:       "pvc://runtime-jobs/kodex-bctx-000000000000000000000000",
		BuildContextDigest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestBundleDigests: map[string]string{"runtime-manager": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}}
	worker := buildContextMaterializerWorker{
		service:      service,
		materializer: materializer,
		cfg:          RuntimeKubernetesWorkerConfig{PollInterval: time.Second},
	}

	result := worker.materialize(context.Background(), buildContext)

	if result != kubernetesWorkerProcessed {
		t.Fatalf("materialize result = %v, want processed", result)
	}
	if len(service.reports) != 2 ||
		service.reports[0].Status != enum.BuildContextStatusRunning ||
		service.reports[1].Status != enum.BuildContextStatusReady {
		t.Fatalf("reports = %#v, want running then ready", service.reports)
	}
	if service.reports[0].SourceSnapshotRef != sourceRef || service.reports[0].SourceSnapshotDigest != sourceDigest {
		t.Fatalf("running source snapshot = %q/%q, want deterministic snapshot", service.reports[0].SourceSnapshotRef, service.reports[0].SourceSnapshotDigest)
	}
	if service.reports[1].BuildContextRef == "" ||
		service.reports[1].BuildContextDigest == "" ||
		service.reports[1].ManifestBundleDigests["runtime-manager"] == "" {
		t.Fatalf("ready report = %#v, want checked context and manifest bundle refs", service.reports[1])
	}
	if service.reports[0].Meta.Actor.ID != buildContextMaterializerActor ||
		service.reports[1].Meta.Actor.ID != buildContextMaterializerActor {
		t.Fatalf("report actors = %q/%q, want materializer actor", service.reports[0].Meta.Actor.ID, service.reports[1].Meta.Actor.ID)
	}
}

func TestBuildContextMaterializerWorkerReportsTypedFailure(t *testing.T) {
	t.Parallel()

	buildContext := testRuntimeBuildContext()
	service := &fakeBuildContextLifecycle{current: buildContext}
	materializer := fakeBuildContextMaterializer{result: runtimekubernetes.BuildContextMaterializationResult{
		ErrorCode:    "build_context_materializer_report_invalid",
		ErrorMessage: "build context materializer report is invalid",
	}}
	worker := buildContextMaterializerWorker{
		service:      service,
		materializer: materializer,
		cfg:          RuntimeKubernetesWorkerConfig{PollInterval: time.Second},
	}

	result := worker.materialize(context.Background(), buildContext)

	if result != kubernetesWorkerProcessed {
		t.Fatalf("materialize failure result = %v, want processed", result)
	}
	if len(service.reports) != 2 ||
		service.reports[0].Status != enum.BuildContextStatusRunning ||
		service.reports[1].Status != enum.BuildContextStatusFailed {
		t.Fatalf("reports = %#v, want running then failed", service.reports)
	}
	if service.reports[1].ErrorCode != "build_context_materializer_report_invalid" ||
		service.reports[1].NextAction != "review_build_context_materialization" {
		t.Fatalf("failure report = %#v, want typed safe diagnostic", service.reports[1])
	}
}

func testRuntimeBuildContext() entity.BuildContext {
	return entity.BuildContext{
		Base: entity.Base{
			ID:        uuid.MustParse("00000000-0000-0000-0000-000000001085"),
			Version:   3,
			CreatedAt: time.Unix(1, 0).UTC(),
			UpdatedAt: time.Unix(1, 0).UTC(),
		},
		Status:               enum.BuildContextStatusPending,
		ProjectID:            uuid.MustParse("00000000-0000-0000-0000-000000001001"),
		RepositoryID:         uuid.MustParse("00000000-0000-0000-0000-000000001002"),
		Provider:             "github",
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex",
		SourceRef:            "refs/heads/main",
		SourceCommitSHA:      "0123456789abcdef0123456789abcdef01234567",
		AffectedServiceKeys:  []string{"runtime-manager"},
		BuildPlanFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ContextFingerprint:   "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
}

type fakeBuildContextLifecycle struct {
	current entity.BuildContext
	reports []runtimeservice.ReportBuildContextProgressInput
}

func (s *fakeBuildContextLifecycle) ReportBuildContextProgress(_ context.Context, input runtimeservice.ReportBuildContextProgressInput) (entity.BuildContext, error) {
	s.reports = append(s.reports, input)
	s.current.Version++
	s.current.Status = input.Status
	if input.SourceSnapshotRef != "" {
		s.current.SourceSnapshotRef = input.SourceSnapshotRef
		s.current.SourceSnapshotDigest = input.SourceSnapshotDigest
	}
	if input.BuildContextRef != "" {
		s.current.BuildContextRef = input.BuildContextRef
		s.current.BuildContextDigest = input.BuildContextDigest
	}
	if input.ManifestBundleDigests != nil {
		s.current.ManifestBundleDigests = input.ManifestBundleDigests
	}
	s.current.LastErrorCode = input.ErrorCode
	s.current.LastErrorMessage = input.ErrorMessage
	s.current.NextAction = input.NextAction
	return s.current, nil
}

type fakeBuildContextMaterializer struct {
	result runtimekubernetes.BuildContextMaterializationResult
}

func (m fakeBuildContextMaterializer) Materialize(context.Context, entity.BuildContext) runtimekubernetes.BuildContextMaterializationResult {
	return m.result
}
