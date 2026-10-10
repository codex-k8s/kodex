package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

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
