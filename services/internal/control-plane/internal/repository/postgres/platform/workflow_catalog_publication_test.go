package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestExecutionServerWorkflowPublicationPreservesPolicy exercises the real owner producer and
// shared consumer with DAG requirements and the existing gate deadline, without a database fixture.
// TestExecutionServerWorkflowPublicationPreservesPolicy проверяет настоящий owner producer и
// общий consumer с условиями DAG и прежним сроком согласования без fixture базы данных.
func TestExecutionServerWorkflowPublicationPreservesPolicy(t *testing.T) {
	version := validWorkflowFixture()
	version.Ref, version.CoordinatorAgentRef = "wfv_workflow01", "agt_coordinator"
	version.ExecutionMode = workflowservice.ServerDAGV1
	version.Steps[0].AgentRef, version.Steps[1].AgentRef = "agt_developer", "agt_reviewer01"
	version.Steps[0].ResultRequirements = []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo"}}
	version.Steps[1].ResultRequirements = []entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "step-001"}}
	ttl := int32(1800)
	version.Steps[1].ApprovalTimeoutSeconds = &ttl
	item := entity.Workflow{Ref: "wfl_workflow01", ProjectRef: "prj_project001", Version: 7, Published: &version}
	raw, digest, err := executionWorkflowPublication(item, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	value, err := controlplaneapi.DecodeWorkflowPublication(raw, digest)
	if err != nil || value.Version != 2 || value.ExecutionMode != workflowservice.ServerDAGV1 || len(value.Steps[0].ResultRequirements) != 1 || value.Steps[0].ResultRequirements[0].Repository != "codex-k8s/test-repo" || len(value.Steps[1].DependsOn) != 1 || value.Steps[1].DependsOn[0] != "step-001" || value.Steps[1].ApprovalTimeoutSeconds == nil || *value.Steps[1].ApprovalTimeoutSeconds != ttl {
		t.Fatal("server publication lost exact result or gate policy")
	}
	version.Steps[1].AgentRef = version.Steps[0].AgentRef
	if _, _, err := executionWorkflowPublication(item, strings.Repeat("a", 64)); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("self review passed the owner predicate")
	}
	version.Steps[1].AgentRef = "agt_reviewer01"
	version.ExecutionMode = "MANAGER_DRIVEN"
	if _, _, err := executionWorkflowPublication(item, strings.Repeat("a", 64)); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("server result contract silently used with legacy orchestration")
	}
}

func TestExecutionWorkflowPublicationOwnerDecoder(t *testing.T) {
	item := entity.Workflow{Ref: "wfl_workflow01", ProjectRef: "prj_project001", Version: 7, Published: &entity.WorkflowVersion{Ref: "wfv_workflow01", VersionNumber: 3, Name: "Процесс", Purpose: "Цель", CoordinatorAgentRef: "agt_coordinator", Instructions: "Полная инструкция", CompletionCriteria: "Проверено", Concurrency: 1, TimeoutSeconds: 3600, Inputs: []entity.WorkflowInputField{{Key: "field-001", Label: "Задача", Type: "LONG_TEXT", DefaultValue: "Значение", Required: true}}, Steps: []entity.WorkflowStep{{Key: "step-001", Position: 1, Name: "INTAKE", AgentRef: "agt_specialist", Instructions: "Собрать план", ExpectedResult: "manager-plan.md", TimeoutSeconds: 900, RequiredCapabilityKeys: []string{"platform.run.launch"}}}, ResultSchema: map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}}}}}
	raw, digest, err := executionWorkflowPublication(item, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	value, err := controlplaneapi.DecodeWorkflowPublication(raw, digest)
	if err != nil || value.Instructions != item.Published.Instructions || value.InputFields[0].DefaultValue != "Значение" || value.Steps[0].RequiredCapabilityKeys[0] != "platform.run.launch" || value.PublishedVersion != 3 || value.WorkflowVersion != 7 {
		t.Fatal("full owner projection lost fields")
	}
	item.Published.ResultSchema = map[string]any{"credential": "CANARY"}
	if _, _, err := executionWorkflowPublication(item, strings.Repeat("a", 64)); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("unsafe schema silently omitted")
	}
	item.Published.ResultSchema = map[string]any{}
	item.Published.Steps[0].DependsOn = []string{"step-001"}
	if _, _, err := executionWorkflowPublication(item, strings.Repeat("a", 64)); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("owner predicate bypassed")
	}
}
