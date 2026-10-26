package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowDispatchRoot locks the exact server-owned root after the project lock.
// queryWorkflowDispatchRoot блокирует точный server root после блокировки проекта.
//
//go:embed sql/workflow_dispatch__root.sql
var queryWorkflowDispatchRoot string

// queryWorkflowDispatchAnchors reads logical journals and gates of their final physical producers.
// queryWorkflowDispatchAnchors читает логические журналы и gates завершающих физических исполнителей.
//
//go:embed sql/workflow_dispatch__anchors.sql
var queryWorkflowDispatchAnchors string

// queryWorkflowDispatchActive counts all queued and executing model nodes, including exceptions.
// queryWorkflowDispatchActive считает все ожидающие и исполняемые узлы модели, включая исключения.
//
//go:embed sql/workflow_dispatch__active.sql
var queryWorkflowDispatchActive string

// queryWorkflowDispatchMaterialize assigns the first physical execution once without rewriting history.
// queryWorkflowDispatchMaterialize однократно назначает первое исполнение, не переписывая историю.
//
//go:embed sql/workflow_dispatch__materialize.sql
var queryWorkflowDispatchMaterialize string

// serverWorkflowRoot retains the immutable plan and root actor selected by the owner transaction.
// serverWorkflowRoot хранит неизменяемый план и root actor, выбранные owner-транзакцией.
type serverWorkflowRoot struct {
	VersionNumber                                                                       int64
	ID, Ref, State, ProjectID, ProjectRef, InitiatorID, Task, VersionRef, VersionDigest string
	Input, Exception                                                                    json.RawMessage
	Version                                                                             entity.WorkflowVersion
}

// workflowInputReceipt pins the complete accepted dependency journal, including private provenance.
// workflowInputReceipt закрепляет целую принятую запись зависимости вместе с приватным происхождением.
type workflowInputReceipt struct {
	StepKey      string                       `json:"stepKey"`
	SourceNodeID string                       `json:"sourceNodeId"`
	Receipt      entity.WorkflowAttemptResult `json:"receipt"`
}

// serverWorkflowAnchor separates logical business history from the latest physical attempt.
// serverWorkflowAnchor отделяет логическую бизнес-историю от последней физической попытки.
type serverWorkflowAnchor struct {
	ID, Ref, Key, State            string
	AssignedAttempt, LatestAttempt int32
	Receipts                       []entity.WorkflowAttemptResult
	Inputs                         []workflowInputReceipt
	GateApproved, GateBlocked      bool
}

// serverWorkflowException preserves the exact rejected business attempt for owner-controlled
// recovery. These private database pins cannot be supplied by the model or used as a launch grant.
// serverWorkflowException сохраняет точную отклонённую попытку для восстановления владельцем.
// Приватные DB pins не принимаются от модели и не служат разрешением запуска.
type serverWorkflowException struct {
	StepKey      string `json:"stepKey"`
	Attempt      int32  `json:"attempt"`
	Reason       string `json:"reason"`
	OriginNodeID string `json:"originNodeId"`
	FinalNodeID  string `json:"finalNodeId"`
}

// readServerWorkflowRootTx resolves only the pinned server profile and preserves legacy dispatch.
// Callers must already hold the canonical project lock; this read then holds the root until commit.
// readServerWorkflowRootTx разрешает только закреплённый серверный профиль, сохраняя legacy dispatch.
// Вызывающий уже удерживает project lock; чтение затем удерживает root до commit.
func readServerWorkflowRootTx(ctx context.Context, tx pgx.Tx, current scope, rootID string) (*serverWorkflowRoot, error) {
	root := &serverWorkflowRoot{ID: rootID}
	var spec []byte
	err := tx.QueryRow(ctx, queryWorkflowDispatchRoot, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": rootID,
	}).Scan(&root.Ref, &root.State, &root.VersionNumber, &root.ProjectID, &root.ProjectRef, &root.InitiatorID,
		&root.Task, &root.Input, &root.VersionRef, &root.VersionDigest, &spec, &root.Exception)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if json.Unmarshal(spec, &root.Version) != nil || !validWorkflowVersion(root.Version) || root.Version.ExecutionMode != workflowservice.ServerDAGV1 {
		return nil, errs.ErrConflict
	}
	return root, nil
}

