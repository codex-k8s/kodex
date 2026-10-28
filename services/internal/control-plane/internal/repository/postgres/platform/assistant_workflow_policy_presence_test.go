package platform

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantWorkflowPolicyPresence preserves omitted optional selectors through actual sparse
// hydration, retains an existing owner policy, and rejects explicit malformed values without mutation.
// TestAssistantWorkflowPolicyPresence сохраняет отсутствие optional selectors при штатной sparse
// hydration, прежнюю owner policy и отказ на неверный явный ввод без изменения исходного снимка.
func TestAssistantWorkflowPolicyPresence(t *testing.T) {
	for _, profile := range []string{"legacy", "server", "server-rework"} {
		for _, change := range []string{"omitted", "clear", "null", "unknown"} {
			t.Run(profile+"/"+change, func(t *testing.T) {
				var draft entity.WorkflowVersion
				var before map[string]any
				if profile == "legacy" {
					draft, before, _ = assistantWorkflowFrontierFixture(t)
				} else {
					created, err := assistantWorkflow(assistantServerWorkflowFixture(t))
					if err != nil || created.Draft == nil {
						t.Fatal("valid server fixture rejected")
					}
					draft = *created.Draft
					if profile == "server-rework" {
						draft.ReworkPolicy = "OWNER_APPROVED_UPSTREAM_V1"
					}
					fields, steps := assistantWorkflowGraphFields(draft)
					before = map[string]any{"workflowRef": "wfl_fixture123", "projectRef": created.ProjectRef,
						"executionMode": draft.ExecutionMode, "name": draft.Name, "purpose": draft.Purpose,
						"instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria,
						"coordinatorAgentRef": draft.CoordinatorAgentRef, "inputFields": fields, "steps": steps,
						"maxConcurrency": float64(draft.Concurrency), "timeoutSeconds": float64(draft.TimeoutSeconds), "draft": draft}
					if draft.ReworkPolicy != "" {
						before["reworkPolicy"] = draft.ReworkPolicy
					}
				}
				original, err := json.Marshal(before)
				if err != nil {
					t.Fatal("owner snapshot cannot be serialized")
				}
				parameters := map[string]any{"workflowRef": before["workflowRef"], "name": "Bounded edited name"}
				if change != "omitted" {
					var value any = ""
					if change == "null" {
						value = nil
					} else if change == "unknown" {
						value = "UNKNOWN_POLICY"
					}
					parameters["reworkPolicy"] = value
				}
				operation, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: parameters})
				if change == "null" || change == "unknown" {
					if !errors.Is(err, errs.ErrInvalid) {
						t.Fatal("malformed explicit policy accepted")
					}
				} else {
					if err != nil {
						t.Fatal("valid sparse policy update rejected")
					}
					updated, version, err := assistantUpdateWorkflow(operation)
					want := draft.ReworkPolicy
					if change == "clear" {
						want = ""
					}
					if err != nil || version != 7 || updated.Draft == nil || updated.Draft.ReworkPolicy != want || updated.Draft.ExecutionMode != draft.ExecutionMode {
						t.Fatal("hydrated policy or mode differs from the exact edit")
					}
					_, supplied := before["reworkPolicy"]
					if !supplied && change == "omitted" {
						if _, exists := operation.Parameters["reworkPolicy"]; exists {
							t.Fatal("absent policy materialized in command parameters")
						}
						if _, exists := operation.After["reworkPolicy"]; exists {
							t.Fatal("absent policy materialized in the owner preview")
						}
					}
					if profile == "legacy" {
						if _, exists := operation.After["executionMode"]; exists {
							t.Fatal("absent legacy mode materialized in the preview")
						}
					}
				}
				after, err := json.Marshal(before)
				if err != nil || string(original) != string(after) {
					t.Fatal("policy hydration mutated the authoritative snapshot")
				}
			})
		}
	}
}
