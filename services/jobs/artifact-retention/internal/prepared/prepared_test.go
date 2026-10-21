package prepared

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// repositoryFixture records whether cleanup finalized and can inject a lost-fence error.
// RU: repositoryFixture записывает конечный исход очистки и может вернуть ошибку утраты fence.
type repositoryFixture struct {
	claim       Claim
	success     bool
	finished    int
	recordError error
}

// RecordPreparedReceipt returns the injected fencing error before deletion may start.
// RU: RecordPreparedReceipt возвращает заданную ошибку fencing до начала удаления.
func (r *repositoryFixture) RecordPreparedReceipt(context.Context, Claim, string, objectstorage.Receipt) error {
	return r.recordError
}

// ClaimPrepared returns the single synthetic ledger claim for the current test.
// RU: ClaimPrepared возвращает единственный синтетический ledger claim текущего теста.
func (r *repositoryFixture) ClaimPrepared(context.Context, string, int, int64) ([]Claim, error) {
	return []Claim{r.claim}, nil
}

// TestPreparedCleanupLostFencePreventsDelete proves a failed receipt fence blocks object deletion.
// RU: TestPreparedCleanupLostFencePreventsDelete доказывает, что потеря fence запрещает удаление объекта.
func TestPreparedCleanupLostFencePreventsDelete(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := objectstorage.Receipt{Key: "key", VersionID: "version", ETag: "etag", Digest: digest, SizeBytes: 3}
	repository := &repositoryFixture{claim: Claim{LedgerID: "ledger", ObjectKey: "key", Digest: digest, SizeBytes: 3, Generation: 1, Uncertain: true}, recordError: ErrLostClaim}
	objects := &objectsFixture{receipt: receipt}
	count, err := NewProcessor(repository, objects).Process(t.Context(), "owner", 1, 30)
	if count != 0 || !errors.Is(err, ErrLostClaim) || objects.deleteCount != 0 || repository.finished != 0 {
		t.Fatal("lost fence allowed external delete")
	}
}

// FinishPrepared captures a terminal cleanup outcome and increments its call count.
// RU: FinishPrepared сохраняет конечный исход очистки и увеличивает счётчик вызовов.
func (r *repositoryFixture) FinishPrepared(_ context.Context, _ Claim, _ string, success bool, _ objectstorage.Receipt) error {
	r.success = success
	r.finished++
	return nil
}

// objectsFixture emulates one immutable object version and tracks exact deletion.
// RU: objectsFixture имитирует одну неизменяемую версию объекта и отслеживает точное удаление.
type objectsFixture struct {
	receipt     objectstorage.Receipt
	headErr     error
	deleted     bool
	deleteCount int
}

// Head returns the configured receipt until the exact version has been deleted.
// RU: Head возвращает заданный receipt, пока точная версия не удалена.
func (o *objectsFixture) Head(context.Context, string, string) (objectstorage.Receipt, error) {
	if o.deleted {
		return objectstorage.Receipt{}, objectstorage.ErrNotFound
	}
	return o.receipt, o.headErr
}

// Delete rejects a foreign key or version and records a successful exact deletion.
// RU: Delete отклоняет чужой ключ либо версию и фиксирует успешное точное удаление.
func (o *objectsFixture) Delete(_ context.Context, key, version string) error {
	if key != o.receipt.Key || version != o.receipt.VersionID {
		return errors.New("wrong exact deletion")
	}
	o.deleted = true
	o.deleteCount++
	return nil
}

// TestPreparedCleanupOutcome distinguishes unknown writes from accepted receipts and
// checks that only confirmed exact deletion or an already-absent known version succeeds.
// RU: TestPreparedCleanupOutcome различает неизвестные записи и принятые receipts,
// засчитывая лишь подтверждённое точное удаление либо уже отсутствующую известную версию.
func TestPreparedCleanupOutcome(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := objectstorage.Receipt{Key: "organizations/org/projects/project/artifacts/art/rev/digest", VersionID: "v1", ETag: "etag", Digest: digest, SizeBytes: 3}
	for _, test := range []struct {
		name      string
		uncertain bool
		headErr   error
		mismatch  bool
		success   bool
		deletes   int
	}{
		{name: "unknown-404", uncertain: true, headErr: objectstorage.ErrNotFound},
		{name: "unknown-unavailable", uncertain: true, headErr: objectstorage.ErrUnavailable},
		{name: "unknown-foreign-digest", uncertain: true, mismatch: true},
		{name: "unknown-positive-exact", uncertain: true, success: true, deletes: 1},
		{name: "known-positive-exact", success: true, deletes: 1},
		{name: "known-delete-ack-lost", headErr: objectstorage.ErrNotFound, success: true},
		{name: "known-mismatch", mismatch: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := Claim{LedgerID: "ledger", ObjectKey: receipt.Key, Digest: digest, SizeBytes: 3, Generation: 1, Uncertain: test.uncertain}
			if !test.uncertain {
				claim.ObjectVersion = "v1"
				claim.ObjectETag = "etag"
			}
			r := &repositoryFixture{claim: claim}
			o := &objectsFixture{receipt: receipt, headErr: test.headErr}
			if test.mismatch {
				o.receipt.Digest = "sha256:" + strings.Repeat("b", 64)
			}
			count, err := NewProcessor(r, o).Process(t.Context(), "test", 1, 30)
			if r.finished != 1 || r.success != test.success || o.deleteCount != test.deletes ||
				(err == nil) != test.success || (count == 1) != test.success {
				t.Fatalf("wrong cleanup outcome count=%d success=%v deletes=%d err=%v", count, r.success, o.deleteCount, err)
			}
		})
	}
}
