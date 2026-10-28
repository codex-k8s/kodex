package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowExceptionParallelProof observes root, logical journals and native model cardinalities.
// queryWorkflowExceptionParallelProof читает root, logical journals и количество native model executions.
//
//go:embed testdata/sql/workflow_exception_parallel_proof.sql
var queryWorkflowExceptionParallelProof string

// queryWorkflowExceptionDelete probes an unauthorized durable-history deletion in a rollback-only transaction.
// queryWorkflowExceptionDelete проверяет отказ удаления durable history в rollback-only transaction.
//
//go:embed testdata/sql/workflow_exception_delete.sql
var queryWorkflowExceptionDelete string

// queryWorkflowExceptionQueuedLock models concurrent claim contention without changing business state;
// it locks only the exact owner-created A2 row and deliberately takes no project/root lock.
// queryWorkflowExceptionQueuedLock моделирует конкурирующий claim без изменения business state:
// блокируется только exact owner-created A2 row, без project/root lock.
//
//go:embed testdata/sql/workflow_exception_queued_lock.sql
var queryWorkflowExceptionQueuedLock string

// workflowRecoveryRegression holds one isolated owner fixture; it never constructs business rows by SQL.
// workflowRecoveryRegression хранит isolated owner fixture, не создавая business rows через SQL.
type workflowRecoveryRegression struct {
	t                              *testing.T
	ctx                            context.Context
	pool                           *pgxpool.Pool
	service                        *platformservice.Service
	owner, worker                  value.Principal
	projectRef, runRef, managerRef string
	agents                         map[string]string
	repository                     *Repository
}

// workflowRecoverySnapshot preserves complete immutable episode and business history for assertions.
// workflowRecoverySnapshot сохраняет целые immutable episode и business history для проверок.
type workflowRecoverySnapshot struct {
	state                       string
	episodes                    []workflowExceptionEpisode
	journals                    map[string][]entity.WorkflowAttemptResult
	managers, leases, physicals int64
}

// newWorkflowRecoveryRegression publishes a real workflow; the expiry variant pins a one-second
// recovery wait through the step's published owner policy, without changing database clocks.
// newWorkflowRecoveryRegression публикует настоящий workflow; expiry-вариант закрепляет одну секунду
// recovery wait опубликованной owner policy шага без изменения часов БД.
func newWorkflowRecoveryRegression(t *testing.T, expiry bool) *workflowRecoveryRegression {
	t.Helper()
	return newWorkflowGateRegression(t, expiry, false)
}

// newWorkflowGateRegression publishes native recovery or result gates with the same exact principals.
// No gate, journal or lease is synthesized by SQL; result gates retain published decisions.
// newWorkflowGateRegression публикует native recovery либо result gates с теми же exact principals.
// SQL не создаёт gate, journal или lease; result gates сохраняют опубликованные решения.
func newWorkflowGateRegression(t *testing.T, expiry, resultGates bool) *workflowRecoveryRegression {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated recovery regression database")
	}
	t.Cleanup(pool.Close)
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, service: service, owner: owner, worker: worker, agents: map[string]string{}, repository: repository}
	project := f.execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Parallel recovery", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "regression-manager", "Менеджер разбора")
	f.managerRef = manager.Ref
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Параллельные отказы", Purpose: "Проверить последовательный разбор всех отказов.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}}
	keys := []string{"a", "b"}
	if expiry {
		keys = keys[:1]
	}
	for index, key := range keys {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "regression-worker-"+key, key)
		agent = *f.execute(command.ChangeAgentCapability, "capability-"+key, &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		f.agents[key] = agent.Ref
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agent.Ref, Instructions: "Создать обязательный материал и точную декларацию результата.", TimeoutSeconds: 120, Parallel: true, ParallelGroup: 1, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: key + ".md", MediaType: "text/markdown"}}}
		if expiry || resultGates {
			step.HumanGateAfter = true
			step.GateDecisions = []string{"APPROVE", "REJECT", "CANCEL"}
		}
		if expiry {
			ttl := int32(1)
			step.ApprovalTimeoutSeconds = &ttl
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := f.execute(command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	f.runRef = f.execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать обязательные материалы.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run.Ref
	return f
}

// execute follows the existing service authorization/idempotency path with exact resolved principals.
// execute использует существующий service authorization/idempotency path с exact resolved principals.
func (f *workflowRecoveryRegression) execute(kind command.Kind, key string, version *int64, payload any) command.Result {
	f.t.Helper()
	actor := f.owner
	if kind == command.ClaimExecution || kind == command.CompleteExecution {
		actor = f.worker
	}
	result, err := f.service.Execute(f.ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "recovery-regression-" + key, ExpectedVersion: version}, Payload: payload})
	if err != nil {
		f.t.Fatalf("native recovery regression: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
	}
	return result
}

