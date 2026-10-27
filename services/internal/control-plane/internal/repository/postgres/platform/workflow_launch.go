package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// workflowLaunchRefPattern accepts only 8 to 96 ASCII letters, digits, underscores or hyphens for
// required-workflow reference guards. It is a syntax predicate, not existence, publication,
// ownership or delegation proof.
// workflowLaunchRefPattern принимает только 8–96 ASCII-букв, цифр, подчёркиваний либо дефисов для
// required-workflow reference guards. Это синтаксический predicate, а не доказательство
// существования, публикации, владения или делегирования.
var workflowLaunchRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// validateRequiredWorkflowAuthorities collects required-workflow ancestor origins in the caller
// transaction, closes rows and then freshly resolves each origin actor through the canonical
// active-subject/access-role query. It pins project scope, requires effective platform.run.launch
// and authorizes the specialized workflow LaunchRun target. Stored origins locate authority but do
// not replace fresh checks; scan failure is Unavailable, missing current actor/capability/access
// is Forbidden. It neither claims nor launches work.
// validateRequiredWorkflowAuthorities собирает required-workflow ancestor origins в транзакции
// вызывающего, закрывает rows, затем заново разрешает каждого origin actor через канонический
// active-subject/access-role query. Закрепляет project scope, требует effective
// platform.run.launch и авторизует специализированный workflow LaunchRun target. Сохранённые
// origins задают locator полномочий, но не заменяют свежую проверку; scan failure даёт
// Unavailable, отсутствие текущего actor/capability/access — Forbidden. Не выполняет claim или
// launch.
func (repository *Repository) validateRequiredWorkflowAuthorities(ctx context.Context, tx pgx.Tx, machine scope, rootID string) error {
	rows, err := tx.Query(ctx, queryWorkflowLaunchClaimAuthorities, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "root_run_id": rootID})
	if err != nil {
		return errs.ErrUnavailable
	}
	// authority stores one ancestor origin projection with actor, organization, project, agent
	// capabilities and workflow ref until query rows close. These local fields are only inputs to
	// fresh actor/capability/resource checks and are never a caller-issued grant.
	// authority хранит одну ancestor origin projection с actor, organization, project, agent
	// capabilities и workflow ref до закрытия query rows. Локальные поля служат только входами свежих
	// actor/capability/resource checks и не являются выданным caller grant.
	type authority struct {
		actorRef, orgRef, projectID, projectRef, agentRef, workflowRef string
		capabilities                                                   []string
	}
	var origins []authority
	for rows.Next() {
		var origin authority
		if rows.Scan(&origin.actorRef, &origin.orgRef, &origin.projectID, &origin.projectRef, &origin.agentRef, &origin.capabilities, &origin.workflowRef) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		origins = append(origins, origin)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, origin := range origins {
		actor := machine
		if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, origin.actorRef, origin.orgRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role) != nil {
			return errs.ErrForbidden
		}
		actor.authorityProjectID = origin.projectID
		effective, _, err := repository.agentCapabilityAuthority(ctx, tx, actor, origin.projectRef, origin.agentRef, origin.capabilities)
		if err != nil {
			return err
		}
		if !capabilityEnabled(effective, "platform.run.launch") {
			return errs.ErrForbidden
		}
		canonical := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: origin.projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: origin.workflowRef}}}
		permission, target, err := repository.commandAccessTarget(ctx, tx, actor, canonical)
		if err != nil {
			return err
		}
		if repository.requireAccess(ctx, tx, actor, permission, target) != nil {
			return errs.ErrForbidden
		}
	}
	return nil
}

