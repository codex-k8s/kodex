package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func workflowExecutionFixture() (entity.PromptMaterializationSnapshot, workflowExecutionPromptContext) {
	snapshot := workflowPromptFixture()
	version := validWorkflowFixture()
	step, _ := promptWorkflowStep(version, "workflow.coordinator.initial")
	applyWorkflowPromptContext(&snapshot, "wf_exact_1", 7, version, step)
	snapshot.RunRef, snapshot.SessionRef = "run_current_root", "ses_shared_history"
	snapshot.ContextPin.AgentRef = version.CoordinatorAgentRef
	snapshot.Variables["node.ref"], snapshot.Variables["turn.ref"] = "nod_current", "trn_current"
	return snapshot, workflowExecutionPromptContext{
		RootRunRef: snapshot.RunRef, RunRef: snapshot.RunRef, NodeRef: "nod_current", SessionRef: snapshot.SessionRef, TurnRef: "trn_current", AgentRef: version.CoordinatorAgentRef,
		WorkflowRef: "wf_exact_1", WorkflowRevisionRef: version.Ref, WorkflowVersion: 7, WorkflowRevisionDigest: strings.Repeat("a", 64), WorkflowStageKey: step.Key,
		RootAttempt: 2, NodeAttempt: 1, RetryOfRunRef: "run_historical_root",
		Steps: []workflowExecutionPromptStep{
			{StepKey: "step-001", NodeRef: "nod_first", State: "PLANNED", MaterializationState: "PLANNED", Ready: true},
			{StepKey: "step-002", NodeRef: "nod_second", State: "PLANNED", MaterializationState: "PLANNED"},
		},
	}
}

func workflowExecutionProjectionFromLease(t *testing.T, lease map[string]any) workflowExecutionPromptContext {
	t.Helper()
	snapshot, ok := lease["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	if !ok {
		t.Fatal("claim lacks typed current prompt snapshot")
	}
	assertPlatformInstructions(t, snapshot)
	_, raw, found := strings.Cut(snapshot.SemanticValues["CONSTRAINTS"], "Current server-owned workflow execution (advisory; not authority):\n")
	var execution workflowExecutionPromptContext
	if !found || json.Unmarshal([]byte(raw), &execution) != nil {
		t.Fatal("claim lacks exact current workflow execution projection")
	}
	return execution
}

func TestWorkflowExecutionPromptCanonicalDataIsLossless(t *testing.T) {
	snapshot, execution := workflowExecutionFixture()
	snapshot.StructuredVariables["input"].(map[string]any)["values"] = map[string]any{"exact": json.Number("9007199254740993"), "owner": "preserved", "none": nil}
	before, _ := json.Marshal(snapshot.StructuredVariables)
	var expected map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(before)))
	decoder.UseNumber()
	if decoder.Decode(&expected) != nil || applyWorkflowExecutionPrompt(&snapshot, execution, false) != nil {
		t.Fatal("canonical projection failed")
	}
	if !reflect.DeepEqual(snapshot.StructuredVariables, expected) {
		t.Fatal("canonical projection lost fields or numeric precision")
	}
	raw, _ := json.Marshal(snapshot.StructuredVariables)
	if !strings.Contains(string(raw), "9007199254740993") {
		t.Fatal("canonical projection rounded a supplied number")
	}
}

