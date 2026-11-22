package prepared

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// kubernetesFileSource selects the owner ledger for one normalized immutable FILE object.
// RU: kubernetesFileSource выбирает owner ledger единственного неизменяемого нормализованного FILE-объекта.
const kubernetesFileSource = "KUBERNETES_FILE_SOURCE"

// processFile consumes fresh delete admission once; later claims only reconcile the exact version.
// An uncertain PUT needs a positive receipt and a newly committed admission before its first Delete.
// RU: processFile однократно использует свежий допуск удаления; следующие claims лишь сверяют точную версию.
// Неизвестный PUT требует положительного receipt и нового зафиксированного допуска перед первым Delete.
func (processor *Processor) processFile(ctx context.Context, claim Claim, owner string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	receipt := objectstorage.Receipt{Key: claim.ObjectKey, VersionID: claim.ObjectVersion, ETag: claim.ObjectETag, Digest: claim.Digest, SizeBytes: claim.SizeBytes}
	known := receipt.VersionID != "" && receipt.ETag != ""
	headOnly := claim.Uncertain && known
	success := false
	head, outcome := processor.objects.Head(ctx, receipt.Key, receipt.VersionID)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if known && errors.Is(outcome, objectstorage.ErrNotFound) {
		outcome = objectstorage.VerifyVersionAbsent(ctx, processor.objects, receipt.Key, receipt.VersionID)
		success = outcome == nil
	} else if outcome == nil && (!exactReceipt(claim, head) || known && head != receipt) {
		outcome = errors.New("FILE cleanup receipt mismatch")
	} else if outcome == nil {
		receipt = head
		if headOnly {
			outcome = errors.New("FILE deletion outcome remains unknown")
		} else {
			deleter, supported := processor.objects.(objectstorage.SingleAttemptDeleter)
			if !supported {
				outcome = errors.New("FILE single-attempt deletion is unavailable")
			} else if outcome = deleter.CheckDeleteOnce(ctx); outcome == nil {
				if !known {
					// Потерянный Record ACK закрывает этот вызов до внешнего эффекта.
					if err := processor.repository.RecordPreparedReceipt(ctx, claim, owner, receipt); err != nil {
						return false, err
					}
				}
				if err := ctx.Err(); err != nil {
					return false, err
				}
				outcome = deleter.DeleteOnce(ctx, receipt.Key, receipt.VersionID)
				if outcome == nil {
					_, outcome = processor.objects.Head(ctx, receipt.Key, receipt.VersionID)
					if errors.Is(outcome, objectstorage.ErrNotFound) {
						success, outcome = true, nil
					} else if outcome == nil {
						outcome = errors.New("FILE deletion is not confirmed")
					}
				}
			}
		}
	}
	if err := processor.repository.FinishPrepared(ctx, claim, owner, success, receipt); err != nil {
		return false, errors.Join(outcome, err)
	}
	return success, outcome
}
