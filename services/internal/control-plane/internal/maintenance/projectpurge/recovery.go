package projectpurge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

// recoverObject validates a bounded exact recovery claim and reserves a safety margin before its lease deadline, executes at most fifteen seconds of storage work, then records the outcome with a separate three-second child context of the caller. This receipt budget does not outlive a cancelled parent and does not authorize more external effects.
// recoverObject проверяет ограниченный точный захват восстановления и оставляет запас до срока аренды, выполняет до пятнадцати секунд работы хранилища и фиксирует результат отдельным трёхсекундным дочерним контекстом вызывающего. Бюджет квитанции не переживает отменённого родителя и не разрешает новые внешние действия.
func (processor *Processor) recoverObject(ctx context.Context, owner string, claim platform.ProjectPurgeRecoveryClaim) error {
	if !objectstorage.ValidKey(claim.Key) || !strings.HasPrefix(claim.Key, "organizations/") ||
		claim.Generation < 1 || claim.SizeBytes < 0 || claim.Deadline.IsZero() || !claim.Deadline.After(time.Now().Add(5*time.Second)) {
		return errors.New("project purge recovery claim is invalid")
	}
	deadline := time.Now().Add(15 * time.Second)
	if leaseDeadline := claim.Deadline.Add(-3 * time.Second); leaseDeadline.Before(deadline) {
		deadline = leaseDeadline
	}
	operation, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	outcome, effectErr := processor.recoverExactObject(operation, owner, claim)
	// Независимый короткий receipt budget не продолжает внешние effects.
	receiptCtx, finishCancel := context.WithTimeout(ctx, 3*time.Second)
	defer finishCancel()
	if err := processor.source.FinishProjectPurgeRecovery(receiptCtx, owner, claim, outcome); err != nil {
		return errors.Join(effectErr, err)
	}
	return effectErr
}

// recoverExactObject distinguishes unpinned absence from deletion of an already pinned version, validates exact digest/size/version/ETag, durably pins the receipt before Delete and verifies version absence. A foreign replacement or pin/readback failure yields FAILED without an unproven successful cleanup.
// recoverExactObject различает непривязанное отсутствие и удаление уже привязанной версии, сверяет точные digest/size/version/ETag, устойчиво фиксирует квитанцию до Delete и проверяет отсутствие версии. Чужая замена либо отказ pin/readback дают FAILED без недоказанного успешного завершения очистки.
func (processor *Processor) recoverExactObject(ctx context.Context, owner string, claim platform.ProjectPurgeRecoveryClaim) (string, error) {
	receipt, err := processor.objects.Head(ctx, claim.Key, claim.Version)
	if errors.Is(err, objectstorage.ErrNotFound) {
		if claim.Version != "" && claim.ETag != "" {
			return "DELETED", nil
		}
		return "ABSENT", nil
	}
	if err != nil || receipt.Key != claim.Key || receipt.Digest != claim.Digest || receipt.SizeBytes != claim.SizeBytes || receipt.VersionID == "" || receipt.ETag == "" ||
		claim.Version != "" && (claim.Version != receipt.VersionID || claim.ETag != receipt.ETag) {
		return "FAILED", errors.New("project purge recovery object readback failed")
	}
	// Receipt обязателен до Delete: crash/replacement продолжит exact version,
	// а не примет произвольный поздний объект под тем же ключом.
	if err := processor.source.PinProjectPurgeRecovery(ctx, owner, claim, receipt); err != nil {
		return "FAILED", err
	}
	if ctx.Err() != nil {
		return "FAILED", ctx.Err()
	}
	if err := processor.objects.Delete(ctx, claim.Key, receipt.VersionID); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return "FAILED", errors.New("project purge recovery object deletion failed")
	}
	if _, err := processor.objects.Head(ctx, claim.Key, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		return "FAILED", errors.New("project purge recovery object remains after deletion")
	}
	return "DELETED", nil
}
