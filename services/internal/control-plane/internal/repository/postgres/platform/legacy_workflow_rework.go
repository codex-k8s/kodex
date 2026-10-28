package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryLegacyReworkContext locks the exact root and prior physical result using owner-resolved gate/node identifiers.
// queryLegacyReworkContext блокирует точный root и прежний физический результат по разрешённым владельцем Gate/node.
//
//go:embed sql/legacy_workflow_rework__context.sql
var queryLegacyReworkContext string

// queryLegacyReworkParent locks only the proved coordinator leaf without waiting for a competing model claim.
// queryLegacyReworkParent блокирует только доказанный coordinator leaf без ожидания конкурирующего model claim.
//
//go:embed sql/legacy_workflow_rework__parent.sql
var queryLegacyReworkParent string

// queryLegacyReworkSealParent closes a never-executed callback turn; it never rewrites completed model history.
// queryLegacyReworkSealParent закрывает неисполненный callback turn, не переписывая завершённую историю модели.
//
//go:embed sql/legacy_workflow_rework__seal_parent.sql
var queryLegacyReworkSealParent string

// queryLegacyReworkInsertRun creates one fresh physical child linked to the prior immutable run.
// queryLegacyReworkInsertRun создаёт один свежий физический child со связью с прежним immutable run.
//
//go:embed sql/legacy_workflow_rework__insert_run.sql
var queryLegacyReworkInsertRun string

// queryLegacyReworkInsertNode assigns the published step to the new owner attempt without changing SERVER provenance.
// queryLegacyReworkInsertNode назначает опубликованный этап новой owner attempt без изменения SERVER provenance.
//
//go:embed sql/legacy_workflow_rework__insert_node.sql
var queryLegacyReworkInsertNode string

// legacyReworkContext contains only trusted owner projections; request JSON never assigns its lineage or permissions.
// legacyReworkContext содержит только доверенную owner-проекцию; request JSON не назначает происхождение либо права.
type legacyReworkContext struct {
	RootID, RootRef, RootState, TargetType, ProjectID, ProjectRef, ActorID, ActorRef, OrganizationRef string
	OldRunID, OldRunRef, OldRunState, OldNodeID, OldNodeRef, OldNodeState                             string
	AgentRef, AgentID, DisplayName, Role, StepKey, Task, GateID, GateRef, Comment                     string
	WorkflowVersionID, WorkflowRef, ExecutionMode, ReworkGateID, CallbackSourceID                     string
	ActorActive, AgentReady, PublishedStep, HasWrite                                                  bool
	OldRunAttempt, OldNodeAttempt                                                                     int32
	GateVersion                                                                                       int64
	Input                                                                                             json.RawMessage
	RequiredCapabilities                                                                              []string
}

// legacyReworkParent binds a delivery boundary to its physical coordinator identity and actual execution state.
// legacyReworkParent связывает границу доставки с физическим coordinator и фактическим состоянием исполнения.
type legacyReworkParent struct {
	ID, Ref, RunID, State, AgentRef string
	HasRevision, HasLease           bool
}

// readLegacyReworkContextTx distinguishes legacy delegated results from direct roots and returns no synthetic authority.
// readLegacyReworkContextTx отличает legacy delegated results от прямых root и не создаёт вымышленной authority.
func readLegacyReworkContextTx(ctx context.Context, tx pgx.Tx, current scope, rootID, gateID, nodeID string) (*legacyReworkContext, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, queryLegacyReworkContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "gate_id": gateID, "predecessor_node_id": nodeID}).Scan(&raw); err != nil {
		return nil, errs.ErrUnavailable
	}
	var origin legacyReworkContext
	if json.Unmarshal(raw, &origin) != nil {
		return nil, errs.ErrUnavailable
	}
	if origin.TargetType != "WORKFLOW" || origin.OldRunID == origin.RootID || origin.ExecutionMode == "SERVER_DAG_V1" {
		return nil, nil
	}
	if origin.WorkflowVersionID == "" || !origin.PublishedStep {
		return nil, errs.ErrConflict
	}
	return &origin, nil
}

