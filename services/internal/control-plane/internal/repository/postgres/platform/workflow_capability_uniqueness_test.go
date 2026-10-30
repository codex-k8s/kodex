package platform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowCapabilityUniquenessAtMutationBoundary rejects duplicate requirements before
// mutation, while retaining historical read eligibility and independent requirements of peers.
// TestWorkflowCapabilityUniquenessAtMutationBoundary отвергает повтор требований до изменения,
// сохраняя чтение истории и независимые требования разных этапов.
func TestWorkflowCapabilityUniquenessAtMutationBoundary(t *testing.T) {
	for _, mode := range []string{"legacy", "SERVER_DAG_V1"} {
		for _, item := range []struct {
			name string
			caps []string
			want bool
		}{
			{name: "empty", caps: []string{}, want: true},
			{name: "unique", caps: []string{"github.pull_request.read", "github.pull_request.review.list"}, want: true},
			{name: "adjacent duplicate", caps: []string{"github.pull_request.read", "github.pull_request.read"}},
			{name: "separated duplicate", caps: []string{"github.pull_request.read", "github.pull_request.review.list", "github.pull_request.read"}},
		} {
			t.Run(mode+"/"+item.name, func(t *testing.T) {
				draft := validWorkflowFixture()
				if mode == "SERVER_DAG_V1" {
					created, err := assistantWorkflow(assistantPDLCWorkflowFixture(t))
					if err != nil {
						t.Fatal("prepare valid server workflow", err)
					}
					draft = *created.Draft
				}
				draft.Steps[0].RequiredCapabilityKeys = append([]string{}, item.caps...)
				draft.Steps[1].RequiredCapabilityKeys = []string{"github.pull_request.read"}
				before := append([]string{}, draft.Steps[0].RequiredCapabilityKeys...)
				if got := validWorkflowVersion(draft); got != item.want {
					t.Fatalf("mutation eligibility = %v, want %v", got, item.want)
				}
				if !reflect.DeepEqual(before, draft.Steps[0].RequiredCapabilityKeys) {
					t.Fatal("validation silently rewrote capability requirements")
				}
				if field := workflowVersionStructureInvalidField(draft); field != "" {
					t.Fatalf("historical snapshot became unreadable: %s", field)
				}
			})
		}
	}
}

// TestAssistantWorkflowRejectsDuplicateCapabilities proves CREATE, UPDATE hydration and direct
// command mapping reject the same duplicate rather than persisting a misleading applicable plan.
// TestAssistantWorkflowRejectsDuplicateCapabilities проверяет единый отказ CREATE, подготовки
// UPDATE и прямого преобразования команды вместо сохранения вводящего в заблуждение плана.
func TestAssistantWorkflowRejectsDuplicateCapabilities(t *testing.T) {
	for _, caps := range [][]any{
		{"github.pull_request.read", "github.pull_request.review.list", "github.pull_request.read"},
		{"github.pull_request.read", " github.pull_request.read "},
	} {
		input := assistantPDLCWorkflowFixture(t)
		created, err := assistantWorkflow(input)
		if err != nil {
			t.Fatal("prepare valid assistant workflow", err)
		}
		draft := *created.Draft
		fields, steps := assistantWorkflowGraphFields(draft)
		before := map[string]any{
			"workflowRef": "wfl_fixture123", "projectRef": created.ProjectRef,
			"executionMode": draft.ExecutionMode, "resultEvidenceProfile": draft.ResultEvidenceProfile,
			"name": draft.Name, "purpose": draft.Purpose, "instructions": draft.Instructions,
			"completionCriteria": draft.CompletionCriteria, "coordinatorAgentRef": draft.CoordinatorAgentRef,
			"inputFields": fields, "steps": steps, "maxConcurrency": float64(draft.Concurrency),
			"timeoutSeconds": float64(draft.TimeoutSeconds), "draft": draft,
		}
		badSteps := input["steps"].([]any)
		badSteps[0].(map[string]any)["requiredCapabilityKeys"] = caps
		if _, err := assistantWorkflow(input); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("CREATE admitted duplicate requirements: %v", err)
		}
		_, err = hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{
			Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": "wfl_fixture123", "steps": badSteps},
		})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("UPDATE hydration admitted duplicate requirements: %v", err)
		}
		direct := cloneAssistantFields(before)
		delete(direct, "draft")
		direct["steps"], direct["expectedVersion"] = badSteps, float64(7)
		_, _, err = assistantUpdateWorkflow(entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Before: before, Input: direct})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("UPDATE command admitted duplicate requirements: %v", err)
		}
	}
}
