package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

func TestWorkspaceCompletionDiagnosticIsClosedAndRedacted(t *testing.T) {
	for _, reason := range []string{runtimecontract.RuntimeWorkspaceReadOnly,
		runtimecontract.RuntimeWorkspaceQuotaExceeded, runtimecontract.RuntimeWorkspacePathOutsideWorkspace,
		runtimecontract.RuntimeWorkspaceIOError, "PRIVATE_PATH_AND_TOKEN_SENTINEL"} {
		for _, stage := range []string{workspaceCompletionCheck, workspaceCompletionPublish, "PRIVATE_STAGE_SENTINEL"} {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			err := errors.Join(&workspacepolicy.Denial{Reason: reason}, errors.New("PRIVATE_CAUSE_SENTINEL"))
			logWorkspaceCompletionDenial(t.Context(), logger, stage, err)
			if strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "PRIVATE_") {
				t.Fatal("workspace diagnostic disclosed input or emitted duplicate records")
			}
			var record map[string]any
			if json.Unmarshal(output.Bytes(), &record) != nil || record["msg"] != workspaceCompletionMessage || record["level"] != "WARN" {
				t.Fatal("workspace diagnostic lost its structured runtime logger")
			}
			wantReason, wantStage := reason, stage
			if strings.HasPrefix(reason, "PRIVATE_") {
				wantReason = runtimecontract.RuntimeWorkspaceIOError
			}
			if strings.HasPrefix(stage, "PRIVATE_") {
				wantStage = "UNKNOWN"
			}
			if record[workspaceCompletionStage] != wantStage || record[workspaceCompletionReason] != wantReason {
				t.Fatal("workspace diagnostic lost its closed stage or denial reason")
			}
		}
	}
	logWorkspaceCompletionDenial(t.Context(), nil, workspaceCompletionCheck, errors.New("PRIVATE_CAUSE_SENTINEL"))
}
