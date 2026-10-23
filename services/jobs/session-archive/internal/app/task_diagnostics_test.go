package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes/fake"
)

// TestArchiveRPCObservationClosedCodesAndStages checks every canonical gRPC code and specialized stage without leaking wrapped errors.
// RU: TestArchiveRPCObservationClosedCodesAndStages проверяет каждый канонический gRPC-код и специализированный этап без утечки обёрнутых ошибок.
func TestArchiveRPCObservationClosedCodesAndStages(t *testing.T) {
	task := model.Task{TaskRef: "sat_public_fixture", SessionRef: "ses_public_fixture", Kind: "SNAPSHOT", ContentGeneration: 3, Attempt: 4}
	for _, stage := range []archiveRPCStage{archiveRPCRenew, archiveRPCFail, archiveRPCCompleteSnapshot,
		archiveRPCCompleteRestore, archiveRPCCompletePVCDeletion, archiveRPCCompleteObjectDeletion} {
		for code := codes.OK; code <= codes.Unauthenticated; code++ {
			t.Run(string(stage)+"/"+code.String(), func(t *testing.T) {
				var output bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&output, nil))
				var err error
				if code != codes.OK {
					err = fmt.Errorf("SENTINEL_WRAPPER: %w", status.Error(code, "SENTINEL_RPC_SECRET"))
				}
				observeArchiveRPC(t.Context(), logger, task, stage, err)
				var record map[string]any
				if err := json.Unmarshal(output.Bytes(), &record); err != nil {
					t.Fatal(err)
				}
				if record["msg"] != archiveRPCObservationMessage || record[archiveRPCCodeAttribute] != code.String() ||
					record[archiveRPCStageAttribute] != string(stage) || record[archiveTaskRefAttribute] != task.TaskRef ||
					record[archiveSessionRefAttribute] != task.SessionRef || record[archiveTaskKindAttribute] != task.Kind ||
					record[archiveContentGenerationAttribute] != float64(3) || record[archiveAttemptAttribute] != float64(4) {
					t.Fatalf("unexpected closed RPC observation: %#v", record)
				}
				if strings.Contains(output.String(), "SENTINEL") || len(record) != 10 {
					t.Fatal("RPC observation exposed error text or undeclared metadata")
				}
			})
		}
	}
}

// archiveRPCFailureFixture accepts renewal but rejects the expected owner failure command for diagnostic assertions.
// RU: archiveRPCFailureFixture допускает продление, но отклоняет ожидаемую owner-команду отказа для проверки диагностики.
type archiveRPCFailureFixture struct {
	controlplanev1.SessionArchiveWorkServiceClient
	called bool
}

// RenewSessionArchiveTask keeps synthetic authority valid so the fixture reaches its terminal failure path.
// RU: RenewSessionArchiveTask сохраняет синтетические полномочия действующими, чтобы оснастка дошла до конечного отказа.
func (client *archiveRPCFailureFixture) RenewSessionArchiveTask(_ context.Context, _ *controlplanev1.RenewSessionArchiveTaskRequest, _ ...grpc.CallOption) (*controlplanev1.RenewSessionArchiveTaskResponse, error) {
	return &controlplanev1.RenewSessionArchiveTaskResponse{}, nil
}

// FailSessionArchiveTask records the expected missing-PVC code and returns a private PermissionDenied error.
// RU: FailSessionArchiveTask фиксирует ожидаемый код отсутствия PVC и возвращает закрытую ошибку PermissionDenied.
func (client *archiveRPCFailureFixture) FailSessionArchiveTask(_ context.Context, request *controlplanev1.FailSessionArchiveTaskRequest, _ ...grpc.CallOption) (*controlplanev1.FailSessionArchiveTaskResponse, error) {
	client.called = request.GetSafeErrorCode() == model.SafeErrorPVCMissing
	return nil, status.Error(codes.PermissionDenied, "SENTINEL_PRIVATE_RPC_ERROR")
}

