package projectpurge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

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
