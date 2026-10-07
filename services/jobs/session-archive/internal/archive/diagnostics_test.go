package archive

import (
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func TestFailureStageRequiresErrorIdentity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		cause error
		stage model.FailureStage
	}{
		{errSourceIdentity, model.FailureStageSourceIdentity},
		{errSourceDigest, model.FailureStageSourceDigest},
		{errObjectWrite, model.FailureStageObjectWrite},
		{errObjectReadback, model.FailureStageObjectReadback},
		{errObjectReadbackMismatch, model.FailureStageObjectReadback},
	} {
		if ClassifyFailureStage(fmt.Errorf("SENTINEL_PRIVATE: %w", scenario.cause)) != scenario.stage {
			t.Fatal("wrapped archive error lost its closed stage")
		}
		if ClassifyFailureStage(errors.New(scenario.cause.Error())) != model.FailureStageUnknown {
			t.Fatal("error text forged an archive failure stage")
		}
	}
	if ClassifyFailureStage(nil) != model.FailureStageUnknown {
		t.Fatal("missing error acquired a failure stage")
	}
}