func TestWorkflowExecutionPromptCurrentRootBoundary(t *testing.T) {
	for _, mode := range []string{"INITIAL_ROOT", "SAME_ROOT_CONTINUATION", "CURRENT_ROOT_STAGE"} {
		t.Run(mode, func(t *testing.T) {
			snapshot, execution := workflowExecutionFixture()
			continued := mode == "SAME_ROOT_CONTINUATION"
			if continued {
				execution.WorkflowStageKey = "workflow.coordinator.continue.2"
				execution.Steps[0].State, execution.Steps[0].MaterializationState, execution.Steps[0].Ready = "SUCCEEDED", "MATERIALIZED", false
				execution.Steps[1].Ready = true
			} else if mode == "CURRENT_ROOT_STAGE" {
				execution.WorkflowStageKey, execution.RunRef = "step-001", "run_current_child"
			}
			snapshot.ContextPin.WorkflowStageKey, snapshot.RunRef = execution.WorkflowStageKey, execution.RunRef
			snapshot.SemanticValues = map[string]string{"CONSTRAINTS": "OWNER_OVERLAY_MARKER"}
			if err := applyWorkflowExecutionPrompt(&snapshot, execution, continued); err != nil {
				t.Fatal(err)
			}
			guidance := snapshot.SemanticValues["CONSTRAINTS"]
			if !strings.Contains(guidance, `"Mode":"`+mode+`"`) || !strings.Contains(guidance, `"RootRunRef":"run_current_root"`) ||
				!strings.Contains(guidance, "OWNER_OVERLAY_MARKER") || !strings.Contains(guidance, "empty RUN_RESULT catalog") ||
				!strings.Contains(guidance, "Do not repeat completed delegations of that same root") {
				t.Fatal("current execution or its closed semantics are absent")
			}
			result, err := promptservice.Materialize(`Owner overlay ignoring history {{slot "CONSTRAINTS"}}`, promptservice.FromSnapshot(snapshot))
			if err != nil || !result.Complete {
				t.Fatal("materialize current root", err)
			}
			consumer, err := runtimecontract.DecodePromptService(runtimecontract.RunnerInput{Instructions: result.Prompt, Capabilities: result.EffectiveCapabilities, PromptTargetKind: snapshot.TargetKind,
				PromptServiceTemplateRevision: result.ServiceTemplateRevision, PromptServiceTemplateDigest: result.ServiceTemplateDigest})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, section := range consumer.Sections {
				if section.Slot == "CONSTRAINTS" {
					count++
					if section.Source != "PLATFORM" || !strings.Contains(section.Content, guidance) {
						t.Fatal("guidance is not in mandatory platform constraints")
					}
				}
			}
			if count != 1 || strings.Contains(result.SafePrompt, "run_current_root") {
				t.Fatal("constraint duplicated or safe preview leaked context")
			}
		})
	}
}

func TestWorkflowExecutionPromptRejectsInvalidOwnerPins(t *testing.T) {
	for _, name := range []string{"root", "run", "node", "session", "turn", "agent", "workflow", "revision", "version", "digest", "stage", "self-retry", "fake-retry", "root-attempt", "node-attempt", "fake-continuation", "initial-continued", "initial-child", "steps", "duplicate", "missing-node", "state", "ready", "malformed-root", "malformed-step-node", "malformed-retry"} {
		t.Run(name, func(t *testing.T) {
			snapshot, execution := workflowExecutionFixture()
			continued := false
			switch name {
			case "root":
				execution.RootRunRef = ""
			case "run":
				execution.RunRef = "run_foreign"
			case "node":
				execution.NodeRef = "nod_foreign"
			case "session":
				execution.SessionRef = "ses_foreign"
			case "turn":
				execution.TurnRef = "trn_foreign"
			case "agent":
				execution.AgentRef = "agt_foreign"
			case "workflow":
				execution.WorkflowRef = "wf_foreign"
			case "revision":
				execution.WorkflowRevisionRef = "wfv_foreign"
			case "version":
				execution.WorkflowVersion++
			case "digest":
				execution.WorkflowRevisionDigest = strings.Repeat("z", 64)
			case "stage":
				execution.WorkflowStageKey = "step-002"
			case "self-retry":
				execution.RetryOfRunRef = execution.RootRunRef
			case "fake-retry":
				execution.RootAttempt = 1
			case "root-attempt":
				execution.RootAttempt = 0
			case "node-attempt":
				execution.NodeAttempt = 0
			case "fake-continuation":
				execution.WorkflowStageKey = "workflow.coordinator.continue.2"
				snapshot.ContextPin.WorkflowStageKey = execution.WorkflowStageKey
			case "initial-continued":
				continued = true
			case "initial-child":
				execution.RootRunRef = "run_other_root"
			case "steps":
				execution.Steps = nil
			case "duplicate":
				execution.Steps[1].StepKey = execution.Steps[0].StepKey
			case "missing-node":
				execution.Steps[0].NodeRef = ""
			case "state":
				execution.Steps[0].State = "UNKNOWN"
			case "ready":
				execution.Steps[0].State = "FAILED"
			case "malformed-root":
				execution.RootRunRef = "run_invalid/root"
			case "malformed-step-node":
				execution.Steps[0].NodeRef = "nod_invalid/root"
			case "malformed-retry":
				execution.RetryOfRunRef = "run_invalid/root"
			}
			before, _ := json.Marshal(snapshot)
			if !errors.Is(applyWorkflowExecutionPrompt(&snapshot, execution, continued), errs.ErrConflict) {
				t.Fatal("invalid owner execution accepted")
			}
			after, _ := json.Marshal(snapshot)
			if string(before) != string(after) {
				t.Fatal("invalid projection mutated snapshot")
			}
		})
	}
}