// validateWorkflowLaunchAuthority validates the specialized payload type and expected
// published-ref/digest/version shape, resolves its live lease/fence/generation origin with owner
// SQL and refreshes the root actor and effective launch capability. It then checks the exact
// workflow LaunchRun target before receipt replay. No rows/access denial is hidden as NotFound,
// actor/capability denial is Forbidden and backend failure is Unavailable. It does not itself
// compare current published pins or materialize a child.
// validateWorkflowLaunchAuthority проверяет специализированный payload type и форму expected
// published-ref/digest/version, разрешает live lease/fence/generation origin через owner SQL и
// обновляет root actor с effective launch capability. Затем проверяет точный workflow LaunchRun
// target до receipt replay. Отсутствие строки/доступа скрывается как NotFound, actor/capability
// denial даёт Forbidden, backend failure — Unavailable. Сам не сравнивает текущие published pins и
// не материализует child.
func (repository *Repository) validateWorkflowLaunchAuthority(ctx context.Context, tx pgx.Tx, machine scope, input command.Command) error {
	payload, ok := input.Payload.(command.LaunchWorkflowInput)
	if !ok || !workflowLaunchRefPattern.MatchString(payload.ExpectedPublishedRef) || !exactSHA256(payload.ExpectedSpecDigest) || payload.ExpectedWorkflowVersion < 1 {
		return errs.ErrInvalid
	}
	fence := sha256.Sum256([]byte(payload.Fence))
	actor := machine
	var rootID, runID, nodeID, sessionID, turnID, revisionID, projectID, projectRef, agentRef, inputDigest, revisionDigest string
	var capabilities []string
	var attempt int32
	err := tx.QueryRow(ctx, queryWorkflowLaunchResolveOrigin, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "lease_ref": payload.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "generation": payload.Generation}).Scan(&rootID, &runID, &nodeID, &sessionID, &turnID, &revisionID, &projectID, &projectRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role, &actor.organizationRef, &agentRef, &capabilities, &attempt, &inputDigest, &revisionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, actor.actorRef, actor.organizationRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role) != nil {
		return errs.ErrForbidden
	}
	actor.authorityProjectID = projectID
	effective, _, err := repository.agentCapabilityAuthority(ctx, tx, actor, projectRef, agentRef, capabilities)
	if err != nil {
		return err
	}
	if !capabilityEnabled(effective, "platform.run.launch") {
		return errs.ErrForbidden
	}
	canonical := input
	canonical.Kind = command.LaunchRun
	canonical.Payload = command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: payload.WorkflowRef}}
	permission, target, err := repository.commandAccessTarget(ctx, tx, actor, canonical)
	if err != nil {
		return err
	}
	if repository.requireAccess(ctx, tx, actor, permission, target) != nil {
		return errs.ErrNotFound
	}
	return nil
}

