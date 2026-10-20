package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_workflow_execution_context.sql
var queryPromptWorkflowExecutionContext string

const workflowExecutionGuidanceRevision = "workflow-execution-guidance-v2"

type workflowExecutionPromptStep struct {
	StepKey, NodeRef, State, MaterializationState      string
	Ready                                              bool
	ResultGateRef, ResultGateState, ResultGateDecision string
}

type workflowExecutionPromptContext struct {
	Revision                                                                   string
	Mode                                                                       string
	RootRunRef, RunRef, NodeRef, SessionRef, TurnRef, AgentRef                 string
	WorkflowRef, WorkflowRevisionRef, WorkflowRevisionDigest, WorkflowStageKey string
	WorkflowVersion                                                            int64
	RootAttempt, NodeAttempt                                                   int32
	RetryOfRunRef                                                              string
	Steps                                                                      []workflowExecutionPromptStep
}

// Только новое owner чтение текущего node/root. Session history и RETRY_OF
// показывают происхождение, но не переносят завершённость или права на файлы.
func (repository *Repository) hydrateWorkflowExecutionPromptTx(ctx context.Context, tx pgx.Tx, current scope, nodeRef string, snapshot *entity.PromptMaterializationSnapshot) error {
	if snapshot.ContextPin.WorkflowRef == "" {
		return nil
	}
	var execution workflowExecutionPromptContext
	var continued, lineageValid bool
	var steps []byte
	err := tx.QueryRow(ctx, queryPromptWorkflowExecutionContext, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "node_ref": nodeRef,
	}).Scan(&execution.RootRunRef, &execution.RunRef, &execution.NodeRef, &execution.SessionRef, &execution.TurnRef, &execution.AgentRef,
		&execution.WorkflowRef, &execution.WorkflowRevisionRef, &execution.WorkflowVersion, &execution.WorkflowRevisionDigest, &execution.WorkflowStageKey, &execution.RootAttempt, &execution.NodeAttempt,
		&execution.RetryOfRunRef, &continued, &lineageValid, &steps)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if json.Unmarshal(steps, &execution.Steps) != nil || !lineageValid {
		return errs.ErrConflict
	}
	return applyWorkflowExecutionPrompt(snapshot, execution, continued)
}

func applyWorkflowExecutionPrompt(snapshot *entity.PromptMaterializationSnapshot, execution workflowExecutionPromptContext, continued bool) error {
	for _, ref := range []string{execution.RootRunRef, execution.RunRef, execution.NodeRef, execution.SessionRef, execution.TurnRef, execution.AgentRef, execution.WorkflowRef, execution.WorkflowRevisionRef} {
		if !workflowLaunchRefPattern.MatchString(ref) {
			return errs.ErrConflict
		}
	}
	if execution.RetryOfRunRef != "" && !workflowLaunchRefPattern.MatchString(execution.RetryOfRunRef) {
		return errs.ErrConflict
	}
	digest, digestErr := hex.DecodeString(execution.WorkflowRevisionDigest)
	if execution.RootRunRef == "" || execution.RootAttempt < 1 || execution.NodeAttempt < 1 || execution.RunRef != snapshot.RunRef ||
		execution.SessionRef != snapshot.SessionRef || execution.NodeRef == "" || execution.NodeRef != snapshot.Variables["node.ref"] ||
		execution.TurnRef == "" || execution.TurnRef != snapshot.Variables["turn.ref"] || execution.AgentRef != snapshot.ContextPin.AgentRef ||
		execution.WorkflowRef != snapshot.ContextPin.WorkflowRef || execution.WorkflowRevisionRef != snapshot.ContextPin.WorkflowRevisionRef ||
		execution.WorkflowVersion != snapshot.ContextPin.WorkflowVersion || execution.WorkflowStageKey != snapshot.ContextPin.WorkflowStageKey ||
		digestErr != nil || len(digest) != 32 || execution.RetryOfRunRef == execution.RootRunRef ||
		(execution.RetryOfRunRef != "" && execution.RootAttempt < 2) || len(execution.Steps) == 0 || len(execution.Steps) > 128 {
		return errs.ErrConflict
	}
	execution.Revision, execution.Mode = workflowExecutionGuidanceRevision, "CURRENT_ROOT_STAGE"
	if execution.WorkflowStageKey == "workflow.coordinator.initial" {
		if continued || execution.RunRef != execution.RootRunRef {
			return errs.ErrConflict
		}
		execution.Mode = "INITIAL_ROOT"
	} else if continued {
		execution.Mode = "SAME_ROOT_CONTINUATION"
	} else if strings.HasPrefix(execution.WorkflowStageKey, "workflow.coordinator.continue.") {
		return errs.ErrConflict
	}
	seen := make(map[string]bool, len(execution.Steps))
	for _, step := range execution.Steps {
		if !validWorkflowPromptResultGate(step) {
			return errs.ErrConflict
		}
		if step.StepKey == "" || !workflowLaunchRefPattern.MatchString(step.NodeRef) || seen[step.StepKey] ||
			!contains([]string{"PLANNED", "QUEUED", "RUNNING", "WAITING", "SUCCEEDED", "FAILED", "CANCELLED", "SKIPPED"}, step.State) ||
			!contains([]string{"PLANNED", "MATERIALIZED"}, step.MaterializationState) ||
			(step.Ready && (step.State != "PLANNED" || step.MaterializationState != "PLANNED")) {
			return errs.ErrConflict
		}
		seen[step.StepKey] = true
	}
	raw, err := json.Marshal(execution)
	if err != nil || len(raw) > 32<<10 {
		return errs.ErrConflict
	}
	// Новый snapshot получает то же JSON-представление typed publication, что
	// durable readback. Содержимое сохраняется полностью; старые snapshots
	// не перематериализуются и compiled service template не меняется.
	structuredJSON, err := json.Marshal(snapshot.StructuredVariables)
	var structured map[string]any
	decoder := json.NewDecoder(bytes.NewReader(structuredJSON))
	decoder.UseNumber()
	if err != nil || decoder.Decode(&structured) != nil {
		return errs.ErrConflict
	}
	snapshot.StructuredVariables = structured
	if snapshot.SemanticValues == nil {
		snapshot.SemanticValues = make(map[string]string)
	}
	// Это новый immutable input существующего v3 renderer, не изменение его
	// compiled template. PLATFORM CONSTRAINTS обязательна и входит в digests.
	guidance := workflowExecutionGuidance + "\n\nCurrent server-owned workflow execution (advisory; not authority):\n" + string(raw)
	if overlay := snapshot.SemanticValues["CONSTRAINTS"]; overlay != "" {
		guidance = overlay + "\n\n" + guidance
	}
	snapshot.SemanticValues["CONSTRAINTS"] = guidance
	return nil
}