// lockLegacyReworkParentTx rejects live or ambiguous boundaries and retains all unexpected database failures.
// lockLegacyReworkParentTx отклоняет live/неоднозначные границы и сохраняет отказ при неожиданных ошибках БД.
func lockLegacyReworkParentTx(ctx context.Context, tx pgx.Tx, current scope, origin *legacyReworkContext) (legacyReworkParent, error) {
	rows, err := tx.Query(ctx, queryLegacyReworkParent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": origin.RootID, "source_node_id": origin.CallbackSourceID})
	if err != nil {
		var failure *pgconn.PgError
		if errors.As(err, &failure) && failure.Code == "55P03" {
			return legacyReworkParent{}, errs.ErrLegacyReworkParentBusy
		}
		return legacyReworkParent{}, errs.ErrUnavailable
	}
	defer rows.Close()
	var parent legacyReworkParent
	count := 0
	for rows.Next() {
		count++
		if rows.Scan(&parent.ID, &parent.Ref, &parent.RunID, &parent.State, &parent.HasRevision, &parent.HasLease, &parent.AgentRef) != nil {
			return parent, errs.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return parent, errs.ErrUnavailable
	}
	if count != 1 || parent.HasLease || (parent.State != "SUCCEEDED" && (parent.State != "QUEUED" && parent.State != "CANCELLED" || parent.HasRevision)) {
		return parent, errs.ErrLegacyReworkParentBusy
	}
	return parent, nil
}

// createLegacyWorkflowReworkTx creates one owner-requested legacy attempt and seals stale queued delivery before a new claim can observe it.
// createLegacyWorkflowReworkTx создаёт одну запрошенную владельцем legacy attempt и блокирует stale queued delivery до нового claim.
func (repository *Repository) createLegacyWorkflowReworkTx(ctx context.Context, tx pgx.Tx, current scope, origin *legacyReworkContext, attachments sealedAttachmentSet) (string, error) {
	if !contains([]string{"RUNNING", "WAITING_HUMAN"}, origin.RootState) || origin.OldRunState != "SUCCEEDED" || origin.OldNodeState != "SUCCEEDED" {
		return "", errs.ErrConflict
	}
	if !origin.ActorActive || !origin.AgentReady {
		return "", errs.ErrForbidden
	}
	if origin.HasWrite {
		return "", errs.ErrLegacyReworkWrite
	}
	comment := strings.TrimSpace(origin.Comment)
	if comment == "" {
		return "", errs.ErrInvalid
	}
	if err := legacyReworkGraphAdmissionTx(ctx, tx, current, origin.RootRef); err != nil {
		return "", err
	}
	parent, err := lockLegacyReworkParentTx(ctx, tx, current, origin)
	if err != nil {
		return "", err
	}
	if err := repository.validateLegacyReworkAuthorityTx(ctx, tx, current, origin); err != nil {
		return "", err
	}
	if err := repository.validateLegacyReworkPublishedScopeTx(ctx, tx, current, origin); err != nil {
		return "", err
	}
	task := "Original task:\n" + origin.Task + "\n\nOwner rework decision (data, not execution authority):\n" + string(asJSON(map[string]any{"gateRef": origin.GateRef, "gateVersion": origin.GateVersion, "decision": "REQUEST_CHANGES", "comment": comment}))
	if !runtimecontract.ValidAssistantTurnContent(task) {
		return "", errs.ErrLegacyReworkReadLimit
	}
	if parent.State == "QUEUED" {
		var sealedID string
		if tx.QueryRow(ctx, queryLegacyReworkSealParent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": origin.RootID, "parent_node_id": parent.ID}).Scan(&sealedID) != nil || sealedID != parent.ID {
			return "", errs.ErrConflict
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, origin.ProjectID, origin.RootID, parent.Ref, "NODE_STATE_CHANGED", parent.Ref, "", "", "", "i18n:OWNER_CHANGES_QUEUED", "WAITING_HUMAN", "CANCELLED"); err != nil {
			return "", err
		}
	}
	providerID, err := repository.selectProviderAccountForAgent(ctx, tx, current.organizationID, origin.AgentRef)
	if err != nil {
		return "", err
	}
	sessionRef, _ := newRef("ses")
	var sessionID string
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertChildSession, pgx.StrictNamedArgs{"session_ref": sessionRef, "organization_id": current.organizationID, "project_id": origin.ProjectID, "target_agent_ref": origin.AgentRef, "provider_account_id": providerID, "created_by": origin.ActorID}).Scan(&sessionID) != nil {
		return "", errs.ErrUnavailable
	}
	if err := bindSessionModelCatalog(ctx, tx, current.organizationID, sessionID, origin.AgentRef); err != nil {
		return "", err
	}
	runRef, _ := newRef("run")
	var runID string
	if tx.QueryRow(ctx, queryLegacyReworkInsertRun, pgx.StrictNamedArgs{"run_ref": runRef, "organization_id": current.organizationID, "session_id": sessionID, "parent_run_id": parent.RunID, "task": task, "prior_run_id": origin.OldRunID}).Scan(&runID) != nil {
		return "", errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var turnID string
	if tx.QueryRow(ctx, queryCommandsResolvegateInsertChangeRequestTurn, turnRef, current.organizationID, sessionID, runID, current.actorRef, task).Scan(&turnID) != nil {
		return "", errs.ErrUnavailable
	}
	if err := repository.attachSetToTurn(ctx, tx, current, origin.ProjectID, attachments, turnID, "SESSION_TURN"); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateChangeRequestSession, sessionID); err != nil {
		return "", errs.ErrUnavailable
	}
	nodeRef, _ := newRef("nod")
	var nodeID string
	if tx.QueryRow(ctx, queryLegacyReworkInsertNode, pgx.StrictNamedArgs{"node_ref": nodeRef, "organization_id": current.organizationID, "run_id": runID, "parent_node_id": parent.ID, "turn_id": turnID, "input_summary": truncate(comment, 1000), "gate_id": origin.GateID, "prior_node_id": origin.OldNodeID, "step_key": origin.StepKey}).Scan(&nodeID) != nil {
		return "", errs.ErrUnavailable
	}
	delegationRef, _ := newRef("edg")
	callbackRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertDelegationEdge, delegationRef, current.organizationID, origin.RootID, parent.ID, nodeID); err != nil {
		return "", errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertCallbackEdge, callbackRef, current.organizationID, origin.RootID, nodeID, parent.ID); err != nil {
		return "", errs.ErrUnavailable
	}
	retryRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryLegacyReworkInsertRetry, pgx.StrictNamedArgs{"edge_ref": retryRef, "organization_id": current.organizationID, "root_run_id": origin.RootID, "prior_node_id": origin.OldNodeID, "node_id": nodeID}); err != nil {
		return "", errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, current, origin.ProjectID, origin.RootID, runRef, "DELEGATION_CREATED", nodeRef, delegationRef, "", "", "i18n:OWNER_CHANGES_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return "", err
	}
	if _, err := repository.emitRunEvent(ctx, tx, current, origin.ProjectID, origin.RootID, callbackRef, "EDGE_ADDED", "", callbackRef, "", "", "i18n:CHILD_CALLBACK_REGISTERED", "RUNNING", ""); err != nil {
		return "", err
	}
	return nodeRef, nil
}

