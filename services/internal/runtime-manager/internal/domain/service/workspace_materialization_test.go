package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func TestStartWorkspaceMaterializationPersistsSourcesAndMarksSlotMaterializing(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	projectID := mustUUID("00000000-0000-0000-0000-000000000031")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000301"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}

	materialization, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000302"), 0),
	})
	if err != nil {
		t.Fatalf("StartWorkspaceMaterialization(): %v", err)
	}
	if materialization.Status != enum.WorkspaceMaterializationStatusRunning {
		t.Fatalf("Status = %s, want running", materialization.Status)
	}
	if len(materialization.Sources) != 2 || materialization.Sources[0].AccessMode != enum.WorkspaceSourceAccessModeWrite {
		t.Fatalf("Sources = %#v, want normalized writable/read-only sources", materialization.Sources)
	}
	updatedSlot := repo.slots[slot.ID]
	if updatedSlot.Status != enum.SlotStatusMaterializing || updatedSlot.Fingerprint != "workspace-policy-sha" {
		t.Fatalf("slot after materialization = %#v, want materializing with policy digest", updatedSlot)
	}
	if updatedSlot.ActiveWorkspaceMaterializationID == nil || *updatedSlot.ActiveWorkspaceMaterializationID != materialization.ID {
		t.Fatalf("active materialization = %v, want %s", updatedSlot.ActiveWorkspaceMaterializationID, materialization.ID)
	}
	if len(repo.events) < 2 || repo.events[len(repo.events)-1].EventType != eventWorkspaceStarted {
		t.Fatalf("last event = %#v, want workspace started", repo.events)
	}
}

func TestStartWorkspaceMaterializationRejectsCrossProjectPolicy(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000037")
	otherProjectID := mustUUID("00000000-0000-0000-0000-000000000038")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000312"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}

	_, err = svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(otherProjectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000313"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("StartWorkspaceMaterialization() err = %v, want conflict", err)
	}
}

func TestReportWorkspaceMaterializationCompletedMarksSlotReady(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	projectID := mustUUID("00000000-0000-0000-0000-000000000032")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000303"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	materialization, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000304"), 0),
	})
	if err != nil {
		t.Fatalf("StartWorkspaceMaterialization(): %v", err)
	}

	completed, err := svc.ReportWorkspaceMaterializationProgress(context.Background(), ReportWorkspaceMaterializationProgressInput{
		WorkspaceMaterializationID: materialization.ID,
		Status:                     enum.WorkspaceMaterializationStatusCompleted,
		Fingerprint:                "materialized-fingerprint",
		Meta:                       commandMeta(mustUUID("00000000-0000-0000-0000-000000000305"), materialization.Version),
	})
	if err != nil {
		t.Fatalf("ReportWorkspaceMaterializationProgress(): %v", err)
	}
	if completed.Status != enum.WorkspaceMaterializationStatusCompleted || completed.FinishedAt == nil {
		t.Fatalf("completed materialization = %#v, want completed with finished_at", completed)
	}
	updatedSlot := repo.slots[slot.ID]
	if updatedSlot.Status != enum.SlotStatusReady || updatedSlot.Fingerprint != "materialized-fingerprint" {
		t.Fatalf("slot after complete = %#v, want ready with materialization fingerprint", updatedSlot)
	}
	if updatedSlot.ActiveWorkspaceMaterializationID != nil {
		t.Fatalf("active materialization = %v, want nil after terminal progress", updatedSlot.ActiveWorkspaceMaterializationID)
	}
	if repo.events[len(repo.events)-1].EventType != eventWorkspaceCompleted {
		t.Fatalf("last event = %s, want workspace completed", repo.events[len(repo.events)-1].EventType)
	}
}

func TestReportWorkspaceMaterializationFailureIsManagedRuntimeState(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	projectID := mustUUID("00000000-0000-0000-0000-000000000033")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000306"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	materialization, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000307"), 0),
	})
	if err != nil {
		t.Fatalf("StartWorkspaceMaterialization(): %v", err)
	}

	failed, err := svc.ReportWorkspaceMaterializationProgress(context.Background(), ReportWorkspaceMaterializationProgressInput{
		WorkspaceMaterializationID: materialization.ID,
		Status:                     enum.WorkspaceMaterializationStatusFailed,
		ErrorCode:                  "RUNTIME_WORKSPACE_SOURCE_UNAVAILABLE",
		ErrorMessage:               "repository token is unavailable",
		Meta:                       commandMeta(mustUUID("00000000-0000-0000-0000-000000000308"), materialization.Version),
	})
	if err != nil {
		t.Fatalf("ReportWorkspaceMaterializationProgress(): %v", err)
	}
	if failed.LastErrorCode != "RUNTIME_WORKSPACE_SOURCE_UNAVAILABLE" {
		t.Fatalf("failure code = %s, want source unavailable", failed.LastErrorCode)
	}
	updatedSlot := repo.slots[slot.ID]
	if updatedSlot.Status != enum.SlotStatusFailed || updatedSlot.LastErrorCode != failed.LastErrorCode {
		t.Fatalf("slot after failure = %#v, want failed with managed error", updatedSlot)
	}
	if updatedSlot.ActiveWorkspaceMaterializationID != nil {
		t.Fatalf("active materialization = %v, want nil after failure", updatedSlot.ActiveWorkspaceMaterializationID)
	}
}

