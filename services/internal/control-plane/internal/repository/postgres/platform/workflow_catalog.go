package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowCatalogEntries embeds at most 11 ref-ordered published workflow rows for an active
// tenant/project with both current workflow.view and workflow.launch eligibility before
// search/cursor/page selection. The final projection adds the immutable published digest for the
// shared workflow scanner; grants still come from the leased caller path.
// queryWorkflowCatalogEntries встраивает до 11 опубликованных workflow rows активного
// tenant/project в порядке ref с текущими workflow.view и workflow.launch до search/cursor/page
// selection. Последнее поле проекции добавляет immutable published digest для общего workflow
// scanner; полномочия по-прежнему устанавливает leased caller path.
//
//go:embed sql/workflow_catalog__entries.sql
var queryWorkflowCatalogEntries string

// queryWorkflowLaunchPublishedPins embeds current published revision ref/digest and workflow OCC
// version under exact tenant/project/workflow. It locks the workflow row for a new launch intent;
// it neither validates actor access nor substitutes draft content for the immutable published
// version.
// queryWorkflowLaunchPublishedPins встраивает текущие published revision ref/digest и workflow OCC
// version внутри точных tenant/project/workflow. Блокирует workflow row для нового launch intent;
// не проверяет actor access и не подменяет immutable published version содержимым draft.
//
//go:embed sql/workflow_launch__published_pins.sql
var queryWorkflowLaunchPublishedPins string

// workflowCatalogRow wraps a row scanner plus the destination for its final immutable published
// digest column, letting scanWorkflow keep the canonical stored-schema decoder. It owns no row
// lifetime and does not validate digest text, content or access itself.
// workflowCatalogRow оборачивает row scanner и destination последней immutable published digest
// column, позволяя scanWorkflow сохранить канонический stored-schema decoder. Не владеет временем
// жизни rows и сам не проверяет digest text, content или доступ.
type workflowCatalogRow struct {
	row    rowScanner
	digest *string
}

// Scan appends the digest destination after the caller-supplied workflow columns and forwards the
// scan error unchanged. Destination order is part of the SQL/scanner contract; this adapter does
// not decode or authorize the result.
// Scan добавляет digest destination после переданных caller workflow columns и передаёт scan error
// без изменения. Порядок destinations является частью SQL/scanner contract; адаптер не декодирует
// и не авторизует результат.
func (row workflowCatalogRow) Scan(dest ...any) error {
	return row.row.Scan(append(dest, row.digest)...)
}

// GetExecutionWorkflowCatalog bounds the entire leased-catalog read with catalogQueryTimeout and
// delegates through the existing locked-read retry wrapper. Each attempt resolves a fresh owner
// snapshot; the wrapper does not relax lease, actor, project or capability requirements and does
// not issue a launch grant.
// GetExecutionWorkflowCatalog ограничивает всё leased-catalog чтение catalogQueryTimeout и
// делегирует существующей locked-read retry wrapper. Каждая попытка разрешает свежий owner
// snapshot; обёртка не ослабляет lease, actor, project или capability requirements и не выдаёт
// launch grant.
func (repository *Repository) GetExecutionWorkflowCatalog(ctx context.Context, p value.Principal, input query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	return retryAssistantLockedRead(ctx, func(attempt context.Context) (entity.ExecutionWorkflowCatalog, error) {
		return repository.executionWorkflowCatalogOnce(attempt, p, input)
	})
}

