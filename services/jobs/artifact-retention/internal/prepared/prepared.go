// Package prepared cleans unadmitted control-plane content under exact ledger claims.
// RU: Package prepared очищает непринятый контент control plane по точным claim записям ledger.
package prepared

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// ErrLostClaim signals that a fenced cleanup claim expired before an external delete.
// RU: ErrLostClaim означает, что ограниченный claim очистки утрачен до внешнего удаления.
var ErrLostClaim = errors.New("prepared content cleanup claim is lost")

// Claim pins one cleanup attempt to a ledger row, object receipt and fencing generation.
// RU: Claim привязывает попытку очистки к записи ledger, receipt объекта и поколению fencing.
type Claim struct {
	LedgerID, ObjectKey, ObjectVersion, ObjectETag, Digest string
	SizeBytes, Generation                                  int64
	Uncertain                                              bool
}

// Repository owns claims, receipt recording and terminal cleanup decisions in the ledger.
// RU: Repository владеет claim, записью receipt и конечным решением об очистке в ledger.
type Repository interface {
	// ClaimPrepared leases a bounded batch of prepared objects to one owner.
	// RU: ClaimPrepared выдаёт одному владельцу ограниченную пачку prepared-объектов.
	ClaimPrepared(context.Context, string, int, int64) ([]Claim, error)
	// FinishPrepared records the fenced outcome after exact object verification or deletion.
	// RU: FinishPrepared фиксирует защищённый поколением исход после точной проверки или удаления объекта.
	FinishPrepared(context.Context, Claim, string, bool, objectstorage.Receipt) error
	// RecordPreparedReceipt fences the exact receipt before any external delete occurs.
	// RU: RecordPreparedReceipt закрепляет точный receipt до любого внешнего удаления.
	RecordPreparedReceipt(context.Context, Claim, string, objectstorage.Receipt) error
}

// ObjectStore exposes only exact-version inspection and deletion for cleanup.
// RU: ObjectStore предоставляет очистке только проверку и удаление точной версии объекта.
type ObjectStore interface {
	// Head reads the specified object version and its immutable receipt.
	// RU: Head читает заданную версию объекта и её неизменяемый receipt.
	Head(context.Context, string, string) (objectstorage.Receipt, error)
	// Delete removes only the specified object version after ledger fencing.
	// RU: Delete удаляет только заданную версию объекта после фиксации в ledger.
	Delete(context.Context, string, string) error
}

// Processor coordinates ledger-fenced verification and object deletion without owning storage state.
// RU: Processor координирует проверку и удаление объекта под ledger-fence, не владея состоянием хранилища.
type Processor struct {
	repository Repository
	objects    ObjectStore
}

// NewProcessor binds the authoritative ledger repository and exact-version object store.
// RU: NewProcessor связывает авторитетный ledger repository и хранилище точных версий объектов.
func NewProcessor(repository Repository, objects ObjectStore) *Processor {
	return &Processor{repository: repository, objects: objects}
}

// Process handles a bounded claimed batch: uncertain writes require a positive exact receipt,
// and deletion counts only after a fenced receipt and confirmed absence of that version.
// RU: Process обрабатывает ограниченную пачку claim: неизвестная запись требует положительного
// точного receipt, а удаление засчитывается лишь после его фиксации и подтверждения отсутствия версии.
func (processor *Processor) Process(ctx context.Context, owner string, batch int, lease int64) (int, error) {
	claims, err := processor.repository.ClaimPrepared(ctx, owner, batch, lease)
	if err != nil {
		return 0, err
	}
	processed := 0
	var resultErr error
	for _, claim := range claims {
		if claim.LedgerID == "" || claim.Generation < 1 || !objectstorage.ValidKey(claim.ObjectKey) ||
			!objectstorage.ValidDigest(claim.Digest) || claim.SizeBytes < 0 || claim.SizeBytes > 1<<20 ||
			(!claim.Uncertain && (claim.ObjectVersion == "" || claim.ObjectETag == "")) {
			resultErr = errors.Join(resultErr, errors.New("prepared cleanup claim is invalid"))
			continue
		}
		receipt := objectstorage.Receipt{Key: claim.ObjectKey, VersionID: claim.ObjectVersion, ETag: claim.ObjectETag, Digest: claim.Digest, SizeBytes: claim.SizeBytes}
		success := false
		head, headErr := processor.objects.Head(ctx, claim.ObjectKey, claim.ObjectVersion)
		if claim.Uncertain {
			// HEAD404 не доказывает завершение единственного позднего Put. Ledger
			// остаётся UNKNOWN/WAITING_OWNER до положительного точного receipt.
			if headErr == nil && exactReceipt(claim, head) {
				receipt = head
			} else {
				headErr = errors.New("prepared write outcome remains unknown")
			}
		} else if errors.Is(headErr, objectstorage.ErrNotFound) {
			// Для ранее принятого immutable receipt отсутствие exact version
			// доказывает уже выполненное удаление после потери cleanup ACK.
			success = true
			headErr = nil
		} else if headErr == nil && head != receipt {
			headErr = errors.New("prepared cleanup receipt mismatch")
		}
		if headErr == nil && !success {
			if err := processor.repository.RecordPreparedReceipt(ctx, claim, owner, receipt); err != nil {
				resultErr = errors.Join(resultErr, err)
				continue
			}
			deleteErr := processor.objects.Delete(ctx, receipt.Key, receipt.VersionID)
			if deleteErr == nil || errors.Is(deleteErr, objectstorage.ErrNotFound) {
				_, readErr := processor.objects.Head(ctx, receipt.Key, receipt.VersionID)
				if errors.Is(readErr, objectstorage.ErrNotFound) {
					success = true
				} else {
					headErr = errors.New("prepared deletion is not confirmed")
				}
			} else {
				headErr = errors.New("prepared deletion failed")
			}
		}
		if finishErr := processor.repository.FinishPrepared(ctx, claim, owner, success, receipt); finishErr != nil {
			resultErr = errors.Join(resultErr, finishErr)
			continue
		}
		if success {
			processed++
		} else {
			resultErr = errors.Join(resultErr, headErr)
		}
	}
	return processed, resultErr
}

// exactReceipt matches an uncertain write to its key, nonempty version, ETag, digest and size.
// RU: exactReceipt сопоставляет неизвестную запись с ключом, непустой версией, ETag, дайджестом и размером.
func exactReceipt(claim Claim, receipt objectstorage.Receipt) bool {
	return receipt.Key == claim.ObjectKey && receipt.VersionID != "" && receipt.ETag != "" &&
		receipt.Digest == claim.Digest && receipt.SizeBytes == claim.SizeBytes
}
