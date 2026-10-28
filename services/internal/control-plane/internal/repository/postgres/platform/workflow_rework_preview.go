package platform

import (
	"encoding/json"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// projectWorkflowReworkPreview whitelists explanatory keys/counters from the immutable owner plan.
// The public preview carries neither node IDs nor evidence/authority copied from model narrative.
// projectWorkflowReworkPreview выбирает explanatory keys/counters immutable owner plan.
// Public preview не содержит node IDs либо evidence/authority из model narrative.
func projectWorkflowReworkPreview(gate *entity.OwnerGate, raw []byte) error {
	if gate.ResolutionKind != "" && (gate.ResolutionKind != "SUPERSEDED" || gate.State != "CANCELLED" || gate.Decision != "") {
		return errs.ErrConflict
	}
	gate.ReworkPlan = nil
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var plan workflowUpstreamReworkPlan
	if json.Unmarshal(raw, &plan) != nil || !validWorkflowReworkPlan(&plan) {
		return errs.ErrConflict
	}
	preview := &entity.WorkflowReworkPreview{Schema: "kodex.workflow-upstream-rework-preview.v1", SourceStepKey: plan.SourceStepKey, SourceAttempt: plan.SourceAttempt, Authors: []entity.WorkflowReworkStepPreview{}, Rechecks: []entity.WorkflowReworkStepPreview{}, BlockReason: plan.BlockReason}
	for _, target := range plan.Targets {
		next := target.NextAttempt
		if next > 4 {
			next = 0
		}
		preview.Authors = append(preview.Authors, entity.WorkflowReworkStepPreview{StepKey: target.StepKey, PreviousAttempt: target.PreviousAttempt, NextAttempt: next})
	}
	for _, target := range plan.Rechecks {
		next := target.NextAttempt
		if next > 4 {
			next = 0
		}
		preview.Rechecks = append(preview.Rechecks, entity.WorkflowReworkStepPreview{StepKey: target.StepKey, PreviousAttempt: target.PreviousAttempt, NextAttempt: next})
	}
	gate.ReworkPlan = preview
	return nil
}
