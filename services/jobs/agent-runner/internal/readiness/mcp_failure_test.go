package readiness

import (
	"errors"
	"fmt"
	"testing"
)

// TestMCPFailureStageIsClosedAndKeepsInnerStage checks all nine startup phases survive wrapping with causal identity while private/unknown errors fall back and success remains nil.
// TestMCPFailureStageIsClosedAndKeepsInnerStage проверяет сохранение всех девяти фаз подготовки при оборачивании вместе с идентичностью причины, общий результат для частных и неизвестных ошибок и nil при успехе.
func TestMCPFailureStageIsClosedAndKeepsInnerStage(t *testing.T) {
	for _, stage := range []mcpStage{mcpStageConfiguration, mcpStageTLSIdentity, mcpStageSocket, mcpStageCallbackAuthority, mcpStageInitialize, mcpStageInitialized, mcpStageCatalogTransport, mcpStageCatalogSchema, mcpStageCatalogBinding} {
		cause := errors.New("private fixture payload")
		err := withMCPStage(withMCPStage(cause, stage), mcpStageConfiguration)
		if FailureStage(fmt.Errorf("wrapper: %w", err)) != string(stage) || !errors.Is(err, cause) {
			t.Fatal("closed stage or causal error lost")
		}
	}
	for _, err := range []error{nil, errors.New("private fixture payload"), &mcpStartupError{stage: "private fixture payload", cause: errors.New("private")}} {
		if FailureStage(err) != "UNKNOWN" {
			t.Fatal("untrusted text became stage")
		}
	}
	if withMCPStage(nil, mcpStageInitialize) != nil {
		t.Fatal("success became failure")
	}
}
