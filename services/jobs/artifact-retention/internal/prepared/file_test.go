package prepared

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// fileRepositoryFixture counts receipt admissions independently of final ledger acknowledgements.
// RU: fileRepositoryFixture считает допуски receipt отдельно от конечных подтверждений ledger.
type fileRepositoryFixture struct {
	repositoryFixture
	records int
}

// RecordPreparedReceipt simulates the durable first-admission response or its lost acknowledgement.
// RU: RecordPreparedReceipt имитирует ответ первого durable admission либо потерянное подтверждение.
func (r *fileRepositoryFixture) RecordPreparedReceipt(ctx context.Context, claim Claim, owner string, receipt objectstorage.Receipt) error {
	r.records++
	return r.repositoryFixture.RecordPreparedReceipt(ctx, claim, owner, receipt)
}

// fileObjectsFixture distinguishes the exact single-attempt transport from ordinary deletion.
// RU: fileObjectsFixture различает транспорт единственной попытки и обычное удаление.
type fileObjectsFixture struct {
	objectsFixture
	once          int
	capabilityErr error
	deleteErr     error
}

// CheckDeleteOnce returns the current exact-transport capability without an external effect.
// RU: CheckDeleteOnce возвращает текущую возможность точного транспорта без внешнего эффекта.
func (o *fileObjectsFixture) CheckDeleteOnce(context.Context) error { return o.capabilityErr }

// DeleteOnce models a single physical request, including a successful delete with a lost response.
// RU: DeleteOnce моделирует один физический запрос, включая успешное удаление с потерянным ответом.
func (o *fileObjectsFixture) DeleteOnce(ctx context.Context, key, version string) error {
	o.once++
	if err := o.objectsFixture.Delete(ctx, key, version); err != nil {
		return err
	}
	return o.deleteErr
}

// VerifyVersionAbsent qualifies this fixture's complete local state, not a generic provider error.
// RU: VerifyVersionAbsent квалифицирует полное локальное состояние fixture, не generic provider error.
func (o *fileObjectsFixture) VerifyVersionAbsent(ctx context.Context, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key != o.receipt.Key || version != o.receipt.VersionID || !o.deleted {
		return objectstorage.ErrConflict
	}
	return nil
}

// TestFileCleanupAdmission separates fresh known credit, PUT uncertainty, and reconciliation-only claims.
// RU: TestFileCleanupAdmission разделяет свежий known-допуск, неизвестный PUT и claims только для сверки.
func TestFileCleanupAdmission(t *testing.T) {
	receipt := objectstorage.Receipt{Key: "key", VersionID: "v1", ETag: "etag", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
	for _, test := range []struct {
		name                                        string
		uncertain, unknownVersion, absent, mismatch bool
		headErr, recordErr, capabilityErr           error
		once, records                               int
		success                                     bool
	}{
		{name: "first-known", once: 1, success: true},
		{name: "known-head-only-present", uncertain: true},
		{name: "known-head-only-absent", uncertain: true, absent: true, success: true},
		{name: "known-head-only-denied", uncertain: true, headErr: objectstorage.ErrUnavailable},
		{name: "unknown-positive-first-admission", uncertain: true, unknownVersion: true, once: 1, records: 1, success: true},
		{name: "unknown-404-is-not-absence", uncertain: true, unknownVersion: true, absent: true},
		{name: "unknown-record-ack-lost", uncertain: true, unknownVersion: true, records: 1, recordErr: ErrLostClaim},
		{name: "unknown-mismatch", uncertain: true, unknownVersion: true, mismatch: true},
		{name: "unsupported-does-not-fallback", capabilityErr: objectstorage.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := Claim{SourceKind: kubernetesFileSource, LedgerID: "ledger", ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID, ObjectETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: receipt.SizeBytes, Generation: 1, Uncertain: test.uncertain}
			if test.unknownVersion {
				claim.ObjectVersion = ""
				claim.ObjectETag = ""
			}
			r := &fileRepositoryFixture{repositoryFixture: repositoryFixture{claim: claim, recordError: test.recordErr}}
			o := &fileObjectsFixture{objectsFixture: objectsFixture{receipt: receipt, deleted: test.absent, headErr: test.headErr}, capabilityErr: test.capabilityErr}
			if test.mismatch {
				o.receipt.Digest = "sha256:" + strings.Repeat("b", 64)
			}
			count, err := NewProcessor(r, o).Process(t.Context(), "owner", 1, 30)
			if (count == 1) != test.success || (err == nil) != test.success || r.success != test.success || o.once != test.once || r.records != test.records || o.deleteCount != o.once {
				t.Fatalf("unexpected FILE cleanup count=%d once=%d records=%d success=%v err=%v", count, o.once, r.records, r.success, err)
			}
		})
	}
}

