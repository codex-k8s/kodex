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

func TestReserveSlotDoesNotReusePrewarmedSlotFromAnotherProjectScope(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	ownerProjectID := mustUUID("00000000-0000-0000-0000-000000000718")
	requestProjectID := mustUUID("00000000-0000-0000-0000-000000000719")
	slotID := mustUUID("00000000-0000-0000-0000-000000000720")
	repo.slots[slotID] = entity.Slot{
		Base:           entity.Base{ID: slotID, Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour)},
		SlotKey:        "slot-project-prewarmed",
		Status:         enum.SlotStatusPrewarmed,
		RuntimeMode:    enum.RuntimeModeCodeOnly,
		IsPrewarmed:    true,
		FleetScopeID:   &testFleetScopeID,
		ProjectID:      &ownerProjectID,
		NamespaceName:  "kodex-rt-project",
		RuntimeProfile: "go-backend",
	}
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &requestProjectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000721"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if slot.ID == slotID {
		t.Fatalf("slot id = %s, want a new slot because project scope differs", slot.ID)
	}
}

func TestReserveSlotDoesNotReusePrewarmedSlotFromAnotherRepositoryScope(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	ownerRepositoryID := mustUUID("00000000-0000-0000-0000-000000000722")
	requestRepositoryID := mustUUID("00000000-0000-0000-0000-000000000723")
	slotID := mustUUID("00000000-0000-0000-0000-000000000724")
	repo.slots[slotID] = entity.Slot{
		Base:           entity.Base{ID: slotID, Version: 1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour)},
		SlotKey:        "slot-repository-prewarmed",
		Status:         enum.SlotStatusPrewarmed,
		RuntimeMode:    enum.RuntimeModeCodeOnly,
		IsPrewarmed:    true,
		FleetScopeID:   &testFleetScopeID,
		RepositoryIDs:  []uuid.UUID{ownerRepositoryID},
		NamespaceName:  "kodex-rt-repository",
		RuntimeProfile: "go-backend",
	}
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		RepositoryIDs:         []uuid.UUID{requestRepositoryID},
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000725"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if slot.ID == slotID {
		t.Fatalf("slot id = %s, want a new slot because repository scope differs", slot.ID)
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

func TestCleanupBatchScrubsJobAndStepShortLogTails(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	policy, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopePlatform,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		KeepShortLogTail: false,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000726"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy(): %v", err)
	}
	slotID := mustUUID("00000000-0000-0000-0000-000000000727")
	jobID := mustUUID("00000000-0000-0000-0000-000000000728")
	stepID := mustUUID("00000000-0000-0000-0000-000000000729")
	repo.slots[slotID] = cleanupSlot(slotID)
	repo.jobs[jobID] = entity.Job{
		Base:         entity.Base{ID: jobID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		JobType:      enum.JobTypeCleanup,
		Status:       enum.JobStatusSucceeded,
		SlotID:       &slotID,
		ShortLogTail: "job tail",
		Steps: []entity.JobStep{
			{
				Base:         entity.Base{ID: stepID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
				JobID:        jobID,
				StepKey:      "cleanup",
				Status:       enum.JobStepStatusSucceeded,
				ShortLogTail: "step tail",
			},
		},
	}
	_, err = svc.RunCleanupBatch(context.Background(), RunCleanupBatchInput{
		CleanupPolicyID: &policy.ID,
		Limit:           10,
		LeaseOwner:      "worker/runtime-cleanup",
		LeaseUntil:      testNow.Add(10 * time.Minute),
		Meta:            commandMeta(mustUUID("00000000-0000-0000-0000-000000000730"), 0),
	})
	if err != nil {
		t.Fatalf("RunCleanupBatch(): %v", err)
	}
	job := repo.jobs[jobID]
	if job.ShortLogTail != "" || job.Steps[0].ShortLogTail != "" {
		t.Fatalf("short log tails = job %q step %q, want scrubbed", job.ShortLogTail, job.Steps[0].ShortLogTail)
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

func TestCleanupPolicyRejectsOrganizationScopeUntilSlotProjectionExists(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	_, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopeOrganization,
		ScopeID:          "org-1",
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000731"), 0),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateOrUpdateCleanupPolicy() err = %v, want invalid argument", err)
	}
}

func TestCleanupPolicyRejectsNonUUIDProjectAndRepositoryScopes(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	for _, scope := range []enum.RuntimeScopeType{enum.RuntimeScopeProject, enum.RuntimeScopeRepository} {
		_, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
			ScopeType:        scope,
			ScopeID:          "not-a-uuid",
			TTLSeconds:       60,
			FailedTTLSeconds: 60,
			Status:           enum.CleanupPolicyStatusActive,
			Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000746"), 0),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("CreateOrUpdateCleanupPolicy(%s) err = %v, want invalid argument", scope, err)
		}
	}
}

