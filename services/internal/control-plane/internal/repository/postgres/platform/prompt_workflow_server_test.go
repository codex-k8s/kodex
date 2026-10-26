package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
)

// TestServerWorkflowPromptExceptionKeys bounds manager classification without borrowing legacy grants.
// TestServerWorkflowPromptExceptionKeys ограничивает классификацию менеджера без legacy-полномочий.
func TestServerWorkflowPromptExceptionKeys(t *testing.T) {
	for _, mode := range []string{"", "MANAGER_DRIVEN", workflowservice.ServerDAGV1} {
		version := validWorkflowFixture()
		version.ExecutionMode = mode
		for _, key := range []string{"workflow.coordinator.exception.1", "workflow.coordinator.exception.48", "workflow.coordinator.exception.0", "workflow.coordinator.exception.49", "workflow.coordinator.exception.01", "workflow.coordinator.exception.+1", "workflow.coordinator.exception.1x", "workflow.coordinator.initial", "workflow.coordinator.continue.2"} {
			step, ok := promptWorkflowStep(version, key)
			exception := key == "workflow.coordinator.exception.1" || key == "workflow.coordinator.exception.48"
			legacy := key == "workflow.coordinator.initial" || key == "workflow.coordinator.continue.2"
			want := mode == workflowservice.ServerDAGV1 && exception || mode != workflowservice.ServerDAGV1 && legacy
			if ok != want {
				t.Fatalf("unexpected prompt stage eligibility: mode=%s key=%s", mode, key)
			}
			if ok && exception && (step.AgentRef != version.CoordinatorAgentRef || !reflect.DeepEqual(step.RequiredCapabilityKeys, []string{runtimecontract.ArtifactCapability})) {
				t.Fatal("exception analysis inherited an orchestration permission")
			}
		}
		if _, ok := promptWorkflowStep(version, version.Steps[0].Key); !ok {
			t.Fatal("published business stage became unavailable")
		}
	}
}

// TestServerWorkflowPromptGuidance keeps stage continuation distinct from exception analysis.
// TestServerWorkflowPromptGuidance отделяет продолжение шага от разбора исключения.
func TestServerWorkflowPromptGuidance(t *testing.T) {
	for _, test := range []struct {
		key, mode string
		continued bool
		valid     bool
	}{
		{"step-001", "SERVER_ASSIGNED_STAGE", false, true},
		{"step-001", "SERVER_STAGE_CONTINUATION", true, true},
		{"workflow.coordinator.exception.1", "SERVER_EXCEPTION_ANALYSIS", false, true},
		{"workflow.coordinator.exception.1", "", true, false},
		{"workflow.coordinator.exception.01", "", false, false},
		{"workflow.coordinator.initial", "", false, false},
		{"workflow.coordinator.continue.2", "", false, false},
	} {
		t.Run(test.key+test.mode, func(t *testing.T) {
			snapshot, execution := workflowExecutionFixture()
			execution.ExecutionMode = workflowservice.ServerDAGV1
			execution.RunRef, snapshot.RunRef = "run_assigned_child", "run_assigned_child"
			execution.WorkflowStageKey, snapshot.ContextPin.WorkflowStageKey = test.key, test.key
			err := applyWorkflowExecutionPrompt(&snapshot, execution, test.continued)
			if !test.valid {
				if !errors.Is(err, errs.ErrConflict) {
					t.Fatal("invalid server stage lineage did not fail closed")
				}
				return
			}
			guidance := snapshot.SemanticValues["CONSTRAINTS"]
			if err != nil || !strings.Contains(guidance, `"Mode":"`+test.mode+`"`) || !strings.Contains(guidance, serverWorkflowExecutionGuidance) || strings.Contains(guidance, workflowExecutionGuidance) {
				t.Fatal("server prompt reused legacy coordinator instructions")
			}
		})
	}
}
