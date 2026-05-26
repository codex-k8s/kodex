package service

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
)

func TestServiceBacklogOperationsReturnUnimplemented(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	svc := New(repository)

	err := svc.RequestFeedback(context.Background())
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("RequestFeedback() err = %v, want ErrNotImplemented", err)
	}
	if len(repository.operations) != 1 || repository.operations[0] != enum.OperationRequestFeedback {
		t.Fatalf("operations = %v, want RequestFeedback", repository.operations)
	}
}

func TestServiceReadinessDependsOnRepository(t *testing.T) {
	t.Parallel()

	if New(&fakeRepository{ready: true}).Ready() != true {
		t.Fatal("Ready() = false, want true")
	}
	if New(&fakeRepository{}).Ready() != false {
		t.Fatal("Ready() = true, want false")
	}
}

func TestServiceBacklogRequiresReadyRepository(t *testing.T) {
	t.Parallel()

	err := New(&fakeRepository{}).RequestApproval(context.Background())
	if !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("RequestApproval() err = %v, want ErrUnavailable", err)
	}
}

type fakeRepository struct {
	ready      bool
	operations []enum.Operation
}

func (r *fakeRepository) Ready() bool {
	return r.ready
}

func (r *fakeRepository) RecordBacklogOperation(_ context.Context, operation enum.Operation) error {
	r.operations = append(r.operations, operation)
	return nil
}