// executionWorkflowCatalogOnce resolves machine scope, opens RepeatableRead without ReadOnly
// because origin rows lock, and validates exact live lease/fence/generation plus current root
// actor/project and launch capability. It delegates pinned publication/active-run modes or builds
// a ten-item authorized discovery page with immutable digests and readiness. Only origin/commit
// errors use the existing locked-read retry marker; deferred rollback controls named resultError.
// Success requires commit and returns metadata rather than execution permission or a child run.
// executionWorkflowCatalogOnce разрешает machine scope, открывает RepeatableRead без ReadOnly
// из-за блокировки origin rows и проверяет точные live lease/fence/generation с текущими root
// actor/project и launch capability. Делегирует pinned publication/active-run modes либо строит
// разрешённую discovery page до десяти items с immutable digests и readiness. Только origin/commit
// errors используют прежний locked-read retry marker; deferred rollback управляет именованным
// resultError. Успех требует commit и возвращает metadata вместо execution permission или child
// run.
func (repository *Repository) executionWorkflowCatalogOnce(ctx context.Context, p value.Principal, input query.ExecutionWorkflowCatalog) (_ entity.ExecutionWorkflowCatalog, resultError error) {
	empty := entity.ExecutionWorkflowCatalog{}
	machine, err := repository.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	// Exact lease resolver удерживает execution/actor rows; query не меняет данные.
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	fence := sha256.Sum256([]byte(input.Fence))
	actor := machine
	var rootID, runID, nodeID, sessionID, turnID, revisionID, projectID, projectRef, agentRef, inputDigest, revisionDigest string
	var capabilities []string
	var attempt int32
	err = tx.QueryRow(ctx, queryWorkflowLaunchResolveOrigin, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "lease_ref": input.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "generation": input.Generation}).Scan(&rootID, &runID, &nodeID, &sessionID, &turnID, &revisionID, &projectID, &projectRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role, &actor.organizationRef, &agentRef, &capabilities, &attempt, &inputDigest, &revisionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, errs.ErrNotFound
	}
	if err != nil {
		return empty, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	err = tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, actor.actorRef, actor.organizationRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, errs.ErrForbidden
	}
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	actor.authorityProjectID = projectID
	effective, _, err := repository.agentCapabilityAuthority(ctx, tx, actor, projectRef, agentRef, capabilities)
	if err != nil {
		return empty, err
	}
	if !capabilityEnabled(effective, "platform.run.launch") {
		return empty, errs.ErrForbidden
	}
	if input.Publication != nil || input.ActiveRuns != nil {
		result, err := repository.readExecutionWorkflowCatalogTx(ctx, tx, actor, projectID, projectRef, revisionID, revisionDigest, input)
		if err != nil {
			return empty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		return result, nil
	}
	filter := query.Filter{ProjectRef: projectRef, ResourceRef: revisionID, ExpectedCatalogDigest: revisionDigest, Query: strings.TrimSpace(input.Query), State: "PUBLISHED", Page: query.Page{Size: 10, Token: input.PageToken}}
	cursor, err := decodeCatalogCursor(actor, "EXECUTION_WORKFLOW", filter)
	if err != nil {
		return empty, err
	}
	rows, err := tx.Query(ctx, queryWorkflowCatalogEntries, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "project_id": projectID, "actor_id": actor.actorID, "query": filter.Query, "cursor": cursor})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	var workflows []entity.Workflow
	var digests []string
	for rows.Next() {
		var digest string
		item, err := scanWorkflow(workflowCatalogRow{row: rows, digest: &digest}, true)
		if err != nil || !exactSHA256(digest) || item.Published == nil {
			rows.Close()
			return empty, errs.ErrUnavailable
		}
		workflows = append(workflows, item)
		digests = append(digests, digest)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	next := ""
	if len(workflows) > 10 {
		workflows = workflows[:10]
		digests = digests[:10]
		next = encodeCatalogCursor(actor, "EXECUTION_WORKFLOW", filter, workflows[9].Ref)
	}
	selected := make([]*entity.Workflow, len(workflows))
	for i := range workflows {
		selected[i] = &workflows[i]
	}
	if err := repository.projectWorkflowLaunchReadiness(ctx, tx, actor, selected); err != nil {
		return empty, err
	}
	result := entity.ExecutionWorkflowCatalog{NextPageToken: next}
	for i, item := range workflows {
		if item.LaunchReadiness == nil {
			return empty, errs.ErrUnavailable
		}
		result.Items = append(result.Items, entity.ExecutionWorkflowCatalogEntry{WorkflowRef: item.Ref, Name: item.Published.Name, Purpose: item.Published.Purpose, WorkflowVersion: item.Version, PublishedRef: item.Published.Ref, SpecDigest: digests[i], Inputs: item.Published.Inputs, Readiness: *item.LaunchReadiness})
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return result, nil
}
