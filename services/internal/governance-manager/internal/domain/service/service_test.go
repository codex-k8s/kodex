package service

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

func TestBacklogOperationRecordsOperationAndReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := New(repository)
	err := service.BacklogOperation(context.Background(), BacklogOperationInput{Operation: enum.OperationEvaluateRisk})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("BacklogOperation() error = %v, want ErrNotImplemented", err)
	}
	if repository.recorded != enum.OperationEvaluateRisk {
		t.Fatalf("recorded operation = %q, want %q", repository.recorded, enum.OperationEvaluateRisk)
	}
}

func TestReadyRequiresRepository(t *testing.T) {
	t.Parallel()

	if New(nil).Ready() {
		t.Fatal("Ready() = true for missing repository, want false")
	}
	if !New(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = false for ready repository, want true")
	}
}

type fakeRepository struct {
	ready    bool
	recorded enum.Operation
}

func (repository *fakeRepository) Ready() bool {
	return repository.ready
}

func (repository *fakeRepository) RecordBacklogOperation(_ context.Context, operation governancerepo.BacklogOperation) error {
	repository.recorded = operation.Operation
	return nil
}
