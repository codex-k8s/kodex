package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowReworkSubjects reads only immutable typed rejection from the exact final journal.
// queryWorkflowReworkSubjects читает immutable typed rejection только exact final journal.
//
//go:embed sql/workflow_rework__subjects.sql
var queryWorkflowReworkSubjects string

// enrichWorkflowReworkCauseTx restores pending sibling causes without parsing truncated summaries.
// Manual owner changes derive targets from published predicates, or the exact author Gate itself;
// they do not fabricate a model verdict. Legacy episodes retain their old route and absent kind.
// enrichWorkflowReworkCauseTx восстанавливает sibling cause без разбора усечённого summary.
// Owner changes выводят targets из published predicates либо самого exact author Gate,
// не создавая model verdict. Legacy сохраняет прежний route и отсутствующий kind.
func enrichWorkflowReworkCauseTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, cause *serverWorkflowException) error {
	if root.Version.ReworkPolicy != workflowservice.OwnerApprovedUpstreamV1 || cause.Kind != "" || (cause.Reason != workflowChangesRequired && cause.Reason != "OWNER_CHANGES_REQUESTED") {
		return nil
	}
	var rejections []workflowReviewRejection
	manual := cause.Reason == "OWNER_CHANGES_REQUESTED"
	if manual {
		for _, step := range root.Version.Steps {
			if step.Key != cause.StepKey {
				continue
			}
			for _, requirement := range step.ResultRequirements {
				if requirement.Kind == "ARTIFACT_REVIEW" || requirement.Kind == "REVIEW" || requirement.Kind == "QA" {
					rejections = append(rejections, workflowReviewRejection{requirement.Key, requirement.SubjectStepKey, requirement.SubjectResultKey})
				}
			}
		}
		if len(rejections) == 0 {
			rejections = []workflowReviewRejection{{SubjectStepKey: cause.StepKey}}
		}
	} else {
		if tx.QueryRow(ctx, queryWorkflowReworkSubjects, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "origin_node_id": cause.OriginNodeID, "final_node_id": cause.FinalNodeID, "business_attempt": cause.Attempt}).Scan(&rejections) != nil {
			return errs.ErrUnavailable
		}
	}
	anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
	if err != nil {
		return err
	}
	plan, err := buildWorkflowUpstreamPlan(root, anchors, cause.StepKey, cause.Attempt, rejections, manual)
	if err != nil {
		return err
	}
	cause.Kind, cause.ReworkPlan = workflowUpstreamReworkKind, plan
	return nil
}
