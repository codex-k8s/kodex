package manifest

import (
	"strings"
	"testing"
	"time"
)

// TestManifestRequiresCompleteExactReceipts rejects backup objects without an exact version after valid readback.
// RU: TestManifestRequiresCompleteExactReceipts отклоняет объекты без точной версии после успешной проверки манифеста.
func TestManifestRequiresCompleteExactReceipts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	receipt := Receipt{Bucket: "backup", Key: "key", VersionID: "v1", ETag: "etag",
		ChecksumSHA256: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
	value := Manifest{
		SchemaVersion: SchemaVersion, Kind: "kodex-backup", BackupID: "20260828T120000Z-0123456789abcdef",
		State: "complete", ControllerVersion: "test", ReleaseRevision: "sha256:test",
		StartedAt: now, CompletedAt: now.Add(time.Minute), DatabaseCount: 1, PlatformObjectCount: 1,
		ConsistencyModel: BoundedCrashConsistencyModel, ConsistencyStarted: now,
		ConsistencyFinished: now.Add(30 * time.Second),
		Databases: []Database{{Name: "control-plane", Engine: "postgresql", ServerVersion: "180003",
			SchemaKind: "goose", SchemaVersion: "goose:1", SchemaChecksum: receipt.ChecksumSHA256,
			SnapshotStarted: now, SnapshotFinished: now.Add(time.Second), Dump: receipt, Schema: receipt}},
		PlatformObjects: []PlatformObject{{StoreName: "artifacts", Source: receipt, Backup: receipt}},
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	value.PlatformObjects[0].Backup.VersionID = ""
	if err := value.Validate(); err == nil {
		t.Fatal("Validate() accepted a receipt without exact version")
	}
}

// TestManifestRejectsAnUnboundedOrMisrepresentedConsistencyWindow rejects unsupported claims and out-of-window snapshots.
// RU: TestManifestRejectsAnUnboundedOrMisrepresentedConsistencyWindow отклоняет неподдерживаемую модель и снимки вне окна.
func TestManifestRejectsAnUnboundedOrMisrepresentedConsistencyWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	receipt := Receipt{Bucket: "backup", Key: "key", VersionID: "v1", ETag: "etag",
		ChecksumSHA256: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
	value := Manifest{
		SchemaVersion: SchemaVersion, Kind: "kodex-backup", BackupID: "20260828T120000Z-0123456789abcdef",
		State: "complete", ControllerVersion: "test", ReleaseRevision: "sha256:test",
		StartedAt: now, CompletedAt: now.Add(time.Minute), ConsistencyModel: BoundedCrashConsistencyModel,
		ConsistencyStarted: now, ConsistencyFinished: now.Add(30 * time.Second), DatabaseCount: 1,
		Databases: []Database{{Name: "control-plane", Engine: "postgresql", ServerVersion: "180003",
			SchemaKind: "goose", SchemaVersion: "goose:1", SchemaChecksum: receipt.ChecksumSHA256,
			SnapshotStarted: now, SnapshotFinished: now.Add(time.Second), Dump: receipt, Schema: receipt}},
	}
	value.ConsistencyModel = "TRANSACTIONALLY_CONSISTENT"
	if err := value.Validate(); err == nil {
		t.Fatal("Validate() accepted an unsupported consistency claim")
	}
	value.ConsistencyModel = BoundedCrashConsistencyModel
	value.ConsistencyFinished = now
	if err := value.Validate(); err == nil {
		t.Fatal("Validate() accepted a database snapshot outside the consistency window")
	}
}

// TestRequestDigestBindsFieldPositions verifies that swapping approval and restore fields changes the digest.
// RU: TestRequestDigestBindsFieldPositions проверяет смену дайджеста при перестановке полей подтверждения и восстановления.
func TestRequestDigestBindsFieldPositions(t *testing.T) {
	t.Parallel()
	first := RequestDigest("approval", "restore", "20260828T120000Z-0123456789abcdef", "sha256:"+strings.Repeat("a", 64))
	second := RequestDigest("restore", "approval", "20260828T120000Z-0123456789abcdef", "sha256:"+strings.Repeat("a", 64))
	if first == second {
		t.Fatal("request digest does not bind field positions")
	}
}