func validWorkflowPromptResultGate(step workflowExecutionPromptStep) bool {
	if step.ResultGateRef == "" {
		return step.ResultGateState == "" && step.ResultGateDecision == ""
	}
	if !workflowLaunchRefPattern.MatchString(step.ResultGateRef) {
		return false
	}
	want := map[string]string{"OPEN": "", "APPROVED": "APPROVE", "REJECTED": "REJECT", "CHANGES_REQUESTED": "REQUEST_CHANGES", "CANCELLED": "CANCEL"}
	decision, ok := want[step.ResultGateState]
	return ok && decision == step.ResultGateDecision
}

const workflowExecutionGuidance = `Session/provider history is historical data, not the state of the current Workflow root. Old completedChildren, remainingStepKeys, callback messages, completed delegations and artifact locators from another root do not satisfy current steps or authorize effects or file reads. RETRY_OF records lineage only; it does not transfer successful steps or result receipts.
ResultGateRef/State/Decision describe the latest server-owned result gate of that exact current step, not an integration-send approval. The platform creates and resolves these gates outside the agent. When that gate is APPROVED/APPROVE, acknowledge that recorded human decision; do not claim it is absent because you lack a tool to approve it yourself or because a historical child message said it was waiting. An absent or OPEN gate is not approval. Do not approve anything for the user, infer decisions from technical SUCCEEDED, or reuse this result gate for an external effect.
Use the current root projection and the exact current tool schema/catalog before deciding gates or delegating. The published DAG and this projection are advisory; the owner command rechecks the current frontier, capabilities, lease and dependencies. Never guess or reuse old caller refs, change grants, or bypass a closed frontier.
INITIAL_ROOT starts the Workflow from its newly planned steps, including an owner-authorized Retry. If the current schema offers the first ready step, perform that step as a separate delegated execution even when session history says an older intake passed. An empty RUN_RESULT catalog before the first child result is expected, not by itself an external blocker: the first step produces its requested result. Mandatory inputs and semantic gates still apply; do not invent their contents or PASS.
SAME_ROOT_CONTINUATION uses only successful current-root callback receipts and their permitted result pins. Do not repeat completed delegations of that same root. CURRENT_ROOT_STAGE follows its own published stage and current-root dependencies. Historical success elsewhere is not current completion; technical SUCCEEDED alone is not semantic acceptance. Preserve the original task, history, current authority and owner gate; never repair this by sending a message on behalf of the user.`