// TestArchiveProcessObservesOwnerFailureWithoutErrorText keeps the owner failure outcome and excludes private claim and error data from logs.
// RU: TestArchiveProcessObservesOwnerFailureWithoutErrorText сохраняет исход owner-отказа и исключает закрытые данные claim и ошибки из журнала.
func TestArchiveProcessObservesOwnerFailureWithoutErrorText(t *testing.T) {
	kube, err := controller.New(fake.NewClientset(), controller.Config{WorkerNamespace: "kodex-runtime", Environment: "local",
		WorkerImage: "registry.invalid/archive@sha256:" + strings.Repeat("a", 64), WorkerServiceAccount: "synthetic-worker",
		ObjectStorageSecret: "synthetic-secret", SessionPVCSize: "1Gi", ObjectStorageEndpoint: "https://storage.invalid",
		ObjectStorageRegion: "synthetic", ObjectStorageBucket: "synthetic", WorkerTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	const sessionRef = "ses_public_fixture"
	pvc, err := runtimecontract.SessionPVCName(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	claim := &controlplanev1.SessionArchiveTask{TaskRef: "sat_public_fixture", SessionRef: sessionRef, OrganizationRef: "org_public_fixture",
		Kind: controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT, ProviderAccountRef: "pacc_fixture",
		RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64),
		CodexSessionId: "00000000-0000-4000-8000-000000000003", ContentGeneration: 3, PvcName: pvc, InputDigest: strings.Repeat("b", 64),
		SourceRelativePath: ".kodex/state/codex-home/sessions/2026/10/04/rollout-2026-10-04T00-00-00-00000000-0000-4000-8000-000000000003.jsonl",
		SourceSha256:       strings.Repeat("c", 64), SourceSizeBytes: 128, TargetObjectKey: "SENTINEL_PRIVATE_OBJECT", Attempt: 4,
		Lease: &controlplanev1.WorkLease{Ref: "SENTINEL_LEASE", Fence: "SENTINEL_FENCE", Generation: 4}}
	client := &archiveRPCFailureFixture{}
	var output bytes.Buffer
	err = process(t.Context(), &controlplaneclient.Client{SessionArchive: client}, kube, claim, newArchiveMetrics(),
		slog.New(slog.NewJSONHandler(&output, nil)), Config{WorkerTimeout: time.Minute, RPCDeadline: time.Second})
	if !client.called || status.Code(err) != codes.PermissionDenied {
		t.Fatal("process changed owner RPC failure or skipped canonical fail command")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record[archiveRPCStageAttribute] != string(archiveRPCFail) || record[archiveRPCCodeAttribute] != "PermissionDenied" ||
		record[archiveTaskRefAttribute] != claim.GetTaskRef() || record[archiveSessionRefAttribute] != sessionRef || strings.Contains(output.String(), "SENTINEL") {
		t.Fatal("process owner failure diagnostic is missing or leaked private data")
	}
}

// TestArchiveRPCObservationRejectsUnboundedDiagnostics normalizes forged identifiers, stages and codes to bounded fallbacks.
// RU: TestArchiveRPCObservationRejectsUnboundedDiagnostics нормализует поддельные идентификаторы, этапы и коды в ограниченные запасные значения.
func TestArchiveRPCObservationRejectsUnboundedDiagnostics(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	task := model.Task{TaskRef: "https://SENTINEL_SECRET", SessionRef: strings.Repeat("s", 129), Kind: "SENTINEL_KIND", ContentGeneration: -1, Attempt: 6,
		OrganizationRef: "SENTINEL_ORG", SourceRelativePath: "SENTINEL_SOURCE", TargetObjectKey: "SENTINEL_OBJECT", InputDigest: "SENTINEL_DIGEST"}
	observeArchiveRPC(t.Context(), logger, task, archiveRPCStage("SENTINEL_STAGE"), status.Error(codes.Code(999), "SENTINEL_ERROR"))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{archiveTaskRefAttribute, archiveSessionRefAttribute, archiveTaskKindAttribute, archiveRPCStageAttribute, archiveRPCCodeAttribute} {
		if record[key] != archiveDiagnosticUnknown {
			t.Fatalf("unbounded %s was exposed", key)
		}
	}
	if record[archiveAttemptAttribute] != float64(0) || record[archiveContentGenerationAttribute] != float64(0) ||
		strings.Contains(output.String(), "SENTINEL") || len(record) != 10 {
		t.Fatal("RPC observation exposed untrusted metadata")
	}
	if archiveDiagnosticRPCCode(errors.New("SENTINEL_ERROR")) != "Unknown" {
		t.Fatal("non-RPC error did not remain unknown")
	}
	observeArchiveRPC(t.Context(), nil, task, archiveRPCFail, nil)
}
