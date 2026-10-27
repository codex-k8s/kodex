package platform

import (
	"testing"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// reworkPlanFixture models one author, two independent reviewers and an unrelated branch.
// Accepted source records stay immutable even when their current effectiveness changes later.
// reworkPlanFixture моделирует автора, двух независимых reviewers и отдельную ветвь.
// Accepted source records неизменны при последующей смене их эффективности.
func reworkPlanFixture() (*serverWorkflowRoot, map[string]serverWorkflowAnchor) {
	root := &serverWorkflowRoot{Version: entity.WorkflowVersion{ReworkPolicy: workflowservice.OwnerApprovedUpstreamV1, Steps: []entity.WorkflowStep{
		{Key: "author", ResultRequirements: []entity.WorkflowResultRequirement{{Key: "spec", Kind: "ARTIFACT", FileName: "spec.md", MediaType: "text/markdown"}}},
		{Key: "r", DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "review", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"}}},
		{Key: "q", DependsOn: []string{"author"}}, {Key: "independent"},
	}}}
	author := freshnessAnchorFixture("author", nil)
	inputs := []workflowInputReceipt{{StepKey: "author", SourceNodeID: author.ID, Receipt: author.Receipts[0]}}
	r := freshnessAnchorFixture("r", inputs)
	r.Receipts[0].Accepted = false
	r.Receipts[0].Reason = workflowChangesRequired
	return root, map[string]serverWorkflowAnchor{"author": author, "r": r, "q": freshnessAnchorFixture("q", inputs), "independent": freshnessAnchorFixture("independent", nil)}
}

// TestWorkflowUpstreamPlanReturnsToAuthorAndInvalidatesBothReviews verifies published targets.
// It does not infer authorship from a model narrative, timestamp or agent role name.
// TestWorkflowUpstreamPlanReturnsToAuthorAndInvalidatesBothReviews проверяет published targets,
// не выводя автора из model narrative, timestamp либо имени роли.
func TestWorkflowUpstreamPlanReturnsToAuthorAndInvalidatesBothReviews(t *testing.T) {
	root, anchors := reworkPlanFixture()
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false)
	if err != nil || !validWorkflowReworkPlan(plan) || len(plan.Targets) != 1 || plan.Targets[0].StepKey != "author" || plan.Targets[0].PreviousAttempt != 1 || plan.Targets[0].NextAttempt != 2 || len(plan.Rechecks) != 2 || plan.Rechecks[0].StepKey != "r" || plan.Rechecks[1].StepKey != "q" {
		t.Fatal("wrong author or affected closure")
	}
	if anchors["q"].Receipts[0].Accepted != true {
		t.Fatal("old independent reviewer receipt was rewritten")
	}
	root.ReworkFloors = map[string]int32{"author": 2, "r": 2, "q": 2}
	effective := effectiveServerWorkflowResults(root, anchors)
	if len(effective) != 1 || effective["independent"].Attempt != 1 {
		t.Fatal("approved deferred plan left old PASS effective before nodes existed")
	}
}

// TestWorkflowUpstreamPlanKeepsManualAuthorChangesSeparateFromReviewVerdict proves owner routing.
// TestWorkflowUpstreamPlanKeepsManualAuthorChangesSeparateFromReviewVerdict проверяет owner routing.
func TestWorkflowUpstreamPlanKeepsManualAuthorChangesSeparateFromReviewVerdict(t *testing.T) {
	root, anchors := reworkPlanFixture()
	author := anchors["author"]
	author.GateBlocked = true
	anchors["author"] = author
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "author", 1, []workflowReviewRejection{{SubjectStepKey: "author"}}, true)
	if err != nil || plan.Targets[0].StepKey != "author" || !anchors["author"].Receipts[0].Accepted {
		t.Fatal("owner changes fabricated or rejected a model review verdict")
	}
	if _, err := buildWorkflowUpstreamPlan(root, anchors, "author", 1, []workflowReviewRejection{{SubjectStepKey: "independent"}}, true); err == nil {
		t.Fatal("owner free target bypassed published route")
	}
}

// TestWorkflowUpstreamPlanClosesUnknownPolicyAndIrreversibleEffects checks explicit blockers.
// TestWorkflowUpstreamPlanClosesUnknownPolicyAndIrreversibleEffects проверяет explicit blockers.
func TestWorkflowUpstreamPlanClosesUnknownPolicyAndIrreversibleEffects(t *testing.T) {
	root, anchors := reworkPlanFixture()
	q := anchors["q"]
	q.Receipts[0].Results = []entity.WorkflowAcceptedResult{{Kind: "INTEGRATION_RECEIPT"}}
	anchors["q"] = q
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false)
	if err != nil || plan.BlockReason != "WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED" {
		t.Fatal("known SUCCESS was treated as automatic rollback")
	}
	root.Version.ReworkPolicy = ""
	if _, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false); err == nil {
		t.Fatal("legacy profile gained upstream authority")
	}
}

// TestWorkflowUpstreamPlanBlocksUnacceptedKnownExternalSuccess uses owner ledger metadata even
// when the affected attempt has no accepted results; a different failed predicate cannot erase a fact.
// TestWorkflowUpstreamPlanBlocksUnacceptedKnownExternalSuccess учитывает owner ledger metadata
// даже без accepted results; отказ другого predicate не стирает состоявшийся внешний факт.
func TestWorkflowUpstreamPlanBlocksUnacceptedKnownExternalSuccess(t *testing.T) {
	root, anchors := reworkPlanFixture()
	author := anchors["author"]
	author.HasIrreversibleEffect = true
	anchors["author"] = author
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false)
	if err != nil || plan.BlockReason != "WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED" {
		t.Fatal("known successful external effect was allowed to repeat")
	}
}

// TestWorkflowReworkPlanMemberCannotConsumeApprovalForLaterFailure checks exact identity/counter.
// TestWorkflowReworkPlanMemberCannotConsumeApprovalForLaterFailure проверяет exact identity/counter.
func TestWorkflowReworkPlanMemberCannotConsumeApprovalForLaterFailure(t *testing.T) {
	root, anchors := reworkPlanFixture()
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	anchor := anchors["q"]
	if !workflowReworkMember(plan, anchor) {
		t.Fatal("covered old sibling receipt was lost")
	}
	anchor.LatestAttempt = 2
	if workflowReworkMember(plan, anchor) {
		t.Fatal("fresh failed attempt inherited older approval")
	}
	anchor = anchors["q"]
	anchor.ID = "foreign"
	if workflowReworkMember(plan, anchor) {
		t.Fatal("neighbor origin consumed approval")
	}
}
