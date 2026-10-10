// Package prepared исполняет ограниченную очистку непринятого контента CP.
package prepared

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

var ErrLostClaim = errors.New("prepared content cleanup claim is lost")

type Claim struct {
	LedgerID, ObjectKey, ObjectVersion, ObjectETag, Digest string
	SizeBytes, Generation                                  int64
	Uncertain                                              bool
}

type Repository interface {
	ClaimPrepared(context.Context, string, int, int64) ([]Claim, error)
	FinishPrepared(context.Context, Claim, string, bool, objectstorage.Receipt) error
	RecordPreparedReceipt(context.Context, Claim, string, objectstorage.Receipt) error
}

type ObjectStore interface {
	Head(context.Context, string, string) (objectstorage.Receipt, error)
	Delete(context.Context, string, string) error
}

type Processor struct {
	repository Repository
	objects    ObjectStore
}

func NewProcessor(repository Repository, objects ObjectStore) *Processor {
	return &Processor{repository: repository, objects: objects}
}

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

func exactReceipt(claim Claim, receipt objectstorage.Receipt) bool {
	return receipt.Key == claim.ObjectKey && receipt.VersionID != "" && receipt.ETag != "" &&
		receipt.Digest == claim.Digest && receipt.SizeBytes == claim.SizeBytes
}
