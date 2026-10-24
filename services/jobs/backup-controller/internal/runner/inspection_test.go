package runner

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// inspectionFixture supplies a real validated manifest with private object keys for projection and owner-binding regression tests.
// inspectionFixture создаёт валидный манифест с приватными ключами для проверки проекции и owner binding.
func inspectionFixture() (manifest.Manifest, manifest.Receipt, manifest.RestoreDrill) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	hash := "sha256:" + strings.Repeat("a", 64)
	receipt := manifest.Receipt{Bucket: "private-bucket", Key: "PRIVATE_OBJECT_KEY", VersionID: "version-1", ETag: "etag", ChecksumSHA256: hash, SizeBytes: 3}
	value := manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup", BackupID: "20261025T000000Z-0123456789abcdef", State: "complete", ControllerVersion: "test", ReleaseRevision: hash, StartedAt: now, CompletedAt: now.Add(time.Minute), ConsistencyModel: manifest.BoundedCrashConsistencyModel, ConsistencyStarted: now, ConsistencyFinished: now.Add(time.Second), DatabaseCount: 1, PlatformObjectCount: 1,
		Databases: []manifest.Database{{Name: "control-plane", Engine: "postgresql", ServerVersion: "180003", SchemaKind: "goose", SchemaVersion: "goose:1", SchemaChecksum: hash, SnapshotStarted: now, SnapshotFinished: now.Add(time.Second), Dump: receipt, Schema: receipt}}, PlatformObjects: []manifest.PlatformObject{{StoreName: "artifacts", Source: receipt, Backup: receipt}}}
	drill := manifest.RestoreDrill{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-restore-drill", BackupID: value.BackupID, RestoreID: "qa-restore-test", ApprovalID: "qa-approval-test", TargetSetSHA256: hash, CompletedAt: now.Add(2 * time.Minute), Databases: []manifest.RestoreDatabase{{Name: "control-plane", SchemaVersion: "goose:1", TargetDigest: hash}}, Objects: []manifest.Receipt{receipt}}
	drill.RequestSHA256 = manifest.RequestDigest(drill.ApprovalID, drill.RestoreID, drill.BackupID, hash)
	return value, receipt, drill
}

// TestInspectionProjectionExcludesPrivateKeys checks safe aggregate metadata and exact byte accounting from validated receipts.
// TestInspectionProjectionExcludesPrivateKeys проверяет безопасные агрегаты и точный подсчёт размера из валидных квитанций.
func TestInspectionProjectionExcludesPrivateKeys(t *testing.T) {
	value, receipt, drill := inspectionFixture()
	result, err := projectRestoreMetadata(value, receipt, drill, drill.RestoreID, drill.ApprovalID, drill.TargetSetSHA256)
	if err != nil || result.Backup.PayloadBytes != 9 || result.DatabaseCount != 1 || result.ObjectCount != 1 {
		t.Fatalf("safe metadata projection failed: %v", err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "PRIVATE_OBJECT_KEY") || strings.Contains(string(raw), "private-bucket") {
		t.Fatal("private locator leaked")
	}
}

// TestInspectionRejectsForeignOwnerPins rejects a valid foreign drill or malformed structure before any successful projection.
// TestInspectionRejectsForeignOwnerPins отклоняет валидную чужую либо повреждённую квитанцию до выдачи результата.
func TestInspectionRejectsForeignOwnerPins(t *testing.T) {
	for _, defect := range []string{"approval", "target", "request", "restore", "schema"} {
		t.Run(defect, func(t *testing.T) {
			value, receipt, drill := inspectionFixture()
			expected := drill
			switch defect {
			case "approval":
				drill.ApprovalID = "foreign-approval"
			case "target":
				drill.TargetSetSHA256 = "sha256:" + strings.Repeat("b", 64)
			case "request":
				drill.RequestSHA256 = "sha256:" + strings.Repeat("b", 64)
			case "restore":
				drill.RestoreID = "foreign-restore"
			case "schema":
				drill.Databases[0].SchemaVersion = "goose:2"
			}
			result, err := projectRestoreMetadata(value, receipt, drill, expected.RestoreID, expected.ApprovalID, expected.TargetSetSHA256)
			if err == nil || result.RestoreID != "" {
				t.Fatal("foreign or corrupt receipt became successful metadata")
			}
		})
	}
}

// TestInspectionRejectsOverflow prevents a valid large receipt set from wrapping safe numeric counters.
// TestInspectionRejectsOverflow не позволяет допустимым большим размерам переполнить безопасные счётчики.
func TestInspectionRejectsOverflow(t *testing.T) {
	value, receipt, _ := inspectionFixture()
	value.Databases[0].Dump.SizeBytes = math.MaxInt64
	if result, err := projectBackupMetadata(value, receipt); err == nil || result.BackupID != "" {
		t.Fatal("overflow returned a partial projection")
	}
}
