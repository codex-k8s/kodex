package providercredential

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestCatalogCleanupPreservesPrimaryFailureAndRejectsSuccess preserves classified primary failures and
// cancellation while turning apparent success into metadata-only unavailability when cleanup fails.
// TestCatalogCleanupPreservesPrimaryFailureAndRejectsSuccess сохраняет классифицированные первичные
// отказы и отмену, заменяя видимый успех на metadata-only недоступность при ошибке cleanup.
func TestCatalogCleanupPreservesPrimaryFailureAndRejectsSuccess(t *testing.T) {
	cleanup := errors.New("synthetic-private-cleanup")
	for _, failure := range []ModelCatalogFailure{CatalogFailureAuthorization, CatalogFailureUnverified, CatalogFailureUnavailable} {
		primary := ModelCatalog{Failure: failure, stage: catalogStageListCall}
		result, err := catalogCleanupFailure(t.Context(), primary, nil, cleanup)
		if err != nil || result.Failure != failure || result.DiagnosticStage() != string(catalogStageListCall) {
			t.Fatal("cleanup masked primary failure")
		}
	}
	for _, success := range []ModelCatalog{{}, {Failure: CatalogFailureNone, Source: CatalogRemoteAPI, Models: []CatalogModel{{ID: "fixture-model"}}}} {
		result, err := catalogCleanupFailure(t.Context(), success, nil, cleanup)
		if err != nil || result.Failure != CatalogFailureUnavailable || result.DiagnosticStage() != string(catalogStageCleanup) || len(result.Models) != 0 || result.Source != "" {
			t.Fatal("cleanup failure allowed a successful catalog")
		}
	}
	_, err := catalogCleanupFailure(t.Context(), ModelCatalog{}, context.Canceled, cleanup)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cleanup masked cancellation")
	}
}

// TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage checks fixed public error text, preservation of
// the precise inner stage and unknown fallback for arbitrary private stage strings.
// TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage проверяет фиксированный публичный error текст,
// сохранение точного внутреннего stage и unknown fallback произвольных private stage строк.
func TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage(t *testing.T) {
	err := atCatalogStage(catalogStageCacheRead, errors.New("synthetic-private-payload"))
	if strings.Contains(err.Error(), "synthetic") {
		t.Fatal("raw diagnostic exposed")
	}
	result, _ := catalogFailure(t.Context(), atCatalogStage(catalogStageResult, err))
	if result.DiagnosticStage() != string(catalogStageCacheRead) {
		t.Fatal("precise stage lost")
	}
	result.stage = "synthetic-private-payload"
	if result.DiagnosticStage() != "unknown" {
		t.Fatal("open diagnostic stage")
	}
}
