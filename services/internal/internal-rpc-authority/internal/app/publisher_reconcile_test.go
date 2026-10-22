package app

import (
	"context"
	"errors"
	"testing"
)

// TestReconcilePublisherSkipsReadbackAfterGraphFailure injects a graph-publication error and verifies it is returned, readback is never called and readiness is still checked once. All callbacks are in-memory fixtures; no graph, Secret or database is published.
// TestReconcilePublisherSkipsReadbackAfterGraphFailure подставляет ошибку публикации графа и проверяет её возврат, отсутствие вызова readback и одну проверку готовности. Все обработчики — эталоны в памяти; граф, Secret и БД не публикуются.
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

// TestReconcilePublisherPublishesReadbackAfterGraphSuccess supplies successful synthetic graph/readback/readiness callbacks and requires one readback call with no returned errors. It covers the local reconciliation branch rather than real publication or transport readiness.
// TestReconcilePublisherPublishesReadbackAfterGraphSuccess задаёт успешные синтетические обработчики графа, readback и готовности и требует один вызов readback без возвращённых ошибок. Проверяется локальная ветвь сопоставления, а не реальная публикация или готовность транспорта.
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
