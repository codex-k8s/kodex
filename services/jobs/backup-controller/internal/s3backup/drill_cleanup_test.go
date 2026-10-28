package s3backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// cleanupFixture writes one exact target version into an isolated fake fixture bucket.
// cleanupFixture записывает одну точную версию цели в изолированный поддельный fixture bucket.
func cleanupFixture(t *testing.T) (*Client, *fakeS3, manifest.RestoreDrill) {
	t.Helper()
	fake := newFakeS3()
	client := &Client{api: fake, config: configspec.S3{Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", Bucket: "kodex-restore-fixture", Prefix: "e2e-restore-test"}}
	hash := sha256.Sum256([]byte("fixture"))
	receipt, err := client.putBytes(context.Background(), "e2e-restore-test/artifacts/object", "application/octet-stream", []byte("fixture"), "sha256:"+hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal(err)
	}
	return client, fake, manifest.RestoreDrill{Objects: []manifest.Receipt{receipt}}
}

// TestCleanupUsesOnlyExactReceiptVersions checks independent presence, exact conditional deletion and empty readback without changing a source-like sibling.
// TestCleanupUsesOnlyExactReceiptVersions проверяет отдельное чтение, точное условное удаление и пустой readback без изменения соседнего source-подобного объекта.
func TestCleanupUsesOnlyExactReceiptVersions(t *testing.T) {
	client, fake, drill := cleanupFixture(t)
	fake.objects["organizations/source"] = fakeObject{body: "source", version: "source-v1"}
	if count, err := client.InspectDrillTarget(context.Background(), drill, TargetComplete); err != nil || count != 1 {
		t.Fatal("target inspection failed", err)
	}
	if fake.lastDeleteVersionID != "" {
		t.Fatal("inspection deleted an object")
	}
	if count, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanup); err != nil || count != 0 {
		t.Fatal("cleanup failed", err)
	}
	if fake.lastDeleteVersionID != drill.Objects[0].VersionID || fake.lastDeleteIfMatch != drill.Objects[0].ETag {
		t.Fatal("delete lost exact receipt preconditions")
	}
	if _, exists := fake.objects["organizations/source"]; !exists {
		t.Fatal("source-like sibling was deleted")
	}
}

// TestCleanupRejectsForeignVersionBeforeEffects rejects foreign objects, corrupted receipts or live-bucket targets before the first delete.
// TestCleanupRejectsForeignVersionBeforeEffects отклоняет чужие объекты, повреждённые квитанции и live bucket до первого удаления.
func TestCleanupRejectsForeignVersionBeforeEffects(t *testing.T) {
	for _, defect := range []string{"foreign", "version", "bucket", "prefix", "digest"} {
		t.Run(defect, func(t *testing.T) {
			client, fake, drill := cleanupFixture(t)
			switch defect {
			case "foreign":
				fake.objects["e2e-restore-test/foreign"] = fakeObject{body: "foreign", version: "v2"}
			case "version":
				drill.Objects[0].VersionID = "foreign-v2"
			case "bucket":
				client.config.Bucket = "kodex-artifacts"
			case "prefix":
				client.config.Prefix = "organizations"
			case "digest":
				drill.Objects[0].ChecksumSHA256 = "sha256:invalid"
			}
			if _, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanup); err == nil {
				t.Fatal("foreign target accepted")
			}
			if fake.lastDeleteVersionID != "" {
				t.Fatal("unsafe target performed a delete")
			}
		})
	}
}

// TestCleanupUnknownDeleteRequiresReadback preserves uncertain outcome and permits read-only absence confirmation without another delete.
// TestCleanupUnknownDeleteRequiresReadback сохраняет неизвестный исход и позволяет подтвердить отсутствие отдельным чтением без повторного удаления.
func TestCleanupUnknownDeleteRequiresReadback(t *testing.T) {
	client, fake, drill := cleanupFixture(t)
	fake.deleteResponseLost = true
	if _, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanup); err == nil {
		t.Fatal("lost response reported success")
	}
	before := fake.lastDeleteVersionID
	if count, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanupReadback); err != nil || count != 0 {
		t.Fatal("absence readback failed", err)
	}
	if fake.lastDeleteVersionID != before {
		t.Fatal("readback repeated deletion")
	}
}

// TestFullTargetInspectionRejectsMissingVersions separates a missing restored object from an explicitly empty cleanup result.
// TestFullTargetInspectionRejectsMissingVersions отделяет утрату восстановленного объекта от явно пустого результата cleanup.
func TestFullTargetInspectionRejectsMissingVersions(t *testing.T) {
	for _, missing := range []string{"one", "all"} {
		t.Run(missing, func(t *testing.T) {
			client, fake, drill := cleanupFixture(t)
			second := drill.Objects[0]
			second.Key = "e2e-restore-test/artifacts/second"
			fake.objects[second.Key] = fake.objects[drill.Objects[0].Key]
			drill.Objects = append(drill.Objects, second)
			delete(fake.objects, drill.Objects[0].Key)
			if missing == "all" {
				delete(fake.objects, second.Key)
			}
			if _, err := client.InspectDrillTarget(context.Background(), drill, TargetComplete); err == nil {
				t.Fatal("missing target reported complete")
			}
			if fake.lastDeleteVersionID != "" {
				t.Fatal("failed initial inspection performed effects")
			}
			remaining, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanupReadback)
			expected := 1
			if missing == "all" {
				expected = 0
			}
			if err != nil || remaining != expected {
				t.Fatal("explicit cleanup readback did not measure remaining set", err)
			}
			if _, err := client.InspectDrillTarget(context.Background(), drill, TargetCleanup); err != nil {
				t.Fatal("explicit cleanup could not resume exact remaining subset", err)
			}
		})
	}
}
