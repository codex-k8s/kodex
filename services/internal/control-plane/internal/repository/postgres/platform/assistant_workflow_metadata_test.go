package platform

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantWorkflowMetadataPublishedProjection compares a synthetic published graph projection
// byte-for-byte with the shared golden, checks field/step shape and rejects private
// sentinels/keys. Removing Published must yield revision0 with no draft steps. It reads a
// repository fixture only, not owner ACL, broker delivery or a live workflow.
// TestAssistantWorkflowMetadataPublishedProjection сравнивает synthetic published graph projection
// byte-for-byte с shared golden, проверяет field/step shape и отсутствие private sentinels/keys.
// Без Published требуется revision0 без draft steps. Читает лишь repository fixture, а не owner
// ACL, broker delivery и live workflow.
func TestAssistantWorkflowMetadataPublishedProjection(t *testing.T) {
	p := entity.WorkflowVersion{Ref: "wfv_fixture123", Name: "Процесс", CoordinatorAgentRef: "agt_manager123", VersionNumber: 2, Concurrency: 3, TimeoutSeconds: 3600, Instructions: "PRIVATE_SENTINEL", CompletionCriteria: "PRIVATE_SENTINEL", Inputs: []entity.WorkflowInputField{{Key: "private", Label: "Private", Type: "TEXT", DefaultValue: "PRIVATE_SENTINEL"}}, ResultSchema: map[string]any{"private": "PRIVATE_SENTINEL"}}
	for i, s := range []struct{ key, name, agent string }{{"implementation", "Разработка", "agt_developer123"}, {"review", "Комплексное ревью", "agt_reviewer123"}, {"acceptance", "Приёмка", "agt_manager123"}} {
		step := entity.WorkflowStep{Key: s.key, Name: s.name, AgentRef: s.agent, Position: int32(i + 1), TimeoutSeconds: 900, Instructions: "PRIVATE_SENTINEL", ExpectedResult: "PRIVATE_SENTINEL", RequiredCapabilityKeys: []string{"platform.artifact.manage"}}
		if i > 0 {
			step.DependsOn = []string{p.Steps[i-1].Key}
		}
		if i == 2 {
			step.HumanGateAfter = true
			step.GateDecisions = []string{"APPROVE", "REJECT"}
		}
		p.Steps = append(p.Steps, step)
	}
	w := entity.Workflow{Ref: "wfl_fixture123", ProjectRef: "prj_fixture123", Version: 7, Name: "Процесс", State: "PUBLISHED", Published: &p, Draft: &entity.WorkflowVersion{Name: "PRIVATE_SENTINEL"}}
	raw, err := assistantWorkflowMetadataJSON(w)
	if err != nil || bytes.Contains(raw, []byte("PRIVATE_SENTINEL")) {
		t.Fatal("published projection leaked private source", err)
	}
	golden, err := os.ReadFile("../../../../../../../contracts/proto/testdata/assistant_workflow_metadata.json")
	if err != nil || !bytes.Equal(raw, bytes.TrimSuffix(golden, []byte("\n"))) {
		t.Fatal("producer differs from shared metadata contract", err)
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 11 || len(snapshot["steps"].([]any)) != 3 {
		t.Fatal("metadata contract incomplete")
	}
	for _, forbidden := range []string{"instructions", "draft", "input_fields", "completion_criteria", "required_capability_keys"} {
		if strings.Contains(string(raw), "\""+forbidden+"\"") {
			t.Fatal("private field exposed", forbidden)
		}
	}
	w.Published = nil
	w.State = "DRAFT"
	raw, err = assistantWorkflowMetadataJSON(w)
	if err != nil || json.Unmarshal(raw, &snapshot) != nil || snapshot["published_revision"] != float64(0) || len(snapshot["steps"].([]any)) != 0 {
		t.Fatal("unpublished draft mistaken for published graph", err)
	}
}