func TestReportWorkspaceMaterializationRejectsInactiveAttempt(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	projectID := mustUUID("00000000-0000-0000-0000-000000000039")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000314"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	materialization, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000315"), 0),
	})
	if err != nil {
		t.Fatalf("StartWorkspaceMaterialization(): %v", err)
	}
	newAttemptID := mustUUID("00000000-0000-0000-0000-000000000040")
	staleGuardSlot := repo.slots[slot.ID]
	staleGuardSlot.ActiveWorkspaceMaterializationID = &newAttemptID
	staleGuardSlot.Version++
	repo.slots[slot.ID] = staleGuardSlot

	_, err = svc.ReportWorkspaceMaterializationProgress(context.Background(), ReportWorkspaceMaterializationProgressInput{
		WorkspaceMaterializationID: materialization.ID,
		Status:                     enum.WorkspaceMaterializationStatusCompleted,
		Fingerprint:                "stale-fingerprint",
		Meta:                       commandMeta(mustUUID("00000000-0000-0000-0000-000000000316"), materialization.Version),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("ReportWorkspaceMaterializationProgress() err = %v, want conflict", err)
	}
	if repo.slots[slot.ID].Fingerprint == "stale-fingerprint" {
		t.Fatalf("slot fingerprint was overwritten by stale materialization")
	}
}

func TestWorkspaceMaterializationReplayChecksScopeAndAuthorizesBeforeReplay(t *testing.T) {
	t.Parallel()

	authorizer := &recordAuthorizer{}
	svc, _ := newTestServiceWithAuthorizer(authorizer)
	projectID := mustUUID("00000000-0000-0000-0000-000000000034")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-before",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000309"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000310"), 0)
	first, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            meta,
	})
	if err != nil {
		t.Fatalf("StartWorkspaceMaterialization(): %v", err)
	}
	authorizer.requests = nil
	replay, err := svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            meta,
	})
	if err != nil {
		t.Fatalf("replay StartWorkspaceMaterialization(): %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, first.ID)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != actionWorkspaceStart {
		t.Fatalf("authorization requests = %#v, want workspace start before replay", authorizer.requests)
	}
	otherPolicy := testWorkspacePolicy(projectID)
	otherPolicy.PolicyDigest = "other-policy-sha"
	_, err = svc.StartWorkspaceMaterialization(context.Background(), StartWorkspaceMaterializationInput{
		SlotID:          slot.ID,
		WorkspacePolicy: otherPolicy,
		Meta:            meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("cross-policy replay err = %v, want conflict", err)
	}
}

func TestPrepareRuntimeCreatesSlotAndWorkspaceAttempt(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000035")
	result, err := svc.PrepareRuntime(context.Background(), PrepareRuntimeInput{
		AgentRunID:      uuidPtr(mustUUID("00000000-0000-0000-0000-000000000036")),
		RuntimeProfile:  "go-backend",
		RuntimeMode:     enum.RuntimeModeFullEnv,
		WorkspacePolicy: testWorkspacePolicy(projectID),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000311"), 0),
	})
	if err != nil {
		t.Fatalf("PrepareRuntime(): %v", err)
	}
	if result.Slot.Status != enum.SlotStatusMaterializing {
		t.Fatalf("slot status = %s, want materializing", result.Slot.Status)
	}
	if result.WorkspaceMaterialization.SlotID != result.Slot.ID {
		t.Fatalf("materialization slot id = %s, want %s", result.WorkspaceMaterialization.SlotID, result.Slot.ID)
	}
	if result.Slot.ActiveWorkspaceMaterializationID == nil || *result.Slot.ActiveWorkspaceMaterializationID != result.WorkspaceMaterialization.ID {
		t.Fatalf("active materialization = %v, want %s", result.Slot.ActiveWorkspaceMaterializationID, result.WorkspaceMaterialization.ID)
	}
	if result.RuntimeContext.WorkspaceRoot != "/workspace" {
		t.Fatalf("workspace root = %s, want /workspace", result.RuntimeContext.WorkspaceRoot)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("placement resolver calls = %d, want 1", len(resolver.requests))
	}
	if resolver.requests[0].ProjectID == nil || *resolver.requests[0].ProjectID != projectID {
		t.Fatalf("placement project = %v, want %s", resolver.requests[0].ProjectID, projectID)
	}
}

func testWorkspacePolicy(projectID uuid.UUID) WorkspacePolicyInput {
	repositoryID := mustUUID("00000000-0000-0000-0000-000000000041")
	return WorkspacePolicyInput{
		ProjectID:     projectID,
		PolicyDigest:  "workspace-policy-sha",
		PolicyVersion: 7,
		Sources: []value.WorkspaceSource{
			{
				SourceID:      "repo-api",
				Kind:          enum.WorkspaceSourceKindCode,
				RepositoryID:  &repositoryID,
				Provider:      "github",
				ProviderOwner: "codex-k8s",
				ProviderName:  "example-api",
				SourceRef:     "main",
				CommitSHA:     "abc123",
				LocalPath:     "src/example-api",
				AccessMode:    enum.WorkspaceSourceAccessModeWrite,
				Metadata:      []byte(`{"role":"primary"}`),
			},
			{
				SourceID:   "guidance-go",
				Kind:       enum.WorkspaceSourceKindGuidancePackage,
				SourceRef:  "v1.0.0",
				LocalPath:  "guidance/go",
				AccessMode: enum.WorkspaceSourceAccessModeRead,
				Digest:     "sha256:guidance",
				Metadata:   []byte(`{}`),
			},
		},
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	return &id
}
