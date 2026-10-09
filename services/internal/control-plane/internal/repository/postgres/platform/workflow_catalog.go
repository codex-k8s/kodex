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

//go:embed sql/workflow_catalog__entries.sql
var queryWorkflowCatalogEntries string

//go:embed sql/workflow_launch__published_pins.sql
var queryWorkflowLaunchPublishedPins string

// scanWorkflow сохраняет единый decoder опубликованной schema; дополнительный
// digest принадлежит immutable owner row, не draft/spec JSON.
type workflowCatalogRow struct {
	row    rowScanner
	digest *string
}

func (row workflowCatalogRow) Scan(dest ...any) error {
	return row.row.Scan(append(dest, row.digest)...)
}

func (repository *Repository) GetExecutionWorkflowCatalog(ctx context.Context, p value.Principal, input query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	return retryAssistantLockedRead(ctx, func(attempt context.Context) (entity.ExecutionWorkflowCatalog, error) {
		return repository.executionWorkflowCatalogOnce(attempt, p, input)
	})
}

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
		if err != nil || !workflowSpecDigestPattern.MatchString(digest) || item.Published == nil {
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
