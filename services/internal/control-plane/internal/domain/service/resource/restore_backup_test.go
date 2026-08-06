package resource

import (
	"strings"
	"testing"
	"time"
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
		ArchiveSHA256:    execution.ArchiveSHA256,
		ProvenanceSHA256: execution.ArchiveProvenanceSHA256,
	}
	if err := validateRestoreRuntimeSource(execution, execution, intent, now); err != nil {
		t.Fatalf("valid source rejected: %v", err)
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
