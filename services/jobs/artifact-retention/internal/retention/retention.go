// Package retention исполняет fenced lifecycle необратимого удаления artifacts.
package retention

import (
	"context"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

var ErrLostClaim = errors.New("artifact retention claim is lost")

type Claim struct {
	ArtifactID, ArtifactRef, ObjectKey, ObjectVersion string
	Generation                                        int64
}

type Repository interface {
	Claim(context.Context, string, int, int64) ([]Claim, error)
	Finalize(context.Context, Claim, string) error
}

type ObjectStore interface {
	Delete(context.Context, string, string) error
	Head(context.Context, string, string) (objectstorage.Receipt, error)
}

type Processor struct {
	repository Repository
	objects    ObjectStore
}

func NewProcessor(repository Repository, objects ObjectStore) *Processor {
	return &Processor{repository: repository, objects: objects}
}

func (processor *Processor) Process(
	ctx context.Context,
	owner string,
	batchSize int,
	leaseSeconds int64,
) (int, error) {
	claims, err := processor.repository.Claim(ctx, owner, batchSize, leaseSeconds)
	if err != nil {
		return 0, err
	}
	processed := 0
	var resultErr error
	for _, claim := range claims {
		if claim.ArtifactID == "" || claim.ObjectKey == "" || claim.ObjectVersion == "" || claim.Generation < 1 {
			resultErr = errors.Join(resultErr, errors.New("artifact retention claim is incomplete"))
			continue
		}
		if err := processor.objects.Delete(ctx, claim.ObjectKey, claim.ObjectVersion); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
			resultErr = errors.Join(resultErr, fmt.Errorf("delete claimed artifact object version: %w", err))
			continue
		}
		if _, err := processor.objects.Head(ctx, claim.ObjectKey, claim.ObjectVersion); err == nil {
			resultErr = errors.Join(resultErr, errors.New("deleted artifact object version is still present"))
			continue
		} else if !errors.Is(err, objectstorage.ErrNotFound) {
			resultErr = errors.Join(resultErr, fmt.Errorf("read back deleted artifact object version: %w", err))
			continue
		}
		if err := processor.repository.Finalize(ctx, claim, owner); err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		processed++
	}
	return processed, resultErr
}
