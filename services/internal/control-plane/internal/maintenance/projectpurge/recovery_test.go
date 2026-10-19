package projectpurge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

type recoverySourceFixture struct {
	sourceFixture
	pinned  bool
	pinErr  error
	outcome string
	receipt objectstorage.Receipt
}

func (source *recoverySourceFixture) PinProjectPurgeRecovery(_ context.Context, _ string, _ platform.ProjectPurgeRecoveryClaim, receipt objectstorage.Receipt) error {
	if source.pinErr != nil {
		return source.pinErr
	}
	source.pinned = true
	source.receipt = receipt
	return nil
}
func (source *recoverySourceFixture) FinishProjectPurgeRecovery(_ context.Context, _ string, _ platform.ProjectPurgeRecoveryClaim, outcome string) error {
	source.outcome = outcome
	return nil
}

type recoveryObjectFixture struct {
	*objectstoragetest.Store
	source  *recoverySourceFixture
	deletes int
}

func (objects *recoveryObjectFixture) Delete(ctx context.Context, key, version string) error {
	objects.deletes++
	if !objects.source.pinned || objects.source.receipt.Key != key || objects.source.receipt.VersionID != version {
		return errors.New("recovery deleted without durable exact receipt")
	}
	return objects.Store.Delete(ctx, key, version)
}

func TestRecoveryAbsenceIsObservationAndLateObjectIsDeleted(t *testing.T) {
	source := &recoverySourceFixture{}
	objects := &recoveryObjectFixture{Store: objectstoragetest.New(), source: source}
	processor := New(source, objects, nil, nil, "")
	claim := platform.ProjectPurgeRecoveryClaim{Key: "organizations/org_recovery/projects/prj_recovery/artifacts/art_recovery/data", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, Generation: 1, Deadline: time.Now().Add(time.Minute)}
	if err := processor.recoverObject(t.Context(), "owner", claim); err != nil || source.outcome != "ABSENT" || source.pinned || objects.deletes != 0 {
		t.Fatalf("unknown404 became deletion: outcome=%s err=%v", source.outcome, err)
	}
	if _, err := objects.Put(t.Context(), objectstorage.PutInput{Key: claim.Key, Digest: claim.Digest, SizeBytes: 1, MediaType: "text/plain", Body: strings.NewReader("x")}); err != nil {
		t.Fatal(err)
	}
	if err := processor.recoverObject(t.Context(), "owner", claim); err != nil || source.outcome != "DELETED" || !source.pinned || objects.deletes != 1 {
		t.Fatalf("late exact object not deleted: outcome=%s err=%v", source.outcome, err)
	}
	// Crash после durable pin: отсутствие exact pinned version — отдельное доказательство.
	claim.Version = source.receipt.VersionID
	claim.ETag = source.receipt.ETag
	if err := processor.recoverObject(t.Context(), "next-owner", claim); err != nil || source.outcome != "DELETED" || objects.deletes != 1 {
		t.Fatal("pinned absence lost durable result", err)
	}
}

func TestRecoveryRejectsForeignBytesAndPinFailure(t *testing.T) {
	for _, pinFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "digest mismatch", true: "durable pin failed"}[pinFails], func(t *testing.T) {
			source := &recoverySourceFixture{}
			objects := &recoveryObjectFixture{Store: objectstoragetest.New(), source: source}
			claim := platform.ProjectPurgeRecoveryClaim{Key: "organizations/org_recovery/projects/prj_recovery/artifacts/art_recovery/bytes", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, Generation: 1, Deadline: time.Now().Add(time.Minute)}
			digest := claim.Digest
			if pinFails {
				source.pinErr = errors.New("receipt storage unavailable")
			} else {
				digest = "sha256:" + strings.Repeat("b", 64)
			}
			if _, err := objects.Put(t.Context(), objectstorage.PutInput{Key: claim.Key, Digest: digest, SizeBytes: 1, MediaType: "text/plain", Body: strings.NewReader("x")}); err != nil {
				t.Fatal(err)
			}
			if err := New(source, objects, nil, nil, "").recoverObject(t.Context(), "owner", claim); err == nil || source.outcome != "FAILED" || objects.deletes != 0 {
				t.Fatalf("unsafe effect allowed: deletes=%d outcome=%s err=%v", objects.deletes, source.outcome, err)
			}
		})
	}
}