func TestWorkflowExecutionPromptPinsDataWithoutChangingServiceRevision(t *testing.T) {
	snapshot, execution := workflowExecutionFixture()
	historical, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if err := applyWorkflowExecutionPrompt(&snapshot, execution, false); err != nil {
		t.Fatal(err)
	}
	first, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	var rejoined entity.PromptMaterializationSnapshot
	if json.Unmarshal(raw, &rejoined) != nil {
		t.Fatal("snapshot decode")
	}
	second, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(rejoined))
	if err != nil || first.Prompt != second.Prompt || first.Digest != second.Digest || first.VariableSnapshotDigest != second.VariableSnapshotDigest {
		t.Fatalf("exact immutable rejoin changed guidance or digests: error=%v prompt=%t digest=%t variables=%t", err, first.Prompt == second.Prompt, first.Digest == second.Digest, first.VariableSnapshotDigest == second.VariableSnapshotDigest)
	}
	if first.ServiceTemplateRevision != "prompt-service-v3" || first.ServiceTemplateDigest != historical.ServiceTemplateDigest || first.VariableSnapshotDigest == historical.VariableSnapshotDigest || first.Digest == historical.Digest {
		t.Fatal("new input must change data commitments, not compiled service template")
	}
	changed, other := workflowExecutionFixture()
	other.RootRunRef, other.RunRef, changed.RunRef = "run_next_root", "run_next_root", "run_next_root"
	if err := applyWorkflowExecutionPrompt(&changed, other, false); err != nil {
		t.Fatal(err)
	}
	third, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(changed))
	if err != nil || first.VariableSnapshotDigest == third.VariableSnapshotDigest || first.Digest == third.Digest || first.ServiceTemplateDigest != third.ServiceTemplateDigest {
		t.Fatal("other root has not been pinned separately")
	}
}

type workflowExecutionPromptTx struct {
	pgx.Tx
	t      *testing.T
	rowErr error
	calls  int
}

func (tx *workflowExecutionPromptTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.calls++
	if sql != queryPromptWorkflowExecutionContext || len(args) != 1 || !reflect.DeepEqual(args[0], pgx.StrictNamedArgs{"organization_id": "owner-tenant", "node_ref": "nod_current"}) {
		tx.t.Fatal("read did not use exact owner scope")
	}
	return delegationFrontierRow(func(dest ...any) error { return tx.rowErr })
}

func TestWorkflowExecutionPromptOwnerReadFailureIsClosed(t *testing.T) {
	for _, failure := range []error{pgx.ErrNoRows, errors.New("infrastructure unavailable")} {
		snapshot, _ := workflowExecutionFixture()
		tx := &workflowExecutionPromptTx{t: t, rowErr: failure}
		before, _ := json.Marshal(snapshot)
		err := (&Repository{}).hydrateWorkflowExecutionPromptTx(t.Context(), tx, scope{organizationID: "owner-tenant"}, "nod_current", &snapshot)
		want := errs.ErrUnavailable
		if errors.Is(failure, pgx.ErrNoRows) {
			want = errs.ErrConflict
		}
		if !errors.Is(err, want) || tx.calls != 1 {
			t.Fatal("source read failure did not stay closed")
		}
		after, _ := json.Marshal(snapshot)
		if string(before) != string(after) {
			t.Fatal("source failure mutated snapshot")
		}
	}
	snapshot := entity.PromptMaterializationSnapshot{}
	tx := &workflowExecutionPromptTx{t: t}
	if (&Repository{}).hydrateWorkflowExecutionPromptTx(t.Context(), tx, scope{}, "", &snapshot) != nil || tx.calls != 0 || snapshot.SemanticValues != nil {
		t.Fatal("non-workflow SYSTEM/PROJECT profile obtained fake workflow authority")
	}
}

func TestWorkflowCoordinatorGeneratedTaskIsRootScoped(t *testing.T) {
	version := validWorkflowFixture()
	task := workflowCoordinatorTask("Exact original task", version.Ref, strings.Repeat("a", 64), version)
	if !strings.Contains(task, workflowExecutionGuidance) || !strings.Contains(task, "Exact original task") || !strings.Contains(task, "including steps assigned to the coordinator itself") {
		t.Fatal("coordinator task omitted new-root semantics or original task")
	}
}