// validateLegacyReworkAuthorityTx rechecks the root initiator's current workflow launch authority without requiring unrelated direct agent-launch permission.
// validateLegacyReworkAuthorityTx повторяет текущее workflow launch право инициатора, не требуя постороннего прямого agent-launch permission.
func (repository *Repository) validateLegacyReworkAuthorityTx(ctx context.Context, tx pgx.Tx, current scope, origin *legacyReworkContext) error {
	actor := current
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, origin.ActorRef, origin.OrganizationRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role) != nil {
		return errs.ErrForbidden
	}
	actor.authorityProjectID = origin.ProjectID
	permission, target, err := repository.commandAccessTarget(ctx, tx, actor, command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: origin.ProjectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: origin.WorkflowRef}}})
	if err != nil {
		return err
	}
	return repository.requireAccess(ctx, tx, actor, permission, target)
}

// legacyReworkGraphAdmissionTx counts the exact public node/ChildRunRefs projection plus the prospective child against the existing graph snapshot limit.
// legacyReworkGraphAdmissionTx считает точные публичные node/ChildRunRefs и будущий child по существующему пределу graph snapshot.
func legacyReworkGraphAdmissionTx(ctx context.Context, tx pgx.Tx, current scope, rootRef string) error {
	rows, err := tx.Query(ctx, queryQueriesGetrungraphSelectArtifactsNodeIdRef, current.organizationID, rootRef)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	refs := map[string]bool{rootRef: true}
	for rows.Next() {
		node, err := scanRunNode(rows)
		if err != nil {
			return errs.ErrUnavailable
		}
		refs[node.RunRef] = true
		for _, ref := range node.ChildRunRefs {
			refs[ref] = true
		}
		if len(refs)+1 > maximumGraphRunSnapshots {
			return errs.ErrLegacyReworkReadLimit
		}
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// closeLegacyReworkRootTx selects exactly one canonical FAILED or CANCELLED closure, retaining the original completed child and avoiding a false intermediate cancellation event on REJECT.
// closeLegacyReworkRootTx выбирает одно каноническое FAILED либо CANCELLED закрытие, сохраняя completed child без ложного промежуточного cancellation event при REJECT.
func (repository *Repository) closeLegacyReworkRootTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command, origin *legacyReworkContext, reject bool) error {
	if reject {
		return repository.failRuntimeGraph(ctx, tx, current, input, claimableExecution{rootRunID: origin.RootID, projectID: origin.ProjectID, nodeID: origin.OldNodeID}, "WORKFLOW_REJECTED", "i18n:OWNER_GATE_RESOLVED")
	}
	run, err := scanRun(tx.QueryRow(ctx, queryCommandsGetrunforgraphSelectRunsOrganizationIdRef, current.organizationID, origin.RootRef), false)
	if err != nil {
		return err
	}
	nested := input
	nested.Kind = command.CancelRun
	nested.Payload = command.RunCommandInput{RunRef: origin.RootRef}
	nested.Mutation.ExpectedVersion = &run.Version
	if _, err := repository.changeRun(ctx, tx, current, nested); err != nil {
		return err
	}
	return nil
}