// lockWorkflowLaunchProjects derives a locator/project from the closed command payload types and
// locks exact related projects using stable SQL advisory-lock order. ClaimExecution and
// queued-provider cancellation include active required-workflow/root projects; a command without a
// relevant locator returns nil. Scan/query failures are Unavailable. It locks within the existing
// transaction and returns project IDs without a global lock, resource authorization or commit.
// lockWorkflowLaunchProjects выводит locator/project из закрытых command payload types и блокирует
// точные связанные projects в устойчивом SQL-порядке advisory locks. ClaimExecution и
// queued-provider cancellation включают активные required-workflow/root projects; команда без
// подходящего locator возвращает nil. Scan/query failures дают Unavailable. Блокирует внутри
// существующей транзакции и возвращает project IDs без global lock, resource authorization или
// commit.
func lockWorkflowLaunchProjects(ctx context.Context, tx pgx.Tx, current scope, input command.Command) ([]string, error) {
	locator, projectRef := "", ""
	switch payload := input.Payload.(type) {
	case command.LaunchWorkflowInput:
		locator = payload.LeaseRef
	case command.LeaseInput:
		locator = payload.LeaseRef
	case command.CompleteExecutionInput:
		locator = payload.LeaseRef
	case command.DelegateInput:
		locator = payload.LeaseRef
	case command.RunCommandInput:
		locator = payload.RunRef
	case command.GateResolutionInput:
		locator = payload.GateRef
	case command.LaunchRunInput:
		projectRef = payload.ProjectRef
	case command.ProjectLifecycleInput:
		projectRef = payload.Ref
	case command.SessionTurnInput:
		locator = payload.RunRef
		if locator == "" {
			locator = payload.SessionRef
		}
	case command.RunToolCallInput:
		locator = payload.LeaseRef
	}
	allRelated := input.Kind == command.ClaimExecution || input.Kind == command.CancelProviderAccountQueuedWork
	if locator == "" && projectRef == "" && !allRelated {
		return nil, nil
	}
	rows, err := tx.Query(ctx, queryWorkflowLaunchLockProjects, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": projectRef, "locator": locator, "claim": allRelated})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var id string
		var ignored any
		if rows.Scan(&id, &ignored) != nil {
			return nil, errs.ErrUnavailable
		}
		projects = append(projects, id)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return projects, nil
}

// launchWorkflowExecution validates bounded required-workflow input, resolves and locks the live
// lease origin, refreshes the root actor/capabilities and authorizes the specialized workflow
// target. Exact accepted intent returns the previous child receipt; a new intent must match
// current published pins and tree bounds before canonical launchRun. It binds child/parent, proxy,
// delegation/callback edges and origin pins, emits events and reads the graph in the caller
// transaction. Reference-generation errors remain ignored as before. No commit, retry or external
// provider action occurs here; any partial SQL work requires caller rollback on error.
// launchWorkflowExecution проверяет ограниченный required-workflow input, разрешает и блокирует
// live lease origin, обновляет root actor/capabilities и авторизует специализированную workflow
// target. Точный принятый intent возвращает прежний child receipt; новый intent обязан совпасть с
// current published pins и tree bounds до canonical launchRun. Связывает child/parent, proxy,
// delegation/callback edges и origin pins, создаёт events и читает graph в транзакции вызывающего.
// Ошибки генерации refs по-прежнему игнорируются. Commit, retry или внешний provider action здесь
// не выполняются; частичная SQL-работа при ошибке требует rollback вызывающего.
func (repository *Repository) launchWorkflowExecution(ctx context.Context, tx pgx.Tx, machine scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LaunchWorkflowInput)
	if !ok || !runtimecontract.ValidAssistantTurnContent(payload.Task) || !workflowLaunchRefPattern.MatchString(payload.WorkflowRef) || !workflowLaunchRefPattern.MatchString(payload.ExpectedPublishedRef) || !exactSHA256(payload.ExpectedSpecDigest) || payload.ExpectedWorkflowVersion < 1 || len([]rune(payload.Title)) > 240 || !validBoundedRunInput(payload.Input) {
		return commandOutcome{}, errs.ErrInvalid
	}
	fence := sha256.Sum256([]byte(payload.Fence))
	actor := machine
	var rootID, runID, nodeID, sessionID, turnID, revisionID, projectID, projectRef, agentRef, inputDigest, revisionDigest string
	var capabilities []string
	var attempt int32
	err := tx.QueryRow(ctx, queryWorkflowLaunchResolveOrigin, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "lease_ref": payload.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "generation": payload.Generation}).Scan(&rootID, &runID, &nodeID, &sessionID, &turnID, &revisionID, &projectID, &projectRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role, &actor.organizationRef, &agentRef, &capabilities, &attempt, &inputDigest, &revisionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	// Root identity и роль разрешаются тем же canonical access registry, не
	// старой membership-проекцией и не служебным principal.
	if err = tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, actor.actorRef, actor.organizationRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role); err != nil {
		return commandOutcome{}, errs.ErrForbidden
	}
	actor.authorityProjectID = projectID
	currentCapabilities, _, err := repository.agentCapabilityAuthority(ctx, tx, actor, projectRef, agentRef, capabilities)
	if err != nil {
		return commandOutcome{}, err
	}
	if !capabilityEnabled(currentCapabilities, "platform.run.launch") {
		return commandOutcome{}, errs.ErrForbidden
	}
	launchInput := command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: payload.WorkflowRef}, Task: payload.Task, Title: payload.Title, TitleSource: "AGENT_PROPOSED", Source: "AGENT_DELEGATION", Input: payload.Input}
	canonical := input
	canonical.Kind = command.LaunchRun
	canonical.Payload = launchInput
	permission, target, err := repository.commandAccessTarget(ctx, tx, actor, canonical)
	if err != nil {
		return commandOutcome{}, err
	}
	if repository.requireAccess(ctx, tx, actor, permission, target) != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	requestHash := sha256.Sum256(asJSON(struct {
		Workflow, Task, Title, PublishedRef, SpecDigest string
		WorkflowVersion                                 int64
		Input                                           map[string]any
	}{payload.WorkflowRef, payload.Task, payload.Title, payload.ExpectedPublishedRef, payload.ExpectedSpecDigest, payload.ExpectedWorkflowVersion, payload.Input}))
	requestDigest := hex.EncodeToString(requestHash[:])
	var childRef, launchRef, callbackRef string
	err = tx.QueryRow(ctx, queryWorkflowLaunchExisting, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "revision_id": revisionID, "workflow_ref": payload.WorkflowRef, "request_digest": requestDigest}).Scan(&childRef, &launchRef, &callbackRef)
	if err == nil {
		child, graph, readErr := repository.readRunGraphTx(ctx, tx, actor, childRef)
		return commandOutcome{result: command.Result{Run: &child, Graph: &graph, Runtime: map[string]any{"launchRef": launchRef, "callbackEdgeRef": callbackRef}}, projectID: projectID, projectRef: projectRef, resourceKind: "RUN", resourceRef: childRef, summary: "i18n:CHILD_RUN_DELEGATED"}, readErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	// Accepted exact intent выше возвращает прежнюю квитанцию. Новый intent
	// блокирует owner row и сравнивает exact published pins до materialization.
	var publishedRef, specDigest string
	var workflowVersion int64
	err = tx.QueryRow(ctx, queryWorkflowLaunchPublishedPins, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "project_id": projectID, "workflow_ref": payload.WorkflowRef}).Scan(&publishedRef, &specDigest, &workflowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if publishedRef != payload.ExpectedPublishedRef || specDigest != payload.ExpectedSpecDigest || workflowVersion != payload.ExpectedWorkflowVersion {
		return commandOutcome{}, errs.ErrConflict
	}
	var bounded bool
	if tx.QueryRow(ctx, queryWorkflowLaunchBound, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "root_run_id": rootID}).Scan(&bounded) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if !bounded {
		return commandOutcome{}, errs.ErrConflict
	}
	outcome, err := repository.launchRun(ctx, tx, actor, canonical)
	if err != nil {
		return commandOutcome{}, err
	}
	childRef = outcome.result.Run.Ref
	if tag, err := tx.Exec(ctx, queryWorkflowLaunchBindParent, pgx.StrictNamedArgs{"organization_id": machine.organizationID, "parent_run_id": runID, "child_ref": childRef}); err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	proxyRef, _ := newRef("nod")
	var proxyID string
	if tx.QueryRow(ctx, queryWorkflowLaunchInsertProxy, pgx.StrictNamedArgs{"node_ref": proxyRef, "organization_id": machine.organizationID, "root_run_id": rootID, "run_id": runID, "parent_node_id": nodeID, "name": truncate(outcome.result.Run.Title, 160), "child_ref": childRef}).Scan(&proxyID) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	delegatedRef, _ := newRef("edg")
	if _, err = tx.Exec(ctx, queryRuntimeDelegateexecutionInsertDelegationEdge, delegatedRef, machine.organizationID, rootID, nodeID, proxyID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	callbackRef, _ = newRef("edg")
	if _, err = tx.Exec(ctx, queryRuntimeDelegateexecutionInsertCallbackEdge, callbackRef, machine.organizationID, rootID, proxyID, nodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	launchRef, _ = newRef("wlaunch")
	tag, err := tx.Exec(ctx, queryWorkflowLaunchInsert, pgx.StrictNamedArgs{"launch_ref": launchRef, "organization_id": machine.organizationID, "project_id": projectID, "actor_id": actor.actorID, "root_run_id": rootID, "run_id": runID, "node_id": nodeID, "session_id": sessionID, "turn_id": turnID, "revision_id": revisionID, "generation": payload.Generation, "attempt": attempt, "input_digest": inputDigest, "revision_digest": revisionDigest, "workflow_ref": payload.WorkflowRef, "request_digest": requestDigest, "child_ref": childRef, "proxy_node_id": proxyID, "callback_ref": callbackRef})
	if err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err = repository.emitRunEvent(ctx, tx, actor, projectID, rootID, childRef, "DELEGATION_CREATED", proxyRef, delegatedRef, "", "", "i18n:CHILD_AGENT_STARTED", "RUNNING", "WAITING"); err != nil {
		return commandOutcome{}, err
	}
	if _, err = repository.emitRunEvent(ctx, tx, actor, projectID, rootID, callbackRef, "EDGE_ADDED", "", callbackRef, "", "", "i18n:CHILD_CALLBACK_REGISTERED", "RUNNING", ""); err != nil {
		return commandOutcome{}, err
	}
	child, graph, err := repository.readRunGraphTx(ctx, tx, actor, childRef)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Run = &child
	outcome.result.Graph = &graph
	outcome.result.Runtime = map[string]any{"launchRef": launchRef, "callbackEdgeRef": callbackRef}
	return outcome, nil
}

// requiredWorkflowTerminal holds a locked OPEN required-workflow launch projection with
// origin/parent/child/proxy/callback identities, child version/state and summary. It is temporary
// reconciliation input; the fields do not independently authorize cancellation, prove callback
// delivery or mark a transaction committed.
// requiredWorkflowTerminal хранит заблокированную OPEN required-workflow launch projection с
// origin/parent/child/proxy/callback identities, child version/state и summary. Это временный вход
// reconciliation; поля отдельно не авторизуют отмену, не доказывают callback delivery и не
// отмечают commit транзакции.
type requiredWorkflowTerminal struct {
	id, rootID, parentRunID, parentNodeID, parentNodeRef, projectID, childID, childRef, childState, summary, parentState, proxyID, proxyRef, edgeID, edgeRef string
	version                                                                                                                                                  int64
}

// reconcileWorkflowLaunches reconciles only supplied projects in the caller transaction for at
// most 128 rounds, closing terminal graphs before locking up to 129 open terminal-related
// launches. A terminal parent cancels a live required child with its current version; completion
// closes the proxy and emits its state event, while only a nonterminal parent receives the child
// callback. Empty work succeeds, excessive batch/rounds is Conflict and failures stop for caller
// rollback. It does not mark the parent successful merely because the child completed or commit
// separately from the command receipt.
// reconcileWorkflowLaunches сверяет только переданные projects в транзакции вызывающего максимум
// за 128 раундов, закрывая terminal graphs до блокировки до 129 открытых terminal-related
// launches. Terminal parent отменяет живого required child с его текущей version; completion
// закрывает proxy и создаёт его state event, а child callback получает только nonterminal parent.
// Пустая работа успешна, чрезмерные batch/rounds дают Conflict, ошибка останавливает путь для
// rollback вызывающего. Не завершает parent успешно лишь из-за завершения child и не фиксируется
// отдельно от command receipt.
func (repository *Repository) reconcileWorkflowLaunches(ctx context.Context, tx pgx.Tx, current scope, projects []string) error {
	if len(projects) == 0 {
		return nil
	}
	for round := 0; round < 128; round++ {
		if err := repository.closeRequiredWorkflowTerminalGraphs(ctx, tx, current, projects); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, queryWorkflowLaunchTerminal, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ids": projects})
		if err != nil {
			return errs.ErrUnavailable
		}
		var items []requiredWorkflowTerminal
		for rows.Next() {
			var item requiredWorkflowTerminal
			if rows.Scan(&item.id, &item.rootID, &item.parentRunID, &item.parentNodeID, &item.parentNodeRef, &item.projectID, &item.childID, &item.childRef, &item.childState, &item.version, &item.summary, &item.parentState, &item.proxyID, &item.proxyRef, &item.edgeID, &item.edgeRef) != nil {
				rows.Close()
				return errs.ErrUnavailable
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return errs.ErrUnavailable
		}
		if len(items) == 0 {
			return nil
		}
		if len(items) > 128 {
			return errs.ErrConflict
		}
		for _, item := range items {
			parentTerminal := contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, item.parentState)
			if parentTerminal && !contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, item.childState) {
				version := item.version
				nested := command.Command{Kind: command.CancelRun, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.RunCommandInput{RunRef: item.childRef}}
				if _, err = repository.changeRun(ctx, tx, current, nested); err != nil {
					return err
				}
				item.childState = "CANCELLED"
			}
			var proxyClosed bool
			if err = tx.QueryRow(ctx, queryWorkflowLaunchComplete, pgx.StrictNamedArgs{"launch_id": item.id, "state": item.childState}).Scan(&proxyClosed); err != nil {
				return errs.ErrUnavailable
			}
			if proxyClosed {
				if _, err = repository.emitRunEvent(ctx, tx, current, item.projectID, item.rootID, item.proxyRef, "NODE_STATE_CHANGED", item.proxyRef, "", "", "", "i18n:CHILD_AGENT_RESULT_DELIVERED", item.parentState, item.childState); err != nil {
					return err
				}
			}
			if !parentTerminal {
				summary := fmt.Sprintf("Workflow %s completed: %s. %s", item.childRef, item.childState, truncate(item.summary, 3000))
				if _, err = repository.recordChildCallback(ctx, tx, current, callbackRecord{childRunID: item.childID, childRunRef: item.childRef, rootRunID: item.rootID, projectID: item.projectID, parentRunID: item.parentRunID, resultSummary: summary, callbackEdgeID: item.edgeID, callbackEdgeRef: item.edgeRef, parentNodeID: item.parentNodeID, parentNodeRef: item.parentNodeRef}); err != nil {
					return err
				}
			}
		}
	}
	return errs.ErrConflict
}