// TestFileCleanupLostDeleteAcknowledgement permits only HEAD reconciliation for the same admitted version.
// RU: TestFileCleanupLostDeleteAcknowledgement допускает лишь HEAD-сверку уже допущенной версии после потери ответа Delete.
func TestFileCleanupLostDeleteAcknowledgement(t *testing.T) {
	receipt := objectstorage.Receipt{Key: "key", VersionID: "v1", ETag: "etag", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
	r := &fileRepositoryFixture{repositoryFixture: repositoryFixture{claim: Claim{SourceKind: kubernetesFileSource, LedgerID: "ledger", ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID, ObjectETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: 3, Generation: 1}}}
	o := &fileObjectsFixture{objectsFixture: objectsFixture{receipt: receipt}, deleteErr: objectstorage.ErrUnavailable}
	if n, err := NewProcessor(r, o).Process(t.Context(), "owner", 1, 30); n != 0 || err == nil || o.once != 1 {
		t.Fatal("lost delete response was accepted")
	}
	r.claim.Generation++
	r.claim.Uncertain = true
	o.capabilityErr = objectstorage.ErrUnavailable
	if n, err := NewProcessor(r, o).Process(t.Context(), "successor", 1, 30); n != 1 || err != nil || o.once != 1 || r.records != 0 {
		t.Fatal("reconciliation replayed deletion or required new capability")
	}
}

// TestFileCleanupRejectsPartialReceipt prevents a malformed known/unknown discriminator from issuing IO.
// RU: TestFileCleanupRejectsPartialReceipt запрещает IO при повреждённом варианте known/unknown receipt.
func TestFileCleanupRejectsPartialReceipt(t *testing.T) {
	r := &fileRepositoryFixture{repositoryFixture: repositoryFixture{claim: Claim{SourceKind: kubernetesFileSource, LedgerID: "ledger", ObjectKey: "key", ObjectVersion: "v1", Digest: "sha256:" + strings.Repeat("a", 64), Generation: 1, Uncertain: true}}}
	o := &fileObjectsFixture{}
	_, err := NewProcessor(r, o).Process(t.Context(), "owner", 1, 30)
	if err == nil || errors.Is(err, ErrLostClaim) || o.once != 0 || r.finished != 0 {
		t.Fatal("partial receipt reached cleanup")
	}
}

// delayedFileObjects expires caller credit during HEAD while returning a superficially valid receipt.
// RU: delayedFileObjects завершает caller credit во время HEAD, возвращая внешне допустимый receipt.
type delayedFileObjects struct {
	fileObjectsFixture
	cancel context.CancelFunc
}

// Head deliberately ignores cancellation until after its receipt to exercise the post-IO boundary.
// RU: Head намеренно игнорирует отмену до возврата receipt для проверки границы после IO.
func (o *delayedFileObjects) Head(ctx context.Context, key, version string) (objectstorage.Receipt, error) {
	o.cancel()
	return o.fileObjectsFixture.Head(ctx, key, version)
}

// TestFileCleanupExpiredHeadCannotIssueDelete preserves the original claim ceiling after a delayed response.
// RU: TestFileCleanupExpiredHeadCannotIssueDelete сохраняет исходный ceiling claim после задержанного ответа.
func TestFileCleanupExpiredHeadCannotIssueDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	receipt := objectstorage.Receipt{Key: "key", VersionID: "v1", ETag: "etag", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
	r := &fileRepositoryFixture{repositoryFixture: repositoryFixture{claim: Claim{SourceKind: kubernetesFileSource, LedgerID: "ledger", ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID, ObjectETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: 3, Generation: 1}}}
	o := &delayedFileObjects{fileObjectsFixture: fileObjectsFixture{objectsFixture: objectsFixture{receipt: receipt}}, cancel: cancel}
	n, err := NewProcessor(r, o).Process(ctx, "owner", 1, 30)
	if n != 0 || !errors.Is(err, context.Canceled) || o.once != 0 || r.records != 0 || r.finished != 0 {
		t.Fatal("delayed HEAD reminted deletion credit")
	}
}

// unqualifiedFileObjects hides the verifier while retaining ordinary HEAD and single-attempt transport.
// RU: unqualifiedFileObjects скрывает verifier, сохраняя обычный HEAD и single-attempt transport.
type unqualifiedFileObjects struct {
	ObjectStore
	objectstorage.SingleAttemptDeleter
}

// TestFileCleanupGenericAbsenceIsUnknown refuses generic404 and denied reads after uncertain deletion.
// RU: TestFileCleanupGenericAbsenceIsUnknown отклоняет generic404 и denied read после неизвестного удаления.
func TestFileCleanupGenericAbsenceIsUnknown(t *testing.T) {
	for _, headErr := range []error{objectstorage.ErrNotFound, objectstorage.ErrUnavailable} {
		receipt := objectstorage.Receipt{Key: "key", VersionID: "v1", ETag: "etag", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3}
		r := &fileRepositoryFixture{repositoryFixture: repositoryFixture{claim: Claim{SourceKind: kubernetesFileSource, LedgerID: "ledger", ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID, ObjectETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: 3, Generation: 2, Uncertain: true}}}
		raw := &fileObjectsFixture{objectsFixture: objectsFixture{receipt: receipt, headErr: headErr}}
		objects := unqualifiedFileObjects{ObjectStore: raw, SingleAttemptDeleter: raw}
		n, err := NewProcessor(r, objects).Process(t.Context(), "successor", 1, 30)
		if n != 0 || err == nil || raw.once != 0 || r.success {
			t.Fatal("ambiguous absence completed FILE cleanup")
		}
	}
}
