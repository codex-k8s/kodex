package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func TestReserveSlotReusesMatchingPrewarmedSlot(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	slotID := mustUUID("00000000-0000-0000-0000-000000000701")
	repo.slots[slotID] = entity.Slot{
		Base:           entity.Base{ID: slotID, Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour)},
		SlotKey:        "slot-prewarmed",
		Status:         enum.SlotStatusPrewarmed,
		RuntimeMode:    enum.RuntimeModeCodeOnly,
		IsPrewarmed:    true,
		FleetScopeID:   &testFleetScopeID,
		ClusterID:      &testClusterID,
		NamespaceName:  "kodex-rt-prewarmed",
		RuntimeProfile: "go-backend",
	}
	projectID := mustUUID("00000000-0000-0000-0000-000000000702")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000703"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if slot.ID != slotID || slot.Status != enum.SlotStatusReserved || slot.Fingerprint != "policy-sha" {
		t.Fatalf("reused slot = %#v, want reserved prewarmed slot with fingerprint", slot)
	}
	if len(repo.slots) != 1 {
		t.Fatalf("slots = %d, want no extra slot creation", len(repo.slots))
	}
}

func TestReserveSlotDoesNotReuseMismatchedReadySlot(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	slotID := mustUUID("00000000-0000-0000-0000-000000000704")
	repo.slots[slotID] = entity.Slot{
		Base:           entity.Base{ID: slotID, Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour)},
		SlotKey:        "slot-ready",
		Status:         enum.SlotStatusReady,
		RuntimeMode:    enum.RuntimeModeCodeOnly,
		FleetScopeID:   &testFleetScopeID,
		ClusterID:      &testClusterID,
		NamespaceName:  "kodex-rt-ready",
		RuntimeProfile: "go-backend",
		Fingerprint:    "old-policy-sha",
	}
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "new-policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000705"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if slot.ID == slotID {
		t.Fatalf("slot id = %s, want a new slot because fingerprint mismatched", slot.ID)
	}
}

func TestCleanupBatchCleansExpiredSlotsAndReportsBlockedSlots(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	policy, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopePlatform,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		KeepShortLogTail: false,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000706"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy(): %v", err)
	}
	cleanSlotID := mustUUID("00000000-0000-0000-0000-000000000707")
	blockedSlotID := mustUUID("00000000-0000-0000-0000-000000000708")
	repo.slots[cleanSlotID] = cleanupSlot(cleanSlotID)
	repo.slots[blockedSlotID] = cleanupSlot(blockedSlotID)
	repo.jobs[mustUUID("00000000-0000-0000-0000-000000000709")] = entity.Job{
		Base:    entity.Base{ID: mustUUID("00000000-0000-0000-0000-000000000709"), Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		JobType: enum.JobTypeCleanup,
		Status:  enum.JobStatusRunning,
		SlotID:  &blockedSlotID,
	}
	result, err := svc.RunCleanupBatch(context.Background(), RunCleanupBatchInput{
		CleanupPolicyID: &policy.ID,
		Limit:           10,
		LeaseOwner:      "worker/runtime-cleanup",
		LeaseUntil:      testNow.Add(10 * time.Minute),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000710"), 0),
	})
	if err != nil {
		t.Fatalf("RunCleanupBatch(): %v", err)
	}
	if result.CleanedCount != 1 || result.FailedCount != 1 || repo.slots[cleanSlotID].Status != enum.SlotStatusCleaned {
		t.Fatalf("cleanup result = %#v clean slot = %#v, want one clean and one failed", result, repo.slots[cleanSlotID])
	}
	if repo.slots[blockedSlotID].LastErrorCode != "CLEANUP_BLOCKED_BY_ACTIVE_JOB" {
		t.Fatalf("blocked slot = %#v, want visible cleanup failure", repo.slots[blockedSlotID])
	}
}

func TestPrewarmPoolReconcileCreatesSlotsAndReserveReusesOne(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	pool, err := svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		ScopeType:      enum.PrewarmPoolScopePlatform,
		RuntimeProfile: "go-backend",
		TargetSize:     2,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           commandMeta(mustUUID("00000000-0000-0000-0000-000000000711"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdatePrewarmPool(): %v", err)
	}
	reconciled, err := svc.ReconcilePrewarmPool(context.Background(), ReconcilePrewarmPoolInput{
		PrewarmPoolID: pool.ID,
		LeaseOwner:    "worker/runtime-prewarm",
		LeaseUntil:    testNow.Add(10 * time.Minute),
		Meta:          commandMeta(mustUUID("00000000-0000-0000-0000-000000000712"), 0),
	})
	if err != nil {
		t.Fatalf("ReconcilePrewarmPool(): %v", err)
	}
	if reconciled.LastCapacityStatus != enum.CapacityStatusOK || len(repo.slots) != 2 {
		t.Fatalf("reconciled pool = %#v slots = %d, want ok with two prewarmed slots", reconciled, len(repo.slots))
	}
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000713"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if !slot.IsPrewarmed || slot.Status != enum.SlotStatusReserved {
		t.Fatalf("reserved slot = %#v, want reused prewarmed slot", slot)
	}
}

func TestOrganizationPrewarmPoolIsVisibleAsInsufficientUntilOrganizationProjectionExists(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	pool, err := svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		ScopeType:      enum.PrewarmPoolScopeOrganization,
		ScopeID:        "org-1",
		RuntimeProfile: "go-backend",
		TargetSize:     1,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           commandMeta(mustUUID("00000000-0000-0000-0000-000000000714"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdatePrewarmPool(): %v", err)
	}
	reconciled, err := svc.ReconcilePrewarmPool(context.Background(), ReconcilePrewarmPoolInput{
		PrewarmPoolID: pool.ID,
		LeaseOwner:    "worker/runtime-prewarm",
		LeaseUntil:    testNow.Add(10 * time.Minute),
		Meta:          commandMeta(mustUUID("00000000-0000-0000-0000-000000000715"), 0),
	})
	if err != nil {
		t.Fatalf("ReconcilePrewarmPool(): %v", err)
	}
	if reconciled.LastCapacityStatus != enum.CapacityStatusInsufficient {
		t.Fatalf("capacity = %s, want insufficient", reconciled.LastCapacityStatus)
	}
}

func TestCleanupPolicyUpdateRequiresExpectedVersion(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	policy, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopePlatform,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000716"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy(): %v", err)
	}
	_, err = svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		CleanupPolicyID:  &policy.ID,
		ScopeType:        enum.RuntimeScopePlatform,
		TTLSeconds:       120,
		FailedTTLSeconds: 120,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000717"), 0),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("update error = %v, want invalid argument without expected version", err)
	}
}

func cleanupSlot(id uuid.UUID) entity.Slot {
	return entity.Slot{
		Base:           entity.Base{ID: id, Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour)},
		SlotKey:        "slot-" + id.String()[24:],
		Status:         enum.SlotStatusCleanupPending,
		RuntimeMode:    enum.RuntimeModeCodeOnly,
		FleetScopeID:   &testFleetScopeID,
		ClusterID:      &testClusterID,
		NamespaceName:  "kodex-rt-cleanup",
		RuntimeProfile: "go-backend",
	}
}