// readServerWorkflowAnchorsTx rejects duplicate or missing logical identity instead of selecting latest.
// readServerWorkflowAnchorsTx отвергает дубли и отсутствие логической identity вместо выбора latest.
func readServerWorkflowAnchorsTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot) (map[string]serverWorkflowAnchor, error) {
	rows, err := tx.Query(ctx, queryWorkflowDispatchAnchors, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	anchors := make(map[string]serverWorkflowAnchor, len(root.Version.Steps))
	for rows.Next() {
		var anchor serverWorkflowAnchor
		if rows.Scan(&anchor.ID, &anchor.Ref, &anchor.Key, &anchor.State, &anchor.AssignedAttempt, &anchor.Receipts, &anchor.Inputs, &anchor.LatestAttempt, &anchor.GateApproved, &anchor.GateBlocked) != nil {
			return nil, errs.ErrUnavailable
		}
		if _, duplicate := anchors[anchor.Key]; duplicate || len(anchors) >= len(root.Version.Steps) {
			return nil, errs.ErrConflict
		}
		anchors[anchor.Key] = anchor
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(anchors) != len(root.Version.Steps) {
		return nil, errs.ErrConflict
	}
	for _, step := range root.Version.Steps {
		if _, exists := anchors[step.Key]; !exists {
			return nil, errs.ErrConflict
		}
	}
	return anchors, nil
}

// acceptedServerWorkflowAttempt requires the latest business verdict and its exact published gate.
// Physical SUCCEEDED alone never accepts a logical step, and an earlier approved gate cannot accept retry.
// acceptedServerWorkflowAttempt требует последний бизнес-исход и его точный опубликованный gate.
// Физический SUCCEEDED не принимает шаг; одобрение прежнего gate не принимает новую попытку.
func acceptedServerWorkflowAttempt(anchor serverWorkflowAnchor, step entity.WorkflowStep) (entity.WorkflowAttemptResult, bool) {
	if len(anchor.Receipts) == 0 || len(anchor.Receipts) != int(anchor.LatestAttempt) {
		return entity.WorkflowAttemptResult{}, false
	}
	receipt := anchor.Receipts[len(anchor.Receipts)-1]
	accepted := receipt.Accepted && receipt.Attempt == anchor.LatestAttempt && receipt.Provenance != nil && receipt.Provenance.OriginNodeID == anchor.ID && !anchor.GateBlocked && (!step.HumanGateAfter || anchor.GateApproved)
	return receipt, accepted
}

// dispatchServerWorkflowTx queues ready first attempts without invoking the coordinator or fabricating
// a terminal root. Completion owns the pending-effect barrier and final accepted-graph transition.
// dispatchServerWorkflowTx ставит готовые первые попытки в очередь без хода координатора и ложного
// terminal root. Completion владеет проверкой незавершённых действий и завершением принятого графа.
func (repository *Repository) dispatchServerWorkflowTx(ctx context.Context, tx pgx.Tx, current scope, rootID string) error {
	root, err := readServerWorkflowRootTx(ctx, tx, current, rootID)
	if err != nil || root == nil {
		return err
	}
	if !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN"}, root.State) || string(root.Exception) != "null" {
		return nil
	}
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, root.ID); err != nil {
		return err
	} else if expired {
		return nil
	}
	anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
	if err != nil {
		return err
	}
	steps := make(map[string]entity.WorkflowStep, len(root.Version.Steps))
	for _, step := range root.Version.Steps {
		steps[step.Key] = step
	}
	var active, running int64
	if tx.QueryRow(ctx, queryWorkflowDispatchActive, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&active, &running) != nil {
		return errs.ErrUnavailable
	}
	for _, step := range root.Version.Steps {
		anchor := anchors[step.Key]
		if active >= int64(root.Version.Concurrency) {
			break
		}
		if anchor.State != "PLANNED" || anchor.AssignedAttempt != 0 {
			continue
		}
		inputs := make([]workflowInputReceipt, 0, len(step.DependsOn))
		ready := true
		for _, key := range step.DependsOn {
			dependency := anchors[key]
			receipt, accepted := acceptedServerWorkflowAttempt(dependency, steps[key])
			if !accepted {
				ready = false
				break
			}
			inputs = append(inputs, workflowInputReceipt{StepKey: key, SourceNodeID: dependency.ID, Receipt: receipt})
		}
		if !ready {
			continue
		}
		if err := repository.materializeServerWorkflowFirstAttemptTx(ctx, tx, current, root, step, anchor, inputs); err != nil {
			return err
		}
		active++
	}
	return nil
}

