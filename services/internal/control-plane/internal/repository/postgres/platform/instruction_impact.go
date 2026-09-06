package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/instruction_impact_snapshot.sql
var queryInstructionImpactSnapshot string

type instructionImpactSnapshot struct {
	agentID, systemKey, projectID, projectRef, revisionRef, state, content, digest, bindingRef, sourceRevision string
	agentVersion, revisionVersion, bindingVersion                                                              int64
	effective                                                                                                  bool
}

func readInstructionImpact(ctx context.Context, tx pgx.Tx, s scope, agent, revision string) (instructionImpactSnapshot, error) {
	var v instructionImpactSnapshot
	err := tx.QueryRow(ctx, queryInstructionImpactSnapshot, pgx.StrictNamedArgs{"organization_id": s.organizationID, "agent_ref": agent, "revision_ref": revision}).Scan(&v.agentID, &v.systemKey, &v.agentVersion, &v.projectID, &v.projectRef, &v.revisionRef, &v.revisionVersion, &v.state, &v.content, &v.digest, &v.bindingRef, &v.bindingVersion, &v.sourceRevision, &v.effective)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, errs.ErrNotFound
	}
	if err != nil {
		return v, errs.ErrUnavailable
	}
	if v.systemKey != "" {
		return v, errs.ErrProtected
	}
	return v, nil
}

func (r *Repository) prepareInstructionsImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command) (commandOutcome, error) {
	p, ok := input.Payload.(command.AgentInput)
	if !ok || p.PlanRef != "" || len(p.SelectedItemRefs) != 0 {
		return commandOutcome{}, errs.ErrInvalid
	}
	v, err := readInstructionImpact(ctx, tx, s, p.Ref, "")
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != v.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if v.state != "VALID" {
		return commandOutcome{}, errs.ErrConflict
	}
	if err = r.validateAgentPromptContextTx(ctx, tx, s, p.Ref, v.content, false); err != nil {
		return commandOutcome{}, err
	}
	plan := entity.RevisionImpactPlan{Kind: "AGENT_INSTRUCTIONS", SourceRef: p.Ref, SourceVersion: v.agentVersion, SourceRevisionRef: v.sourceRevision, DraftRef: v.revisionRef, DraftVersion: v.revisionVersion, TargetDigest: v.digest}
	items := []entity.RevisionImpactItem{}
	if v.effective {
		ref, err := newRef("rvit")
		if err != nil {
			return commandOutcome{}, err
		}
		items = append(items, entity.RevisionImpactItem{Ref: ref, ProjectRef: v.projectRef, ConsumerKind: "AGENT", ConsumerRef: p.Ref, ConsumerVersion: v.agentVersion, BindingRef: v.bindingRef, BindingVersion: v.bindingVersion, SourceRevisionRef: v.sourceRevision, Outcome: "PENDING"})
	}
	plan, err = persistRevisionImpact(ctx, tx, s, plan, items)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{RevisionImpactPlan: &plan}, projectID: v.projectID, projectRef: v.projectRef, resourceKind: "INSTRUCTIONS", resourceRef: p.Ref, summary: "i18n:AGENT_INSTRUCTIONS_IMPACT_PREPARED"}, nil
}

func (r *Repository) authorizeInstructionsImpactPublish(ctx context.Context, tx pgx.Tx, s scope, input command.Command) error {
	p, ok := input.Payload.(command.AgentInput)
	if !ok {
		return errs.ErrInvalid
	}
	row, err := r.revisionImpact(ctx, tx, s, p.PlanRef)
	if err != nil {
		return err
	}
	if row.plan.Kind != "AGENT_INSTRUCTIONS" || row.plan.SourceRef != p.Ref {
		return errs.ErrNotFound
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return err
	}
	v, err := readInstructionImpact(ctx, tx, s, p.Ref, row.plan.DraftRef)
	if err != nil {
		return err
	}
	if v.digest != row.plan.TargetDigest {
		return errs.ErrConflict
	}
	return r.validateAgentPromptContextTx(ctx, tx, s, p.Ref, v.content, false)
}

func (r *Repository) instructionPlanForPublish(ctx context.Context, tx pgx.Tx, s scope, p command.AgentInput, version int64) (revisionImpactRow, []entity.RevisionImpactItem, error) {
	row, err := r.revisionImpact(ctx, tx, s, p.PlanRef)
	if err != nil {
		return row, nil, err
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return row, nil, err
	}
	v, err := readInstructionImpact(ctx, tx, s, p.Ref, "")
	if err != nil {
		return row, nil, err
	}
	if row.plan.Kind != "AGENT_INSTRUCTIONS" || row.plan.SourceRef != p.Ref || row.plan.SourceVersion != version || row.plan.State != "PREPARED" || row.plan.DraftRef != v.revisionRef || row.plan.DraftVersion != v.revisionVersion || row.plan.TargetDigest != v.digest || row.plan.SourceRevisionRef != v.sourceRevision || v.state != "VALID" {
		return row, nil, errs.ErrConflict
	}
	items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
	if err != nil {
		return row, nil, err
	}
	if err = selectRevisionImpactItems(items, p.SelectedItemRefs); err != nil {
		return row, nil, err
	}
	for i := range items {
		item := &items[i]
		if item.ConsumerRef != p.Ref || item.ConsumerKind != "AGENT" {
			return row, nil, errs.ErrUnavailable
		}
		if item.Outcome == "PENDING" && (!v.effective || item.BindingRef != v.bindingRef || item.BindingVersion != v.bindingVersion || item.ConsumerVersion != version || item.SourceRevisionRef != v.sourceRevision) {
			item.Outcome = "CONFLICT"
		}
	}
	return row, items, nil
}