func TestCleanupPolicyUpdateAuthorizesCurrentAndRequestedScopes(t *testing.T) {
	t.Parallel()

	authorizer := &recordAuthorizer{}
	svc, _ := newTestServiceWithAuthorizer(authorizer)
	currentProjectID := mustUUID("00000000-0000-0000-0000-000000000732").String()
	targetProjectID := mustUUID("00000000-0000-0000-0000-000000000733").String()
	policy, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopeProject,
		ScopeID:          currentProjectID,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000734"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy(): %v", err)
	}
	authorizer.requests = nil
	_, err = svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		CleanupPolicyID:  &policy.ID,
		ScopeType:        enum.RuntimeScopeProject,
		ScopeID:          targetProjectID,
		TTLSeconds:       120,
		FailedTTLSeconds: 120,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             commandMeta(mustUUID("00000000-0000-0000-0000-000000000735"), policy.Version),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy() update: %v", err)
	}
	if len(authorizer.requests) != 2 {
		t.Fatalf("authorization requests = %d, want current and requested scopes", len(authorizer.requests))
	}
	if authorizer.requests[0].ScopeID != currentProjectID || authorizer.requests[1].ScopeID != targetProjectID {
		t.Fatalf("authorization scope ids = %#v, want current then requested", authorizer.requests)
	}
}

func TestCleanupPolicyReplayRejectsMismatchedScope(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000739"), 0)
	projectID := mustUUID("00000000-0000-0000-0000-000000000740").String()
	otherProjectID := mustUUID("00000000-0000-0000-0000-000000000741").String()
	_, err := svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopeProject,
		ScopeID:          projectID,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             meta,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanupPolicy(): %v", err)
	}
	_, err = svc.CreateOrUpdateCleanupPolicy(context.Background(), CreateOrUpdateCleanupPolicyInput{
		ScopeType:        enum.RuntimeScopeProject,
		ScopeID:          otherProjectID,
		TTLSeconds:       60,
		FailedTTLSeconds: 60,
		Status:           enum.CleanupPolicyStatusActive,
		Meta:             meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want conflict for mismatched scope", err)
	}
}

func TestPrewarmPoolReplayRejectsMismatchedScope(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000736"), 0)
	projectID := mustUUID("00000000-0000-0000-0000-000000000737").String()
	otherProjectID := mustUUID("00000000-0000-0000-0000-000000000738").String()
	_, err := svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		ScopeType:      enum.PrewarmPoolScopeProject,
		ScopeID:        projectID,
		RuntimeProfile: "go-backend",
		TargetSize:     1,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           meta,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdatePrewarmPool(): %v", err)
	}
	_, err = svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		ScopeType:      enum.PrewarmPoolScopeProject,
		ScopeID:        otherProjectID,
		RuntimeProfile: "go-backend",
		TargetSize:     1,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want conflict for mismatched scope", err)
	}
}

func TestPrewarmPoolUpdateAuthorizesCurrentAndRequestedScopes(t *testing.T) {
	t.Parallel()

	authorizer := &recordAuthorizer{}
	svc, _ := newTestServiceWithAuthorizer(authorizer)
	currentProjectID := mustUUID("00000000-0000-0000-0000-000000000742").String()
	targetProjectID := mustUUID("00000000-0000-0000-0000-000000000743").String()
	pool, err := svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		ScopeType:      enum.PrewarmPoolScopeProject,
		ScopeID:        currentProjectID,
		RuntimeProfile: "go-backend",
		TargetSize:     1,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           commandMeta(mustUUID("00000000-0000-0000-0000-000000000744"), 0),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdatePrewarmPool(): %v", err)
	}
	authorizer.requests = nil
	_, err = svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
		PrewarmPoolID:  &pool.ID,
		ScopeType:      enum.PrewarmPoolScopeProject,
		ScopeID:        targetProjectID,
		RuntimeProfile: "go-backend",
		TargetSize:     2,
		Status:         enum.PrewarmPoolStatusActive,
		Meta:           commandMeta(mustUUID("00000000-0000-0000-0000-000000000745"), pool.Version),
	})
	if err != nil {
		t.Fatalf("CreateOrUpdatePrewarmPool() update: %v", err)
	}
	if len(authorizer.requests) != 2 {
		t.Fatalf("authorization requests = %d, want current and requested scopes", len(authorizer.requests))
	}
	if authorizer.requests[0].ScopeID != currentProjectID || authorizer.requests[1].ScopeID != targetProjectID {
		t.Fatalf("authorization scope ids = %#v, want current then requested", authorizer.requests)
	}
}

func TestPrewarmPoolRejectsNonUUIDProjectAndRepositoryScopes(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	for _, scope := range []enum.PrewarmPoolScopeType{enum.PrewarmPoolScopeProject, enum.PrewarmPoolScopeRepository} {
		_, err := svc.CreateOrUpdatePrewarmPool(context.Background(), CreateOrUpdatePrewarmPoolInput{
			ScopeType:      scope,
			ScopeID:        "not-a-uuid",
			RuntimeProfile: "go-backend",
			TargetSize:     1,
			Status:         enum.PrewarmPoolStatusActive,
			Meta:           commandMeta(mustUUID("00000000-0000-0000-0000-000000000747"), 0),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("CreateOrUpdatePrewarmPool(%s) err = %v, want invalid argument", scope, err)
		}
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