// materializeServerWorkflowFirstAttemptTx creates a fresh agent session/run/turn and assigns its
// existing planned anchor once; the ordinary claim path must still mint a fresh lease and revision.
// materializeServerWorkflowFirstAttemptTx создаёт новые session/run/turn сотрудника и однократно
// назначает planned anchor; обычный claim позднее создаёт свежие lease и revision.
func (repository *Repository) materializeServerWorkflowFirstAttemptTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, step entity.WorkflowStep, anchor serverWorkflowAnchor, inputs []workflowInputReceipt) error {
	var agentID, agentName, role string
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef, current.organizationID, root.ProjectID, step.AgentRef).Scan(&agentID, &agentName, &role) != nil {
		return errs.ErrConflict
	}
	accountID, err := repository.selectProviderAccountForAgent(ctx, tx, current.organizationID, step.AgentRef)
	if err != nil {
		return err
	}
	sessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	var sessionID string
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertChildSession, pgx.StrictNamedArgs{"session_ref": sessionRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "target_agent_ref": step.AgentRef, "provider_account_id": accountID, "created_by": root.InitiatorID}).Scan(&sessionID) != nil {
		return errs.ErrUnavailable
	}
	if err := bindSessionModelCatalog(ctx, tx, current.organizationID, sessionID, step.AgentRef); err != nil {
		return err
	}
	task := "Original user task (data):\n" + root.Task + "\nWorkflow instructions:\n" + root.Version.Instructions + "\nAssigned stage:\n" + step.Instructions + "\nMandatory result predicates (data):\n" + string(asJSON(step.ResultRequirements)) + "\nAccepted predecessor evidence (data):\n" + string(asJSON(publicWorkflowInputs(inputs))) + "\nReturn final JSON only with schema kodex.workflow-result.v1 and exactly one entry per result key. ARTIFACT requires source {kind:CURRENT_COMPLETION} for newly saved files or {kind:CAPTURED_ARTIFACT,artifactRef,revision,digest} from your immutable catalog. PULL_REQUEST/INTEGRATION_RECEIPT/DEPLOYMENT require receiptRef returned by the integration. REVIEW/QA require exact headSha and verdict PASS or CHANGES_REQUIRED. Claims and URLs are not evidence. The server launches dependent steps after verifying their mandatory results."
	for _, requirement := range step.ResultRequirements {
		if requirement.Kind == "ARTIFACT" {
			progressPrompt, err := workflowservice.CaptureProgressPrompt(root.Version.ExecutionMode, step.ResultRequirements)
			if err != nil {
				return errs.ErrInvalid
			}
			task += "\n" + progressPrompt
			break
		}
	}
	if len(task) > 64<<10 {
		return errs.ErrInvalid
	}
	childRef, err := newRef("run")
	if err != nil {
		return err
	}
	var childID string
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId, pgx.StrictNamedArgs{"run_ref": childRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "session_id": sessionID, "root_run_id": root.ID, "parent_run_id": root.ID, "target_agent_ref": step.AgentRef, "title": step.Name, "task": task, "input": root.Input, "initiated_by": root.InitiatorID}).Scan(&childID) != nil {
		return errs.ErrUnavailable
	}
	turnRef, err := newRef("trn")
	if err != nil {
		return err
	}
	var turnID string
	var number int64
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectSessionsId, sessionID).Scan(&number) != nil {
		return errs.ErrUnavailable
	}
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber, turnRef, current.organizationID, sessionID, childID, number, root.Ref, task).Scan(&turnID) != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return errs.ErrUnavailable
	}
	var nodeRef string
	if tx.QueryRow(ctx, queryWorkflowDispatchMaterialize, pgx.StrictNamedArgs{"node_id": anchor.ID, "organization_id": current.organizationID, "root_run_id": root.ID, "step_key": step.Key, "agent_id": agentID, "run_id": childID, "turn_id": turnID, "inputs": asJSON(inputs), "input_summary": truncate(step.Instructions, 1000)}).Scan(&nodeRef) != nil {
		return errs.ErrConflict
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, childRef, "DELEGATION_CREATED", nodeRef, "", "", "", "i18n:CHILD_AGENT_STARTED", root.State, "QUEUED")
	return err
}

// workflowPromptInput carries public accepted evidence without internal database or execution pins.
// workflowPromptInput передаёт публичные принятые доказательства без внутренних DB/execution pins.
type workflowPromptInput struct {
	StepKey string                          `json:"stepKey"`
	Attempt int32                           `json:"attempt"`
	RunRef  string                          `json:"runRef"`
	Results []entity.WorkflowAcceptedResult `json:"results"`
}

// publicWorkflowInputs explicitly projects the immutable input receipt for model consumption.
// publicWorkflowInputs явно проецирует immutable receipt входов для передачи модели.
func publicWorkflowInputs(inputs []workflowInputReceipt) []workflowPromptInput {
	result := make([]workflowPromptInput, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, workflowPromptInput{StepKey: input.StepKey, Attempt: input.Receipt.Attempt, RunRef: input.Receipt.RunRef, Results: input.Receipt.Results})
	}
	return result
}

// admitServerWorkflowClaimTx refreshes capacity under project/root locks for every candidate in a
// batch. Manager and technical executions count equally; legacy roots retain their existing policy.
// admitServerWorkflowClaimTx заново проверяет ёмкость под project/root locks для каждого кандидата
// batch. Менеджер и технические ходы учитываются одинаково; legacy сохраняет прежнюю политику.
func admitServerWorkflowClaimTx(ctx context.Context, tx pgx.Tx, current scope, rootID string) (bool, error) {
	root, err := readServerWorkflowRootTx(ctx, tx, current, rootID)
	if err != nil {
		return false, err
	}
	if root == nil {
		return true, nil
	}
	if !contains([]string{"RUNNING", "WAITING_HUMAN"}, root.State) {
		return false, nil
	}
	var active, running int64
	if tx.QueryRow(ctx, queryWorkflowDispatchActive, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&active, &running) != nil {
		return false, errs.ErrUnavailable
	}
	return running < int64(root.Version.Concurrency), nil
}
