package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowGatePolicyRow supplies one bounded owner query result or a closed driver sentinel;
// it contains synthetic policy only and never represents a real execution lease.
// workflowGatePolicyRow возвращает ограниченный owner query result либо закрытый driver sentinel;
// содержит только synthetic policy и не представляет настоящий execution lease.
type workflowGatePolicyRow struct {
	raw               []byte
	stepKey, agentRef string
	err               error
}

// Scan copies the three explicit policy projection columns and propagates a synthetic query error.
// Scan копирует три явные колонки policy projection и передаёт synthetic query error.
func (row workflowGatePolicyRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*[]byte) = row.raw
	*dest[1].(*string) = row.stepKey
	*dest[2].(*string) = row.agentRef
	return nil
}

// workflowGatePolicyTx rejects changes to the owner query's closed pin set in isolated unit tests.
// workflowGatePolicyTx отклоняет изменение закрытого набора pins owner query в изолированном unit.
type workflowGatePolicyTx struct {
	pgx.Tx
	t        *testing.T
	row      workflowGatePolicyRow
	expected pgx.StrictNamedArgs
	calls    int
}

// QueryRow checks the actual query identity and every bound pin before returning a synthetic row.
// QueryRow проверяет настоящую query identity и каждый bound pin до возврата synthetic row.
func (tx *workflowGatePolicyTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.calls++
	if sql != queryRuntimeCompleteexecutionSelectGatePolicy || len(args) != 1 || !reflect.DeepEqual(args[0], tx.expected) {
		tx.t.Fatal("owner gate policy query lost exact execution pins")
	}
	return tx.row
}

// TestExecutionStepGateDecisionsOwnerReadBoundary keeps absent/corrupt/foreign owner projection
// closed and requires the repository query to bind the verified lease's exact immutable pins.
// TestExecutionStepGateDecisionsOwnerReadBoundary закрыто отклоняет отсутствующую, повреждённую
// либо чужую owner projection и требует exact immutable pins проверенного lease в query.
func TestExecutionStepGateDecisionsOwnerReadBoundary(t *testing.T) {
	version := validWorkflowFixture()
	raw, _ := json.Marshal(version)
	lease := map[string]any{"leaseID": "lease", "runtimeRevisionID": "revision", "runID": "run", "rootRunID": "root", "nodeID": "node", "generation": int64(7)}
	expected := pgx.StrictNamedArgs{"organization_id": "tenant", "lease_id": "lease", "runtime_revision_id": "revision", "run_id": "run", "root_run_id": "root", "node_id": "node", "generation": int64(7)}
	if _, _, err := expected.RewriteQuery(t.Context(), nil, queryRuntimeCompleteexecutionSelectGatePolicy, nil); err != nil {
		t.Fatal("owner gate policy SQL does not bind every exact execution pin")
	}
	for _, scenario := range []string{"exact", "missing", "query-failure", "malformed", "foreign-agent"} {
		t.Run(scenario, func(t *testing.T) {
			row := workflowGatePolicyRow{raw: raw, stepKey: "step-002", agentRef: "agt-reviewer"}
			var want error
			switch scenario {
			case "missing":
				row.err = pgx.ErrNoRows
				want = errs.ErrConflict
			case "query-failure":
				row.err = errs.ErrUnavailable
				want = errs.ErrUnavailable
			case "malformed":
				row.raw = []byte("{")
				want = errs.ErrConflict
			case "foreign-agent":
				row.agentRef = "agt-other"
				want = errs.ErrConflict
			}
			tx := &workflowGatePolicyTx{t: t, row: row, expected: expected}
			repository := &Repository{}
			decisions, err := repository.executionStepGateDecisions(t.Context(), tx, scope{organizationID: "tenant"}, lease)
			if !errors.Is(err, want) || tx.calls != 1 || (want != nil && decisions != nil) || (want == nil && !reflect.DeepEqual(decisions, version.Steps[1].GateDecisions)) {
				t.Fatal("owner gate policy read did not preserve closed failure mapping")
			}
		})
	}
}

