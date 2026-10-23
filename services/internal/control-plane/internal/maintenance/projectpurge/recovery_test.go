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

// recoverySourceFixture extends the ordinary purge fixture with recorded pin receipt/outcome and an injected pin failure for recovery ordering assertions.
// recoverySourceFixture дополняет обычный fixture очистки записанной pin-квитанцией/результатом и внедряемым отказом pin для проверки порядка восстановления.
type recoverySourceFixture struct {
	sourceFixture
	pinned  bool
	pinErr  error
	outcome string
	receipt objectstorage.Receipt
}

// PinProjectPurgeRecovery fails before recording a pin when configured, otherwise stores the exact observed receipt and marks it pinned for the deletion spy.
// PinProjectPurgeRecovery при настройке отказывает до записи pin, иначе сохраняет точную наблюдаемую квитанцию и признак привязки для spy удаления.
func (source *recoverySourceFixture) PinProjectPurgeRecovery(_ context.Context, _ string, _ platform.ProjectPurgeRecoveryClaim, receipt objectstorage.Receipt) error {
	if source.pinErr != nil {
		return source.pinErr
	}
	source.pinned = true
	source.receipt = receipt
	return nil
}

// FinishProjectPurgeRecovery records only the recovery outcome supplied by the processor; it is a unit receipt spy, not a database owner transition.
// FinishProjectPurgeRecovery записывает лишь переданный процессором результат восстановления; это unit spy квитанции, а не переход владельца БД.
func (source *recoverySourceFixture) FinishProjectPurgeRecovery(_ context.Context, _ string, _ platform.ProjectPurgeRecoveryClaim, outcome string) error {
	source.outcome = outcome
	return nil
}

// recoveryObjectFixture wraps synthetic object storage with a delete counter and the recovery receipt spy so tests can forbid deletion before a matching pin.
// recoveryObjectFixture оборачивает синтетическое хранилище счётчиком удалений и spy квитанции восстановления, позволяя запретить удаление до совпадающего pin.
type recoveryObjectFixture struct {
	*objectstoragetest.Store
	source  *recoverySourceFixture
	deletes int
}

// Delete increments the deletion observation, rejects calls without a pinned matching key/version and delegates only confirmed deletes to the synthetic store.
// Delete увеличивает счётчик наблюдения удаления, отклоняет вызов без привязанных совпадающих key/version и передаёт лишь подтверждённое удаление синтетическому хранилищу.
func (objects *recoveryObjectFixture) Delete(ctx context.Context, key, version string) error {
	objects.deletes++
	if !objects.source.pinned || objects.source.receipt.Key != key || objects.source.receipt.VersionID != version {
		return errors.New("recovery deleted without durable exact receipt")
	}
	return objects.Store.Delete(ctx, key, version)
}

// TestRecoveryAbsenceIsObservationAndLateObjectIsDeleted distinguishes initial unpinned ABSENT from exact late-object deletion and proves a replacement worker can finish a pinned absent version without another delete.
// TestRecoveryAbsenceIsObservationAndLateObjectIsDeleted различает начальное непривязанное ABSENT и удаление точного позднего объекта, доказывая завершение отсутствующей привязанной версии новым worker без повторного Delete.
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

// TestRecoveryRejectsForeignBytesAndPinFailure requires FAILED with no storage deletion for either a mismatched content digest or failure to persist the exact recovery receipt.
// TestRecoveryRejectsForeignBytesAndPinFailure требует FAILED без удаления хранилища как при несовпадении digest содержимого, так и при отказе сохранения точной квитанции восстановления.
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
