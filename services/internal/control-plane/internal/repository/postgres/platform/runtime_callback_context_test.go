package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestCallbackContinuationTask(t *testing.T) {
	snapshot := `{"completedChildren":[{"runRef":"run_child","state":"SUCCEEDED","resultSummary":"Verified result","artifactRefs":["art_result"]}],"remainingStepKeys":["step002"]}`
	task, err := callbackContinuationTask([]byte(snapshot))
	if err != nil || !strings.HasSuffix(task, snapshot) || !strings.Contains(task, "untrusted data") {
		t.Fatal("callback did not retain complete owner snapshot as untrusted data")
	}
	for _, input := range []string{`{`, `{"completedChildren":[],"remainingStepKeys":["step002"]}`, `{"completedChildren":[{}]}`, strings.Replace(snapshot, "Verified result", strings.Repeat("x", runtimecontract.MaximumAssistantTurnCodepoints), 1)} {
		if value, err := callbackContinuationTask([]byte(input)); value != "" || !errors.Is(err, errs.ErrConflict) {
			t.Fatal("invalid or oversized callback snapshot was accepted or truncated")
		}
	}
}
