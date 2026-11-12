package providercredential

import (
	"errors"
	"strings"
	"testing"
)

// TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage checks fixed public error text, preservation of
// the precise inner stage and unknown fallback for arbitrary private stage strings.
// TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage проверяет фиксированный публичный error текст,
// сохранение точного внутреннего stage и unknown fallback произвольных private stage строк.
func TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage(t *testing.T) {
	err := atCatalogStage(catalogStageCredential, errors.New("synthetic-private-payload"))
	if strings.Contains(err.Error(), "synthetic") {
		t.Fatal("raw diagnostic exposed")
	}
	result, _ := catalogFailure(t.Context(), atCatalogStage(catalogStageResult, err))
	if result.DiagnosticStage() != string(catalogStageCredential) {
		t.Fatal("precise stage lost")
	}
	result.stage = "synthetic-private-payload"
	if result.DiagnosticStage() != "unknown" {
		t.Fatal("open diagnostic stage")
	}
}
