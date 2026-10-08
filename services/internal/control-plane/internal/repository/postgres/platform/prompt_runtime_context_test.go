package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func workflowPromptFixture() entity.PromptMaterializationSnapshot {
	return entity.PromptMaterializationSnapshot{
		ServiceTemplateRevision: promptservice.ServiceTemplateRevision, Locale: "en",
		TemplateRef: "ins_fixture", TemplateDigest: strings.Repeat("a", 64),
		Variables: map[string]string{},
		StructuredVariables: map[string]any{
			"workflow": map[string]any{"files_count": 1},
			"input":    map[string]any{"values": map[string]any{"task_data": "preserved"}},
		},
		UserCapabilities: []string{"platform.run.delegate"}, AgentCapabilities: []string{"platform.run.delegate"},
	}
}

func TestWorkflowPromptPublishedDAGEveryStage(t *testing.T) {
	version := validWorkflowFixture()
	version.Steps[1].Parallel, version.Steps[1].ParallelGroup = true, 2
	for _, key := range []string{"workflow.coordinator.initial", "workflow.coordinator.continue.2", "step-001", "step-002"} {
		t.Run(key, func(t *testing.T) {
			snapshot := workflowPromptFixture()
			step, ok := promptWorkflowStep(version, key)
			if !ok {
				t.Fatal("stage is missing")
			}
			applyWorkflowPromptContext(&snapshot, "wf_exact", 7, version, step)
			workflow := snapshot.StructuredVariables["workflow"].(map[string]any)
			publication := workflow["publication"].(promptWorkflowPublication)
			if publication.RevisionRef != version.Ref || publication.VersionNumber != version.VersionNumber || publication.CoordinatorAgentRef != version.CoordinatorAgentRef || len(publication.Steps) != 2 {
				t.Fatal("published pins or full DAG are missing")
			}
			second := publication.Steps[1]
			if second.Key != "step-002" || second.AgentRef != "agt-reviewer" || second.Position != 2 || !reflect.DeepEqual(second.DependsOn, []string{"step-001"}) || !second.Parallel || second.ParallelGroup != 2 || !second.HumanGateAfter || !reflect.DeepEqual(second.GateDecisions, version.Steps[1].GateDecisions) {
				t.Fatal("published assignment, dependencies or gate changed")
			}
			if workflow["files_count"] != 1 || snapshot.StructuredVariables["input"].(map[string]any)["values"].(map[string]any)["task_data"] != "preserved" || snapshot.ContextPin.WorkflowRevisionRef != version.Ref || snapshot.ContextPin.WorkflowStageKey != key {
				t.Fatal("existing context was replaced")
			}
			result, err := promptservice.Materialize("Agent", promptservice.FromSnapshot(snapshot))
			if err != nil || !result.Complete {
				t.Fatalf("materialization: %v", err)
			}
			if _, err := runtimecontract.DecodePromptService(runtimecontract.RunnerInput{Instructions: result.Prompt, Capabilities: result.EffectiveCapabilities, PromptTargetKind: snapshot.TargetKind, PromptServiceTemplateRevision: result.ServiceTemplateRevision, PromptServiceTemplateDigest: result.ServiceTemplateDigest}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.SafePrompt, "agt-reviewer") || strings.Contains(result.SafePrompt, version.Ref) {
				t.Fatal("safe preview exposed workflow metadata")
			}
			publication.Steps[1].DependsOn[0] = "changed"
			publication.Steps[1].GateDecisions[0] = "changed"
			if version.Steps[1].DependsOn[0] != "step-001" || version.Steps[1].GateDecisions[0] != "APPROVE" {
				t.Fatal("snapshot aliases published source")
			}
		})
	}
}

func TestWorkflowPromptPublishedDAGDigestsAndCompactness(t *testing.T) {
	version := validWorkflowFixture()
	materialize := func(version entity.WorkflowVersion) promptservice.Materialization {
		snapshot := workflowPromptFixture()
		step, _ := promptWorkflowStep(version, "workflow.coordinator.continue.2")
		applyWorkflowPromptContext(&snapshot, "wf_exact", 7, version, step)
		raw, err := json.Marshal(snapshot)
		var captured entity.PromptMaterializationSnapshot
		if err != nil || json.Unmarshal(raw, &captured) != nil {
			t.Fatal("durable snapshot is invalid")
		}
		result, err := promptservice.Materialize("Agent", promptservice.FromSnapshot(captured))
		if err != nil || !result.Complete {
			t.Fatalf("materialization: %v", err)
		}
		return result
	}
	baseline := materialize(version)
	for _, change := range []string{"assignment", "dependency", "revision"} {
		changed := validWorkflowFixture()
		switch change {
		case "assignment":
			changed.Steps[1].AgentRef = "agt-other"
		case "dependency":
			changed.Steps[1].DependsOn = nil
		case "revision":
			changed.Ref, changed.VersionNumber = "wfv-new", 2
		}
		result := materialize(changed)
		if result.VariableSnapshotDigest == baseline.VariableSnapshotDigest || result.Digest == baseline.Digest || result.ServiceTemplateDigest != baseline.ServiceTemplateDigest {
			t.Fatalf("data-only change is not independently pinned: %s", change)
		}
	}
	version.Steps = nil
	for i := 1; i <= 39; i++ {
		step := entity.WorkflowStep{Key: fmt.Sprintf("step-%03d", i), Position: int32(i), Name: "Published stage", AgentRef: "agt-exact", TimeoutSeconds: 600, Instructions: strings.Repeat("I", 700), ExpectedResult: strings.Repeat("R", 700)}
		if i > 1 {
			step.DependsOn = []string{fmt.Sprintf("step-%03d", i-1)}
		}
		version.Steps = append(version.Steps, step)
	}
	if !validWorkflowVersion(version) {
		t.Fatal("compactness fixture is not publishable")
	}
	snapshot := workflowPromptFixture()
	applyWorkflowPromptContext(&snapshot, "wf_exact", 7, version, version.Steps[0])
	raw, err := json.Marshal(snapshot.StructuredVariables["workflow"].(map[string]any)["publication"])
	if err != nil || len(raw) > 16<<10 || strings.Contains(string(raw), strings.Repeat("I", 100)) || strings.Contains(string(raw), "ExpectedResult") {
		t.Fatal("compact projection copied private instruction bodies or exceeded fixture budget")
	}
}
