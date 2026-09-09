package readiness

import (
	"errors"
	"fmt"
	"testing"
)

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
