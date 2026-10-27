package platform

import (
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowUpstreamReworkKind versions the owner episode without reinterpreting old Gates.
// workflowUpstreamReworkKind версионирует owner episode без переосмысления прежних Gates.
const workflowUpstreamReworkKind = "UPSTREAM_REWORK"

// workflowReworkPlanSchema pins the closed private transition shape, not a caller permission.
// workflowReworkPlanSchema закрепляет closed private transition shape, не caller permission.
const workflowReworkPlanSchema = "kodex.workflow-upstream-rework.v1"

// workflowReworkTarget records the exact logical author and its pre-approved next business attempt.
// workflowReworkTarget хранит exact logical author и следующую согласуемую business attempt.
type workflowReworkTarget struct {
	StepKey         string `json:"stepKey"`
	OriginNodeID    string `json:"originNodeId"`
	PreviousAttempt int32  `json:"previousAttempt"`
	NextAttempt     int32  `json:"nextAttempt"`
}

// workflowUpstreamReworkPlan is server-derived from published subjects under the owner root lock.
// Node IDs stay private; public projection contains only safe keys, counters and a closed blocker.
// workflowUpstreamReworkPlan выводится сервером из published subjects под owner root lock.
// Node IDs приватны; public projection содержит только safe keys, counters и closed blocker.
type workflowUpstreamReworkPlan struct {
	Schema        string                 `json:"schema"`
	SourceStepKey string                 `json:"sourceStepKey"`
	SourceAttempt int32                  `json:"sourceAttempt"`
	Targets       []workflowReworkTarget `json:"targets"`
	Rechecks      []workflowReworkTarget `json:"rechecks"`
	BlockReason   string                 `json:"blockReason,omitempty"`
}

// buildWorkflowUpstreamPlan resolves all requested authors and affected consumers from one snapshot.
// Invalid or ambiguous subjects cannot become launch authority; exhausted or irreversible work is
// represented as an explicit owner-visible blocker. No state, lease or history is changed here.
// buildWorkflowUpstreamPlan разрешает authors и affected consumers из одного snapshot.
// Invalid/ambiguous subjects не дают launch authority; исчерпание бюджета и irreversible work
// представлены явным blocker. State, leases и history здесь не изменяются.
func buildWorkflowUpstreamPlan(root *serverWorkflowRoot, anchors map[string]serverWorkflowAnchor, sourceStepKey string, sourceAttempt int32, rejections []workflowReviewRejection, manual bool) (*workflowUpstreamReworkPlan, error) {
	if root == nil || root.Version.ReworkPolicy != workflowservice.OwnerApprovedUpstreamV1 || len(root.Version.Steps) > workflowservice.MaximumSteps || len(rejections) < 1 || len(rejections) > 4 || sourceAttempt < 1 || sourceAttempt > 4 {
		return nil, errs.ErrConflict
	}
	steps := make(map[string]entity.WorkflowStep, len(root.Version.Steps))
	for _, step := range root.Version.Steps {
		steps[step.Key] = step
	}
	source, exists := steps[sourceStepKey]
	if !exists || anchors[sourceStepKey].LatestAttempt != sourceAttempt {
		return nil, errs.ErrConflict
	}
	effective := effectiveServerWorkflowResults(root, anchors)
	targets := make(map[string]bool)
	for _, rejection := range rejections {
		if manual && rejection.RequirementKey == "" && rejection.SubjectStepKey == sourceStepKey && rejection.SubjectResultKey == "" {
			targets[sourceStepKey] = true
			continue
		}
		matched := false
		for _, requirement := range source.ResultRequirements {
			if requirement.Key != rejection.RequirementKey || requirement.SubjectStepKey != rejection.SubjectStepKey || requirement.SubjectResultKey != rejection.SubjectResultKey {
				continue
			}
			if requirement.Kind == "ARTIFACT_REVIEW" {
				if !workflowservice.ValidRequirements([]entity.WorkflowResultRequirement{requirement}, source.DependsOn) {
					return nil, errs.ErrConflict
				}
				targets[requirement.SubjectStepKey], matched = true, true
			} else if requirement.Kind == "REVIEW" || requirement.Kind == "QA" {
				author, valid := workflowReworkGitAuthor(requirement.SubjectStepKey, steps, map[string]bool{}, 0)
				if !valid {
					return nil, errs.ErrConflict
				}
				targets[author], matched = true, true
			}
		}
		if !matched {
			return nil, errs.ErrConflict
		}
	}
	plan := &workflowUpstreamReworkPlan{Schema: workflowReworkPlanSchema, SourceStepKey: sourceStepKey, SourceAttempt: sourceAttempt, Targets: []workflowReworkTarget{}, Rechecks: []workflowReworkTarget{}}
	affected := make(map[string]bool)
	for _, step := range root.Version.Steps {
		if targets[step.Key] {
			anchor := anchors[step.Key]
			if _, accepted := effective[step.Key]; !accepted {
				if !manual || step.Key != sourceStepKey || len(anchor.Receipts) == 0 || len(anchor.Receipts) != int(anchor.LatestAttempt) || !anchor.Receipts[len(anchor.Receipts)-1].Accepted {
					return nil, errs.ErrConflict
				}
			}
			if anchor.ID == "" || anchor.LatestAttempt < 1 {
				return nil, errs.ErrConflict
			}
			plan.Targets = append(plan.Targets, workflowReworkTarget{step.Key, anchor.ID, anchor.LatestAttempt, anchor.LatestAttempt + 1})
			affected[step.Key] = true
		}
		for _, dependency := range step.DependsOn {
			if affected[dependency] {
				affected[step.Key] = true
			}
		}
		if !affected[step.Key] {
			continue
		}
		anchor := anchors[step.Key]
		if anchor.LatestAttempt >= 4 && plan.BlockReason == "" {
			plan.BlockReason = "WORKFLOW_ATTEMPTS_EXHAUSTED"
		}
		if anchor.HasIrreversibleEffect {
			plan.BlockReason = "WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED"
		}
		for _, receipt := range anchor.Receipts {
			if !receipt.Accepted {
				continue
			}
			for _, result := range receipt.Results {
				if result.Kind == "INTEGRATION_RECEIPT" || result.Kind == "DEPLOYMENT" {
					plan.BlockReason = "WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED"
				}
			}
		}
		if !targets[step.Key] {
			plan.Rechecks = append(plan.Rechecks, workflowReworkTarget{step.Key, anchor.ID, anchor.LatestAttempt, anchor.LatestAttempt + 1})
		}
	}
	if len(plan.Targets) == 0 || len(plan.Targets) > 4 || len(plan.Rechecks) > workflowservice.MaximumSteps {
		return nil, errs.ErrConflict
	}
	return plan, nil
}

// workflowReworkGitAuthor follows only published REVIEW/QA subject links to a single PR producer.
// It rejects multiple authors, cycles and irreversible merge/deployment subjects instead of guessing.
// workflowReworkGitAuthor следует лишь published REVIEW/QA links к одному PR producer.
// Несколько авторов, cycles и irreversible subjects отклоняются вместо предположения.
func workflowReworkGitAuthor(key string, steps map[string]entity.WorkflowStep, visiting map[string]bool, depth int) (string, bool) {
	step, exists := steps[key]
	if !exists || depth >= workflowservice.MaximumSteps || visiting[key] {
		return "", false
	}
	visiting[key] = true
	defer delete(visiting, key)
	hasPR := false
	for _, requirement := range step.ResultRequirements {
		if requirement.Kind == "PULL_REQUEST" {
			hasPR = true
		}
		if requirement.Kind == "INTEGRATION_RECEIPT" || requirement.Kind == "DEPLOYMENT" {
			return "", false
		}
	}
	if hasPR {
		return key, true
	}
	authors := map[string]bool{}
	for _, requirement := range step.ResultRequirements {
		if requirement.Kind != "REVIEW" && requirement.Kind != "QA" {
			continue
		}
		author, valid := workflowReworkGitAuthor(requirement.SubjectStepKey, steps, visiting, depth+1)
		if !valid {
			return "", false
		}
		authors[author] = true
	}
	if len(authors) != 1 {
		return "", false
	}
	keys := make([]string, 0, len(authors))
	for author := range authors {
		keys = append(keys, author)
	}
	sort.Strings(keys)
	return keys[0], true
}
