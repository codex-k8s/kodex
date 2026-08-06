package resource

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func TestValidateRestoreRuntimeSourcePinsEligibleArchive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	execution := RuntimeExecution{
		ID: "backup", SessionID: "session", Version: 7, Fence: 11, State: "FAILED",
		CleanupAuthorizationState: "CONSUMED", ArchiveReference: "archive-reference",
		ArchiveSHA256: strings.Repeat("a", 64), ArchiveObjectKey: "object-key",
		ArchiveVersionID: "object-version", ArchiveKMSKeyARN: "arn:aws:kms:region:account:key/id",
		ArchiveObjectLockMode: "COMPLIANCE", ArchiveProvenanceSHA256: strings.Repeat("b", 64),
		RestoreProofReference: "restore-proof", RestoreProofSHA256: strings.Repeat("c", 64),
		CleanupDeletionProofSHA256: strings.Repeat("d", 64), ArchiveRetainUntil: now.Add(time.Hour),
		RetentionPolicyID: "policy", RetentionPolicyVersion: 3,
	}
	intent := restoreRuntimeIntent{
		BackupID: execution.ID, SessionID: execution.SessionID,
		SourceVersion: execution.Version, SourceFence: execution.Fence,
		ExpectedBackupVersion: execution.Version,
		ArchiveSHA256:         execution.ArchiveSHA256,
		ProvenanceSHA256:      execution.ArchiveProvenanceSHA256,
	}
	if err := validateRestoreRuntimeSource(execution, execution, intent, now); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	staleProjection := intent
	staleProjection.ExpectedBackupVersion++
	if err := validateRestoreRuntimeSource(execution, execution, staleProjection, now); err == nil {
		t.Fatal("stale dynamic backup version accepted for a new restore command")
	}

	tests := []struct {
		name   string
		mutate func(*RuntimeExecution)
	}{
		{name: "expired retention", mutate: func(value *RuntimeExecution) { value.ArchiveRetainUntil = now }},
		{name: "missing private proof", mutate: func(value *RuntimeExecution) { value.RestoreProofReference = "" }},
		{name: "wrong state", mutate: func(value *RuntimeExecution) { value.State = "RETRIED" }},
		{name: "missing object locator", mutate: func(value *RuntimeExecution) { value.ArchiveObjectKey = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := execution
			test.mutate(&changed)
			if err := validateRestoreRuntimeSource(changed, changed, intent, now); err == nil {
				t.Fatal("invalid restore source accepted")
			}
		})
	}
}

func TestRestoreSourceAuthorityPinsPrivateTuple(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	execution := RuntimeExecution{
		ID: "2d990875-d278-4325-98bb-07a0bf61b9d7", SessionID: "d7490930-7232-46cd-8cf7-052c31db6f4b",
		Version: 7, Fence: 11, RuntimeRevisionSHA256: strings.Repeat("1", 64),
		ImmutableInputSHA256: strings.Repeat("2", 64), ArchiveReference: "archive-reference",
		ArchiveSHA256: strings.Repeat("3", 64), ArchiveObjectKey: "private/object/key",
		ArchiveVersionID: "version-a", ArchiveKMSKeyARN: "arn:aws:kms:region:account:key/id",
		ArchiveObjectLockMode: "COMPLIANCE", ArchiveProvenanceSHA256: strings.Repeat("4", 64),
		RestoreProofReference: "proof-reference", RestoreProofSHA256: strings.Repeat("5", 64),
		RetentionPolicyID: "policy", RetentionPolicyVersion: 3, ArchiveRetainUntil: now.Add(time.Hour),
	}
	digest, err := runtimeRestoreSourceAuthoritySHA256(execution)
	if err != nil || !validSHA256Text(digest) {
		t.Fatalf("authority digest: %q %v", digest, err)
	}
	changed := execution
	changed.ArchiveVersionID = "version-b"
	changedDigest, err := runtimeRestoreSourceAuthoritySHA256(changed)
	if err != nil || changedDigest == digest {
		t.Fatal("private source version is not bound to restore authority")
	}
}

func TestProtectedRegistriesCloseProjectGenericLifecycle(t *testing.T) {
	t.Parallel()
	if !protectedCreateKind(enum.KindProject) || !protectedMutationKind(enum.KindProject) ||
		!protectedTransitionKind(enum.KindProject) || !ownerBoundLifecycleKind(enum.KindProject) {
		t.Fatal("PROJECT generic lifecycle bypass remains open")
	}
}

func TestCreateProjectRequiresSpecializedCommand(t *testing.T) {
	t.Parallel()
	if createCommandAllowed(enum.KindProject, false, false) {
		t.Fatal("generic PROJECT create must be denied")
	}
	if createCommandAllowed(enum.KindProject, true, false) {
		t.Fatal("administrative PROJECT create must be denied")
	}
	if !createCommandAllowed(enum.KindProject, false, true) {
		t.Fatal("specialized PROJECT create must be allowed")
	}
	if createCommandAllowed(enum.KindSchedule, false, true) {
		t.Fatal("specialized PROJECT authority must not create SCHEDULE")
	}
	if !createCommandAllowed(enum.KindChat, false, false) {
		t.Fatal("ordinary generic create must remain available")
	}
}

func TestOwnerGateChangesRequestedReceiptSurvivesContinuationProgress(t *testing.T) {
	t.Parallel()
	storedSpec := entity.ProcessRunSpec{
		RootInitiatorActorID: "actor", RootSessionID: "session", RootTurnID: "root-turn",
		ContinuationTurnID: "continuation", ContinuationTurnVersion: 2,
		ContinuationInputSHA256: strings.Repeat("a", 64),
	}
	currentSpec := storedSpec
	currentSpec.Outcome = "continuation_completed"
	stored := entity.Resource{ID: "process", OrganizationID: "organization", ProjectID: "project",
		OwnerActorID: "actor", Kind: enum.KindProcessRun, Version: 7, Spec: storedSpec}
	current := stored
	current.Version = 10
	current.Spec = currentSpec
	gate := entity.OwnerGateSpec{Decision: "CHANGES_REQUESTED", ContinuationTurnID: "continuation",
		ContinuationTurnVersion: 2, ContinuationInputSHA256: strings.Repeat("a", 64)}
	if err := ownerGateReceiptProcessValid(current, stored, gate); err != nil {
		t.Fatalf("valid advanced continuation rejected: %v", err)
	}
}
