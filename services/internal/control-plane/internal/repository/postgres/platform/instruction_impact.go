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

// queryInstructionImpactSnapshot embeds locked agent/instruction/binding provenance for a project
// agent and exact revision or current draft. Effective status excludes an overriding managed
// prompt binding; reading this row does not confer agent permission.
// queryInstructionImpactSnapshot встраивает заблокированное происхождение
// agent/instruction/binding для project agent и точной revision либо текущего draft. Effective
// status исключает перекрывающий managed prompt binding; чтение строки не выдаёт право на агента.
//
//go:embed sql/instruction_impact_snapshot.sql
var queryInstructionImpactSnapshot string

// instructionImpactSnapshot carries locked agent, draft/source revision, binding OCC and scope
// metadata plus content/digest and effectiveness for one impact calculation. It is internal
// transaction data, not public preview or a capability.
// instructionImpactSnapshot хранит заблокированные agent, draft/source revision, binding OCC и
// scope metadata с content/digest и effectiveness одного impact calculation. Это внутренние данные
// транзакции, а не public preview и capability.
type instructionImpactSnapshot struct {
	agentID, systemKey, projectID, projectRef, revisionRef, state, content, digest, bindingRef, sourceRevision string
	agentVersion, revisionVersion, bindingVersion                                                              int64
	effective                                                                                                  bool
	scopeKind, organizationRef                                                                                 string
}

// readInstructionImpact scans the exact owner snapshot and rejects system-key agents as Protected.
// Missing rows are NotFound, query/scan failures Unavailable; caller permission and
// lifecycle-state validation remain outside.
// readInstructionImpact читает точный owner snapshot и отклоняет агентов с system key как
// Protected. Отсутствие строки даёт NotFound, query/scan errors — Unavailable; права caller и
// проверка lifecycle state остаются снаружи.
func readInstructionImpact(ctx context.Context, tx pgx.Tx, s scope, agent, revision string) (instructionImpactSnapshot, error) {
	var v instructionImpactSnapshot
	err := tx.QueryRow(ctx, queryInstructionImpactSnapshot, pgx.StrictNamedArgs{"organization_id": s.organizationID, "agent_ref": agent, "revision_ref": revision}).Scan(&v.agentID, &v.systemKey, &v.agentVersion, &v.projectID, &v.projectRef, &v.revisionRef, &v.revisionVersion, &v.state, &v.content, &v.digest, &v.bindingRef, &v.bindingVersion, &v.sourceRevision, &v.effective, &v.scopeKind, &v.organizationRef)
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

// prepareInstructionsImpact requires typed input without prior plan/selection, agent OCC, VALID
// draft and validated prompt context. It captures one PENDING agent binding only when instructions
// are effective and persists an actor-bound plan, otherwise an empty plan. No instruction
// publication, binding mutation or independent commit occurs.
// prepareInstructionsImpact требует typed input без прежнего плана и выбора, agent OCC, VALID
// draft и проверенный prompt context. Фиксирует один PENDING agent binding лишь при effective
// instructions и сохраняет actor-bound plan, иначе пустой план. Не публикует instructions, не
// меняет binding и не делает отдельный Commit.
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
		items = append(items, entity.RevisionImpactItem{Ref: ref, ProjectRef: v.projectRef, ScopeKind: v.scopeKind, OrganizationRef: v.organizationRef, ConsumerKind: "AGENT", ConsumerRef: p.Ref, ConsumerVersion: v.agentVersion, BindingRef: v.bindingRef, BindingVersion: v.bindingVersion, SourceRevisionRef: v.sourceRevision, Outcome: "PENDING"})
	}
	plan, err = persistRevisionImpact(ctx, tx, s, plan, items)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{RevisionImpactPlan: &plan}, projectID: v.projectID, projectRef: v.projectRef, resourceKind: "INSTRUCTIONS", resourceRef: p.Ref, summary: "i18n:AGENT_INSTRUCTIONS_IMPACT_PREPARED"}, nil
}

// authorizeInstructionsImpactPublish checks exact plan kind/source and access, locks the pinned
// draft and matches its digest, then revalidates prompt context. Full
// PREPARED/source-version/current-binding and selected-item checks occur later; PlanRef alone
// cannot grant authority.
// authorizeInstructionsImpactPublish проверяет точные plan kind/source и доступ, блокирует pinned
// draft, сопоставляет digest и заново проверяет prompt context. Полные
// PREPARED/source-version/current-binding и selected-item проверки выполняются далее; один PlanRef
// не выдаёт полномочия.
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

// instructionPlanForPublish requires the current VALID draft and exact PREPARED
// source/version/digest/previous revision, rechecks plan access and actor-bound items, then
// applies bounded selection. Wrong consumer kind/ref is Unavailable; a selected stale/eclipsed
// binding becomes CONFLICT rather than being silently rebound. It returns metadata without
// publishing.
// instructionPlanForPublish требует текущий VALID draft и точные PREPARED
// source/version/digest/previous revision, перепроверяет доступ к плану и actor-bound items и
// применяет ограниченный выбор. Неверный consumer kind/ref даёт Unavailable; выбранная устаревшая
// либо перекрытая binding становится CONFLICT вместо скрытого rebind. Возвращает metadata без
// публикации.
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