// closeRequiredWorkflowTerminalGraphs reads terminal required-workflow roots for the selected
// tenant/projects, closes query rows and applies the canonical terminal-graph SQL to each root.
// That SQL cancels live runs/nodes/turns/leases/gates and preserves UNKNOWN_OUTCOME for uncertain
// claimed effects/deliveries. It emits only returned RUN/NODE/GATE transitions with the existing
// state mapping; repeat reads with no transitions add no events. Errors propagate without commit,
// retry or a partial successful envelope.
// closeRequiredWorkflowTerminalGraphs читает terminal required-workflow roots выбранных
// tenant/projects, закрывает query rows и применяет canonical terminal-graph SQL к каждому root.
// SQL отменяет живые runs/nodes/turns/leases/gates и сохраняет UNKNOWN_OUTCOME для неопределённых
// claimed effects/deliveries. Создаёт только возвращённые RUN/NODE/GATE transitions с прежним
// state mapping; повторное чтение без transitions не добавляет events. Ошибки передаются без
// commit, retry или частично успешной оболочки.
func (repository *Repository) closeRequiredWorkflowTerminalGraphs(ctx context.Context, tx pgx.Tx, current scope, projects []string) error {
	rows, err := tx.Query(ctx, queryWorkflowLaunchTerminalRoots, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ids": projects})
	if err != nil {
		return errs.ErrUnavailable
	}
	// terminalRoot retains root UUID/ref, project UUID and terminal state after the root query closes,
	// allowing the canonical cleanup and event mapper to use the same selected owner projection. It is
	// not a new grant or durable receipt.
	// terminalRoot сохраняет root UUID/ref, project UUID и terminal state после закрытия root query,
	// чтобы canonical cleanup и event mapper использовали одну выбранную owner projection. Не является
	// новым grant или durable receipt.
	type terminalRoot struct{ id, ref, project, state string }
	var roots []terminalRoot
	for rows.Next() {
		var root terminalRoot
		if rows.Scan(&root.id, &root.ref, &root.project, &root.state) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		roots = append(roots, root)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, root := range roots {
		transitions, err := tx.Query(ctx, queryRuntimeCompleteexecutionCloseTerminalGraph, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.id, "actor_id": current.actorID})
		if err != nil {
			return errs.ErrUnavailable
		}
		// transition retains one returned cleanup kind/ref/node triple after transition rows close. The
		// surrounding mapper selects the existing event kind and node/gate state; this local record does
		// not add a transition or authorize a different resource.
		// transition сохраняет одну возвращённую cleanup kind/ref/node тройку после закрытия transition
		// rows. Окружающий mapper выбирает прежний event kind и node/gate state; локальная запись не
		// добавляет transition и не авторизует другой resource.
		type transition struct{ kind, ref, node string }
		var changes []transition
		for transitions.Next() {
			var change transition
			if transitions.Scan(&change.kind, &change.ref, &change.node) != nil {
				transitions.Close()
				return errs.ErrUnavailable
			}
			changes = append(changes, change)
		}
		err = transitions.Err()
		transitions.Close()
		if err != nil {
			return errs.ErrUnavailable
		}
		for _, change := range changes {
			eventKind, gateRef, nodeState := "RUN_STATE_CHANGED", "", ""
			if change.kind == "NODE" {
				eventKind, nodeState = "NODE_STATE_CHANGED", "CANCELLED"
			}
			if change.kind == "GATE" {
				eventKind, gateRef, nodeState = "OWNER_GATE_RESOLVED", change.ref, "CANCELLED"
			}
			if _, err = repository.emitRunEvent(ctx, tx, current, root.project, root.id, change.ref, eventKind, change.node, "", gateRef, "", "i18n:RUN_CANCELLED", root.state, nodeState); err != nil {
				return err
			}
		}
	}
	return nil
}
