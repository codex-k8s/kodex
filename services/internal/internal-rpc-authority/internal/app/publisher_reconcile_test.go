package app

import (
	"context"
	"errors"
	"testing"
)

func TestReconcilePublisherSkipsReadbackAfterGraphFailure(t *testing.T) {
	t.Parallel()

	graphFailure := errors.New("graph rejected")
	readbackCalls := 0
	readyCalls := 0
	graphErr, publishErr, readyErr := reconcilePublisher(
		context.Background(),
		func(context.Context) error { return graphFailure },
		func(context.Context) error {
			readbackCalls++
			return nil
		},
		func(context.Context) error {
			readyCalls++
			return nil
		},
	)

	if !errors.Is(graphErr, graphFailure) {
		t.Fatalf("graph error = %v, want %v", graphErr, graphFailure)
	}
	if publishErr != nil {
		t.Fatalf("readback error = %v, want nil", publishErr)
	}
	if readyErr != nil {
		t.Fatalf("readiness error = %v, want nil", readyErr)
	}
	if readbackCalls != 0 {
		t.Fatalf("readback calls = %d, want 0", readbackCalls)
	}
	if readyCalls != 1 {
		t.Fatalf("readiness calls = %d, want 1", readyCalls)
	}
}

func TestReconcilePublisherPublishesReadbackAfterGraphSuccess(t *testing.T) {
	t.Parallel()

	readbackCalls := 0
	graphErr, publishErr, readyErr := reconcilePublisher(
		context.Background(),
		func(context.Context) error { return nil },
		func(context.Context) error {
			readbackCalls++
			return nil
		},
		func(context.Context) error { return nil },
	)

	if graphErr != nil || publishErr != nil || readyErr != nil {
		t.Fatalf(
			"reconcile errors = (%v, %v, %v), want nil",
			graphErr,
			publishErr,
			readyErr,
		)
	}
	if readbackCalls != 1 {
		t.Fatalf("readback calls = %d, want 1", readbackCalls)
	}
}