// read uses a SELECT-only proof, not a prepared expected state or a model's self-reported success.
// read использует SELECT-only proof, а не подготовленное ожидаемое состояние или заявление модели.
func (f *workflowRecoveryRegression) read() workflowRecoverySnapshot {
	f.t.Helper()
	var snapshot workflowRecoverySnapshot
	if err := f.pool.QueryRow(f.ctx, queryWorkflowExceptionParallelProof, pgx.StrictNamedArgs{"run_ref": f.runRef, "project_ref": f.projectRef}).Scan(&snapshot.state, &snapshot.episodes, &snapshot.journals, &snapshot.managers, &snapshot.leases, &snapshot.physicals); err != nil {
		f.t.Fatal("read native parallel recovery proof", completionOutcomeClass(err))
	}
	return snapshot
}

// claims requests at most two actual leases without assigning their ordering or identities.
// claims запрашивает максимум две настоящие аренды, не назначая порядок либо identity.
func (f *workflowRecoveryRegression) claims(key string) []map[string]any {
	f.t.Helper()
	return f.execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "recovery-regression-fixture", Limit: 2}).RuntimeItems
}

// complete reconstructs one native intent; valid output includes real bytes and exact SHA-256.
// complete заново строит native intent; допустимый результат включает настоящие bytes и exact SHA-256.
func (f *workflowRecoveryRegression) complete(key string, lease map[string]any, file string, valid bool) {
	f.t.Helper()
	payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`}
	if file == "" {
		payload.ResultSummary = "The missing material needs an owner-approved fresh attempt; this analysis cannot waive any predicate."
	} else if valid {
		body := []byte("Проверенный материал " + file + "\n")
		sum := sha256.Sum256(body)
		payload.Artifacts = []command.CompletedArtifact{{FileName: file, MediaType: "text/markdown", Content: body, SizeBytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}}
	}
	f.execute(command.CompleteExecution, key, nil, payload)
}

// recoveryGate reads the exact server-pinned gate rather than discovering an unrelated open gate.
// recoveryGate читает exact server-pinned gate, а не произвольный открытый Gate.
func (f *workflowRecoveryRegression) recoveryGate(snapshot workflowRecoverySnapshot) entity.OwnerGate {
	f.t.Helper()
	if len(snapshot.episodes) == 0 {
		f.t.Fatal("recovery episode missing")
	}
	gate, err := f.service.GetOwnerGate(f.ctx, f.owner, snapshot.episodes[len(snapshot.episodes)-1].GateRef)
	if err != nil || gate.State != "OPEN" {
		f.t.Fatal("exact recovery gate is not open")
	}
	assertOwnerGateApprovalConsequence(f.t, gate, false, false)
	return gate
}

// TestWorkflowExceptionParallelRecoveryComponent proves that one active episode does not erase a
// sibling's committed rejection and that a newer queued attempt never replays the prior failure.
// TestWorkflowExceptionParallelRecoveryComponent доказывает, что активный episode не теряет committed
// отказ соседней ветви, а новая queued attempt не повторяет обработку прежнего отказа.
func TestWorkflowExceptionParallelRecoveryComponent(t *testing.T) {
	f := newWorkflowRecoveryRegression(t, false)
	first := f.claims("initial")
	if len(first) != 2 {
		t.Fatal("two independent stages were not admitted together")
	}
	leases := map[string]map[string]any{}
	for _, lease := range first {
		for key, agent := range f.agents {
			if stringMap(lease, "agentRef") == agent {
				leases[key] = lease
			}
		}
	}
	if len(leases) != 2 {
		t.Fatal("initial claims did not identify both published stages")
	}
	f.complete("fail-a", leases["a"], "a.md", false)
	firstFailure := f.read()
	f.complete("fail-b", leases["b"], "b.md", false)
	both := f.read()
	if len(both.episodes) != 1 || !reflect.DeepEqual(firstFailure.episodes, both.episodes) || both.episodes[0].StepKey != "a" || len(both.journals["a"]) != 1 || len(both.journals["b"]) != 1 || both.journals["b"][0].Accepted {
		t.Fatal("parallel rejection replaced or lost the first episode or sibling journal")
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin isolated durable episode delete probe")
	}
	_, cause := tx.Exec(f.ctx, queryWorkflowExceptionDelete, pgx.StrictNamedArgs{"run_ref": f.runRef, "project_ref": f.projectRef})
	var driver *pgconn.PgError
	denied := errors.As(cause, &driver) && driver.Code == "23514" && driver.ConstraintName == "workflow_exception_delete_authority"
	rollbackWorkflowJournalProbe(t, f.ctx, tx)
	if !denied || !reflect.DeepEqual(both, f.read()) {
		t.Fatal("durable exception history was deleted outside exact purge authority")
	}
	manager := f.claims("manager-a")
	if len(manager) != 1 || stringMap(manager[0], "agentRef") != f.managerRef {
		t.Fatal("first exception manager was not unique")
	}
	f.complete("analysis-a", manager[0], "", false)
	gate := f.recoveryGate(f.read())
	f.execute(command.ResolveOwnerGate, "approve-a", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Повторить первую ветвь."})
	afterApproval := f.read()
	if len(afterApproval.episodes) != 2 || afterApproval.episodes[0].State != "RECOVERED" || afterApproval.episodes[1].StepKey != "b" || afterApproval.episodes[1].State != "MANAGER_QUEUED" || afterApproval.managers != 2 {
		t.Fatal("outstanding sibling rejection was not scheduled after the first recovery")
	}
	contended, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin exact queued-row contention")
	}
	released := false
	defer func() {
		if !released {
			rollbackWorkflowJournalProbe(t, f.ctx, contended)
		}
	}()
	var queuedRef string
	if err := contended.QueryRow(f.ctx, queryWorkflowExceptionQueuedLock, pgx.StrictNamedArgs{"run_ref": f.runRef, "project_ref": f.projectRef, "node_id": afterApproval.episodes[0].RecoveryNodeID, "agent_ref": f.agents["a"]}).Scan(&queuedRef); err != nil {
		t.Fatal("lock exact owner-created queued A2 row", completionOutcomeClass(err))
	}
	managerB := f.claims("manager-b-skips-contended-a2")
	if len(managerB) != 1 || stringMap(managerB[0], "agentRef") != f.managerRef {
		t.Fatal("native SKIP LOCKED did not skip the exact contended A2 row")
	}
	rollbackWorkflowJournalProbe(t, f.ctx, contended)
	released = true
	f.complete("analysis-b", managerB[0], "", false)
	gate = f.recoveryGate(f.read())
	waiting := f.read()
	if waiting.state != "WAITING_HUMAN" || waiting.leases != 0 || waiting.physicals != 3 {
		t.Fatal("second gate did not leave A2 queued under owner wait")
	}
	_, err = f.service.Execute(f.ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: f.owner, Mutation: value.Mutation{IdempotencyKey: "recovery-regression-premature-b", ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Нельзя обойти незавершённую A2."}})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatal("recovery approval bypassed a queued sibling attempt")
	}
	untouched, err := f.service.GetOwnerGate(f.ctx, f.owner, gate.Ref)
	if err != nil || untouched.State != "OPEN" || untouched.Version != gate.Version || !reflect.DeepEqual(waiting, f.read()) {
		t.Fatal("denied approval changed the gate, episode or queued attempt")
	}
	resumed := f.claims("a2-while-b-gate-open")
	if len(resumed) != 1 || stringMap(resumed[0], "agentRef") != f.agents["a"] || stringMap(resumed[0], "nodeRef") != queuedRef || stringMap(resumed[0], "runRef") == stringMap(leases["a"], "runRef") {
		t.Fatal("owner wait on independent B gate prevented the exact assigned A2 claim")
	}
	f.complete("a2-success", resumed[0], "a.md", true)
	snapshot := f.read()
	if len(snapshot.episodes) != 2 || snapshot.episodes[0].State != "RECOVERED" || snapshot.state != "WAITING_HUMAN" || snapshot.leases != 0 {
		t.Fatal("A2 completion lost the pending B owner decision or created another exception")
	}
	f.execute(command.ResolveOwnerGate, "approve-b", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Повторить вторую ветвь."})
	secondB := f.claims("b2")
	if len(secondB) != 1 || stringMap(secondB[0], "agentRef") != f.agents["b"] {
		t.Fatal("second branch did not receive exactly one fresh attempt")
	}
	f.complete("b2-success", secondB[0], "b.md", true)
	final := f.read()
	if final.state != "SUCCEEDED" || len(final.episodes) != 2 || final.episodes[1].State != "RECOVERED" || final.managers != 2 || final.leases != 0 || final.physicals != 4 {
		t.Fatal("parallel recovery left the root stalled or fabricated a terminal state")
	}
	for _, key := range []string{"a", "b"} {
		journal := final.journals[key]
		if len(journal) != 2 || journal[0].Accepted || !journal[1].Accepted || journal[1].Attempt != 2 {
			t.Fatal("parallel recovery changed prior business outcomes")
		}
	}
	if len(f.claims("terminal")) != 0 {
		t.Fatal("terminal parallel recovery admitted another execution")
	}
}

// TestWorkflowExceptionRecoveryExpiryComponent exercises real server expiry. A FAILED root with
// EXPIRED gate/decisionNULL must close the recovery episode as CANCELLED, never human REJECTED.
// TestWorkflowExceptionRecoveryExpiryComponent проверяет настоящее server expiry: FAILED root с
// EXPIRED gate/decisionNULL закрывает recovery episode как CANCELLED, не human REJECTED.
func TestWorkflowExceptionRecoveryExpiryComponent(t *testing.T) {
	f := newWorkflowRecoveryRegression(t, true)
	worker := f.claims("worker")
	if len(worker) != 1 {
		t.Fatal("expiry producer missing")
	}
	f.complete("missing-result", worker[0], "a.md", false)
	manager := f.claims("manager")
	if len(manager) != 1 {
		t.Fatal("expiry analysis manager missing")
	}
	f.complete("analysis", manager[0], "", false)
	gate := f.recoveryGate(f.read())
	if gate.ExpiresAt == nil {
		t.Fatal("published recovery timeout was not pinned")
	}
	wait := time.Until(*gate.ExpiresAt) + 50*time.Millisecond
	if wait > 2*time.Second {
		t.Fatal("fixture recovery expiry exceeds its one-second policy")
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			t.Fatal("recovery expiry fixture deadline")
		}
	}
	if len(f.claims("expire")) != 0 {
		t.Fatal("expired recovery admitted a model")
	}
	expired, err := f.service.GetOwnerGate(f.ctx, f.owner, gate.Ref)
	if err != nil || expired.State != "EXPIRED" || expired.Decision != "" {
		t.Fatal("server expiry fabricated a human decision")
	}
	terminal := f.read()
	if terminal.state != "FAILED" || len(terminal.episodes) != 1 || terminal.episodes[0].State != "CANCELLED" || terminal.leases != 0 {
		t.Fatal("system failure was misclassified as human rejection")
	}
	project, err := f.service.GetProject(f.ctx, f.owner, f.projectRef)
	if err != nil {
		t.Fatal("read project before native episode purge")
	}
	trashed := f.execute(command.TrashProject, "trash", &project.Version, command.ProjectLifecycleInput{Ref: f.projectRef}).Project
	f.execute(command.PurgeProject, "purge", &trashed.Version, command.ProjectLifecycleInput{Ref: f.projectRef})
	candidates, err := f.repository.ListPendingProjectPurges(f.ctx, 2)
	if err != nil || len(candidates) != 1 || candidates[0].ProjectRef != f.projectRef {
		t.Fatal("exact episode purge candidate missing")
	}
	inventory, err := f.repository.ProjectPurgeInventory(f.ctx, candidates[0])
	if err != nil {
		t.Fatal("read native episode purge inventory")
	}
	for _, item := range inventory {
		if item.Kind != "PVC" {
			t.Fatal("empty episode fixture unexpectedly contains an external object")
		}
	}
	if err := f.repository.FinalizeProjectPurge(f.ctx, candidates[0], inventory); err != nil {
		t.Fatal("authorized episode purge failed", completionOutcomeClass(err))
	}
	if err := f.repository.FinalizeProjectPurge(f.ctx, candidates[0], inventory); err != nil {
		t.Fatal("episode purge replay failed", completionOutcomeClass(err))
	}
	var nodes, runs, targets, contexts int64
	var receipt string
	if err := f.pool.QueryRow(f.ctx, queryWorkflowCapturePurgeProof, pgx.StrictNamedArgs{"node_ref": stringMap(worker[0], "nodeRef"), "run_ref": f.runRef, "project_ref": f.projectRef}).Scan(&nodes, &runs, &targets, &contexts, &receipt); err != nil || nodes != 0 || runs != 0 || targets != 0 || contexts != 0 || receipt != "DONE" {
		t.Fatal("authorized episode purge left rows or authority context")
	}
}
