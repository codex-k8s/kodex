package service

import (
	"context"
	"errors"
	"testing"
)

func TestServicePingDelegatesToRepository(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("storage unavailable")
	repository := &fakeRepository{err: expectedErr}
	service := New(repository)

	if err := service.Ping(context.Background()); !errors.Is(err, expectedErr) {
		t.Fatalf("Ping() err = %v, want %v", err, expectedErr)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repository.calls)
	}
}

func TestNewPanicsWithoutRepository(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic")
		}
	}()
	_ = New(nil)
}

type fakeRepository struct {
	err   error
	calls int
}

func (r *fakeRepository) Ping(context.Context) error {
	r.calls++
	return r.err
}