// TestWorkflowResultGateInsertUsesExactDecisions requires the actual INSERT to bind the caller's
// owner-resolved decision list rather than silently inserting a broader hardcoded policy.
// TestWorkflowResultGateInsertUsesExactDecisions требует привязки разрешённого владельцем списка
// решений в настоящем INSERT вместо молчаливой подстановки более широкой фиксированной политики.
func TestWorkflowResultGateInsertUsesExactDecisions(t *testing.T) {
	args := pgx.StrictNamedArgs{"gate_ref": "gate", "organization_id": "tenant", "project_id": "project", "root_run_id": "root", "node_id": "node", "context_summary": "safe", "allowed_decisions": []string{"APPROVE", "REJECT", "CANCEL"}}
	_, values, err := args.RewriteQuery(t.Context(), nil, queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId, nil)
	if err != nil {
		t.Fatal("result gate INSERT does not bind exact owner decisions")
	}
	found := false
	for _, value := range values {
		if decisions, ok := value.([]string); ok && len(decisions) == 3 && decisions[0] == "APPROVE" && decisions[1] == "REJECT" && decisions[2] == "CANCEL" {
			found = true
		}
	}
	if !found {
		t.Fatal("result gate INSERT discarded the immutable decision list")
	}
}

// TestWorkflowStepGateDecisions preserves the exact published list and rejects malformed or
// mismatched step origins without inventing a policy for coordinator aggregate gates.
// TestWorkflowStepGateDecisions сохраняет точный опубликованный список, отклоняя повреждённое
// либо чужое происхождение этапа; политику aggregate Gate координатора не определяет.
func TestWorkflowStepGateDecisions(t *testing.T) {
	for _, decisions := range [][]string{{"APPROVE", "REJECT", "CANCEL"}, {"CANCEL", "APPROVE"}, {"REQUEST_CHANGES"}, {"APPROVE", "APPROVE"}, {"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}} {
		version := validWorkflowFixture()
		version.Steps[1].GateDecisions = decisions
		actual, err := workflowStepGateDecisions(version, "step-002", "agt-reviewer")
		if err != nil || !reflect.DeepEqual(actual, decisions) {
			t.Fatal("published step gate decisions were changed or expanded")
		}
		actual[0] = "modified returned slice"
		if !reflect.DeepEqual(version.Steps[1].GateDecisions, decisions) {
			t.Fatal("returned policy aliases the immutable published list")
		}
	}
	for _, scenario := range []string{"missing-step", "wrong-agent", "coordinator-origin", "ungated-step", "empty-decisions", "unknown-decision", "duplicate-step", "invalid-structure"} {
		t.Run(scenario, func(t *testing.T) {
			version := validWorkflowFixture()
			stepKey, agentRef := "step-002", "agt-reviewer"
			switch scenario {
			case "missing-step":
				stepKey = "missing"
			case "wrong-agent":
				agentRef = "agt-writer"
			case "coordinator-origin":
				stepKey, agentRef = "workflow.coordinator.initial", version.CoordinatorAgentRef
			case "ungated-step":
				version.Steps[1].HumanGateAfter = false
			case "empty-decisions":
				version.Steps[1].GateDecisions = nil
			case "unknown-decision":
				version.Steps[1].GateDecisions = []string{"UNKNOWN"}
			case "duplicate-step":
				version.Steps[1].Key = version.Steps[0].Key
			case "invalid-structure":
				version.TimeoutSeconds = 0
			}
			if decisions, err := workflowStepGateDecisions(version, stepKey, agentRef); !errors.Is(err, errs.ErrConflict) || decisions != nil {
				t.Fatal("invalid gate policy did not fail closed")
			}
		})
	}
	version := validWorkflowFixture()
	before, err := workflowStepGateDecisions(version, "step-002", "agt-reviewer")
	if err != nil {
		t.Fatal("published fixture cannot resolve gate policy")
	}
	raw, _ := json.Marshal(version)
	var editedDraft entity.WorkflowVersion
	if json.Unmarshal(raw, &editedDraft) != nil {
		t.Fatal("decode independent draft fixture")
	}
	editedDraft.Steps[1].GateDecisions = []string{"CANCEL"}
	after, err := workflowStepGateDecisions(version, "step-002", "agt-reviewer")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("separate draft edit changed pinned published policy")
	}
}
