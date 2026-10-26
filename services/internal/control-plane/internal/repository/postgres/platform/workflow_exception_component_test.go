package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowExceptionProof observes only native owner rows in the isolated component database.
// queryWorkflowExceptionProof наблюдает только native owner rows в isolated component database.
//
//go:embed testdata/sql/workflow_exception_proof.sql
var queryWorkflowExceptionProof string

// queryWorkflowExceptionMutate attempts only forbidden changes inside rollback-only fixture transactions.
// queryWorkflowExceptionMutate пробует только запрещённые изменения в rollback-only fixture transactions.
//
//go:embed testdata/sql/workflow_exception_mutate.sql
var queryWorkflowExceptionMutate string

// TestWorkflowExceptionRecoveryComponent creates real typed business failures, one manager per episode,
// and owner-approved fresh attempts through Execute only. It never inserts a ready business state.
// TestWorkflowExceptionRecoveryComponent создаёт настоящие typed business failures, одного manager на
// episode и owner-approved fresh attempts только через Execute; готовый business state не вставляется.
func TestWorkflowExceptionRecoveryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated workflow exception database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		actor := owner
		if kind == command.ClaimExecution || kind == command.CompleteExecution {
			actor = worker
		}
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "workflow-exception-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native exception command: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Owner recovery lifecycle", Language: "ru"}).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "exception-manager", "Менеджер разбора")
	workerAgent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "exception-worker", "Исполнитель")
	workerAgent = *execute(command.ChangeAgentCapability, "artifact", &workerAgent.Version, command.AgentBindingInput{AgentRef: workerAgent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Восстановление владельцем", Purpose: "Проверить четыре business attempts.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "work", Position: 1, Name: "Работа", AgentRef: workerAgent.Ref, Instructions: "Создать retry.md и вернуть точную декларацию.", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "retry.md", MediaType: "text/markdown"}}}}}
	workflow := execute(command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать обязательный материал.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	read := func() (string, []workflowExceptionEpisode, int64, int64, string) {
		t.Helper()
		var id, state, originRun string
		var history []workflowExceptionEpisode
		var managers, leases int64
		if err := pool.QueryRow(ctx, queryWorkflowExceptionProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref}).Scan(&id, &state, &history, &managers, &leases, &originRun); err != nil {
			t.Fatal("read native exception proof", completionOutcomeClass(err))
		}
		return state, history, managers, leases, originRun
	}
	claim := func(key string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "workflow-exception-fixture", Limit: 2}).RuntimeItems
		if len(items) != 1 {
			t.Fatal("exception lifecycle did not admit exactly one native execution")
		}
		return items[0]
	}
	complete := func(key string, lease map[string]any, summary string) {
		t.Helper()
		execute(command.CompleteExecution, key, nil, command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: summary})
	}
	declaration := `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`
	var anchorID, anchorRun string
	seenRun, seenNode := map[string]bool{}, map[string]bool{}
	for attempt := 1; attempt <= 4; attempt++ {
		step := claim(fmt.Sprintf("step-%d", attempt))
		if stringMap(step, "agentRef") != workerAgent.Ref || seenRun[stringMap(step, "runRef")] || seenNode[stringMap(step, "nodeRef")] {
			t.Fatal("business retry reused a physical run/node or selected the manager")
		}
		seenRun[stringMap(step, "runRef")], seenNode[stringMap(step, "nodeRef")] = true, true
		complete(fmt.Sprintf("fail-%d", attempt), step, declaration)
		state, history, managers, _, originRun := read()
		if len(history) != attempt || history[attempt-1].Attempt != int32(attempt) || history[attempt-1].State != "MANAGER_QUEUED" || managers != int64(attempt) || state != "RUNNING" {
			t.Fatal("business rejection did not persist one distinct manager episode")
		}
		episode := history[attempt-1]
		if attempt == 1 {
			encoded, err := json.Marshal(history)
			if err != nil {
				t.Fatal("encode native episode baseline")
			}
			for _, fault := range []string{"clear", "replace-reason", "jump-version", "replace-manager", "duplicate", "premature-recovery", "replace-inputs"} {
				t.Run("episode-"+fault, func(t *testing.T) {
					var altered []map[string]any
					if json.Unmarshal(encoded, &altered) != nil {
						t.Fatal("decode independent episode mutation")
					}
					switch fault {
					case "clear":
						altered = []map[string]any{}
					case "replace-reason":
						altered[0]["reason"] = "FORGED_REASON"
					case "jump-version":
						altered[0]["version"] = altered[0]["version"].(float64) + 2
					case "replace-manager":
						altered[0]["managerNodeRef"] = "nod_foreign_manager"
					case "duplicate":
						altered = append(altered, altered[0])
					case "premature-recovery":
						altered[0]["state"] = "RECOVERED"
						altered[0]["version"] = altered[0]["version"].(float64) + 1
						altered[0]["recoveryNodeId"] = episode.FinalNodeID
					case "replace-inputs":
						altered[0]["inputs"] = []any{map[string]any{"stepKey": "foreign"}}
					}
					var active any
					if len(altered) > 0 {
						active = altered[len(altered)-1]
					}
					tx, err := pool.Begin(ctx)
					if err != nil {
						t.Fatal("begin disposable episode mutation")
					}
					_, cause := tx.Exec(ctx, queryWorkflowExceptionMutate, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref, "history": asJSON(altered), "active": asJSON(active)})
					var driver *pgconn.PgError
					closed := errors.As(cause, &driver) && driver.Code == "23514"
					rollbackWorkflowJournalProbe(t, ctx, tx)
					_, after, _, _, _ := read()
					if !closed || !reflect.DeepEqual(history, after) {
						t.Fatal("episode mutation was not rejected atomically")
					}
				})
			}
		}
		if attempt == 1 {
			anchorID, anchorRun = episode.OriginNodeID, originRun
		}
		if episode.OriginNodeID != anchorID || originRun != anchorRun {
			t.Fatal("business retry rewrote the immutable logical anchor")
		}
		complete(fmt.Sprintf("fail-%d", attempt), step, declaration)
		_, replay, replayManagers, _, _ := read()
		if !reflect.DeepEqual(history, replay) || replayManagers != managers {
			t.Fatal("business completion replay duplicated recovery")
		}
		managerLease := claim(fmt.Sprintf("manager-%d", attempt))
		capabilities := runtimeRevisionStringSlice(managerLease["capabilities"])
		if stringMap(managerLease, "agentRef") != manager.Ref || slices.Contains(capabilities, "platform.run.delegate") || slices.Contains(capabilities, "platform.run.launch") || len(runtimeRevisionGrants(managerLease["integrationGrants"])) != 0 {
			t.Fatal("exception manager obtained ordinary launch/delegate/integration authority")
		}
		complete(fmt.Sprintf("analysis-%d", attempt), managerLease, "The required file is missing. Owner approval may authorize a fresh attempt; this analysis cannot waive its predicate.")
		state, history, managers, leases, _ := read()
		episode = history[attempt-1]
		if state != "WAITING_HUMAN" || episode.State != "WAITING_OWNER" || episode.GateRef == "" || managers != int64(attempt) || leases != 0 {
			t.Fatal("manager completion did not open one recovery gate without active leases")
		}
		gate, err := service.GetOwnerGate(ctx, owner, episode.GateRef)
		if err != nil || gate.State != "OPEN" {
			t.Fatal("owner cannot read the exact recovery gate")
		}
		if attempt < 4 {
			if !slices.Contains(gate.AllowedDecisions, "APPROVE") {
				t.Fatal("recoverable attempt omitted owner approval")
			}
			execute(command.ResolveOwnerGate, fmt.Sprintf("approve-%d", attempt), &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Повторить с обязательным файлом."})
			_, history, _, _, _ := read()
			if history[attempt-1].State != "RECOVERED" || history[attempt-1].RecoveryNodeID == episode.FinalNodeID {
				t.Fatal("owner approval did not create a fresh physical business attempt")
			}
		} else {
			if !slices.Equal(gate.AllowedDecisions, []string{"REJECT", "CANCEL"}) {
				t.Fatal("attempt five was offered by the recovery gate")
			}
			_, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "workflow-exception-forbidden-five", ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"}})
			if !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("owner approval bypassed the maximum business attempt")
			}
			execute(command.ResolveOwnerGate, "reject-final", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "REJECT", Comment: "Завершить без принятия недостающего результата."})
		}
	}
	state, history, managers, leases, _ := read()
	if state != "FAILED" || history[3].State != "REJECTED" || managers != 4 || leases != 0 {
		t.Fatal("final rejection did not close the whole recovery graph")
	}
	items := execute(command.ClaimExecution, "terminal-claim", nil, command.LeaseInput{WorkloadInstance: "workflow-exception-fixture", Limit: 2}).RuntimeItems
	if len(items) != 0 {
		t.Fatal("terminal recovery graph admitted another model")
	}
}
