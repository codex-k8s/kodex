package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
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

// queryServerRootDeadlineProof observes exact native clock, custody and terminal counts without constructing business state.
// queryServerRootDeadlineProof читает точные native clocks, custody и terminal counts без подготовки business state.
//
//go:embed testdata/sql/server_root_deadline_proof.sql
var queryServerRootDeadlineProof string

// serverRootDeadlineProof preserves native identity and immutable evidence across timeout and replay.
// serverRootDeadlineProof сохраняет native identity и immutable evidence при timeout и replay.
type serverRootDeadlineProof struct {
	RootState, RootCode, RootProcessState                                    string
	RootVersion, RootAgentNodes, PlannedNodes, PlannedOpen, PlannedCancelled int64
	ActiveRuns, ActiveModels, ActiveInvocations, OpenGates, Events           int64
	StartedAt, DeadlineAt                                                    time.Time
	Custody                                                                  json.RawMessage
	EpisodeHistory                                                           []workflowExceptionEpisode
}

// serverRootDeadlineTrace emits only fixed statement labels, SQLSTATE and one allowlisted constraint name.
// serverRootDeadlineTrace выдаёт только фиксированные query labels, SQLSTATE и одно разрешённое имя constraint.
type serverRootDeadlineTrace struct{ t *testing.T }

// serverRootDeadlineTraceKey keeps the bounded statement label separate from arguments and error contents.
// serverRootDeadlineTraceKey отделяет bounded statement label от аргументов и содержимого ошибки.
type serverRootDeadlineTraceKey struct{}

// TraceQueryStart recognizes only the four deadline/closure queries involved in the regression.
// TraceQueryStart узнаёт только четыре deadline/closure query проверяемого перехода.
func (trace serverRootDeadlineTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	label := ""
	switch data.SQL {
	case queryRuntimeDeadlineExpired:
		label = "expired"
	case queryRuntimeDeadlineOwner:
		label = "owner"
	case queryOwnerGateEarlierExecution:
		label = "earlier"
	case queryRuntimeClaimFailGraph:
		label = "close"
	}
	if label != "" {
		return context.WithValue(ctx, serverRootDeadlineTraceKey{}, label)
	}
	return ctx
}

// TraceQueryEnd discards native error text, SQL and arguments while retaining the known constraint failure class.
// TraceQueryEnd отбрасывает native error text, SQL и аргументы, сохраняя известный constraint failure class.
func (trace serverRootDeadlineTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	label, ok := ctx.Value(serverRootDeadlineTraceKey{}).(string)
	if !ok {
		return
	}
	code, constraint := "NONE", "NONE"
	var cause *pgconn.PgError
	if errors.As(data.Err, &cause) {
		code = cause.Code
		constraint = "OTHER"
		if cause.ConstraintName == "run_nodes_materialization_check" {
			constraint = cause.ConstraintName
		}
	}
	trace.t.Logf("deadline stage=%s rows=%d sqlstate=%s constraint=%s", label, data.CommandTag.RowsAffected(), code, constraint)
}

// TestServerWorkflowRootDeadlineComponent uses real owner commands and clocks to cover planned/no-agent roots, autonomous expiry, late decisions and subsequent independent claims.
// TestServerWorkflowRootDeadlineComponent использует реальные owner commands и часы для planned/no-agent roots, autonomous expiry, поздних решений и следующего независимого claim.
func TestServerWorkflowRootDeadlineComponent(t *testing.T) {
	for _, scenario := range []string{"planned-recovery-autonomous", "all-materialized-autonomous", "planned-recovery-owner-decision", "all-materialized-owner-decision", "all-materialized-earlier-gate"} {
		t.Run(scenario, func(t *testing.T) { testServerWorkflowRootDeadline(t, scenario) })
	}
}

// testServerWorkflowRootDeadline never inserts or rewrites a ready business state; a fresh queued root must survive closure and be claimed without manually cancelling the overdue root.
// testServerWorkflowRootDeadline не вставляет и не переписывает готовое business state; свежий queued root переживает closure и исполняется без ручной отмены просроченного root.
func testServerWorkflowRootDeadline(t *testing.T, scenario string) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse deadline fixture database")
	}
	config.ConnConfig.Tracer = serverRootDeadlineTrace{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open deadline fixture database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	mutate := func(kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
		actor := owner
		if kind == command.ClaimExecution || kind == command.CompleteExecution {
			actor = worker
		}
		return service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "server-deadline-" + key, ExpectedVersion: version}, Payload: payload})
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := mutate(kind, key, version, payload)
		if err != nil {
			t.Fatalf("deadline native command: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Server root clock", Language: "ru"}).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "deadline-manager", "Менеджер разбора")
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "deadline-worker", "Исполнитель материала")
	agent = *execute(command.ChangeAgentCapability, "artifact", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	planned := strings.HasPrefix(scenario, "planned-")
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Проверка часов корня", Purpose: "Точный terminal envelope без исполнения planned anchors.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 12, ResultSchema: map[string]any{}}
	for index, key := range []string{"work", "after"} {
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agent.Ref, Instructions: "Вернуть report.md и точную декларацию результата.", TimeoutSeconds: 60, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}}
		if planned && index == 1 {
			step.DependsOn = []string{"work"}
		}
		if !planned {
			step.Parallel = true
			step.ParallelGroup = 1
			step.HumanGateAfter = true
			step.GateDecisions = []string{"APPROVE", "REJECT", "CANCEL"}
			if scenario == "all-materialized-earlier-gate" {
				seconds := int32(15)
				step.ApprovalTimeoutSeconds = &seconds
			}
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := execute(command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	launch := func(key string) *entity.Run {
		t.Helper()
		return execute(command.LaunchRun, key, nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Сохранить материал и точную историю решения."}).Run
	}
	root := launch("launch")
	read := func() serverRootDeadlineProof {
		t.Helper()
		var raw []byte
		var proof serverRootDeadlineProof
		if err := pool.QueryRow(ctx, queryServerRootDeadlineProof, pgx.StrictNamedArgs{"run_ref": root.Ref, "project_ref": project.Ref}).Scan(&raw); err != nil || json.Unmarshal(raw, &proof) != nil {
			t.Fatal("read native deadline proof")
		}
		return proof
	}
	claim := func(key string, limit int32) []map[string]any {
		t.Helper()
		return execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "server-root-deadline", Limit: limit}).RuntimeItems
	}
	complete := func(key string, lease map[string]any, summary string, artifact bool) {
		t.Helper()
		payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: summary}
		if artifact {
			body := []byte("Проверенный материал до deadline.\n")
			digest := sha256.Sum256(body)
			payload.Artifacts = []command.CompletedArtifact{{FileName: "report.md", MediaType: "text/markdown", Content: body, SizeBytes: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}}
		}
		execute(command.CompleteExecution, key, nil, payload)
	}
	work := claim("initial", 2)
	want := 2
	if planned {
		want = 1
	}
	if len(work) != want {
		t.Fatal("native server dispatch did not claim exact initial steps")
	}
	valid := `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`
	var gate entity.OwnerGate
	if planned {
		invalid := `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","kind":"ARTIFACT","fileName":"report.md","mediaType":"text/markdown","source":{"kind":"CURRENT_COMPLETION"}}]}`
		complete("invalid", work[0], invalid, false)
		managerWork := claim("manager", 1)
		if len(managerWork) != 1 || stringMap(managerWork[0], "agentRef") != manager.Ref {
			t.Fatal("invalid result did not dispatch exact exception manager")
		}
		complete("manager-complete", managerWork[0], "Схема результата отклонена; только владелец может разрешить новую попытку.", false)
	} else {
		for index, lease := range work {
			complete([]string{"complete-work", "complete-after"}[index], lease, valid, true)
		}
	}
	current, err := service.GetRun(ctx, owner, root.Ref)
	if err != nil || current.State != "WAITING_HUMAN" || len(current.GateRefs) == 0 {
		t.Fatal("native fixture did not reach owner waiting before deadline")
	}
	gate, err = service.GetOwnerGate(ctx, owner, current.GateRefs[0])
	if err != nil || gate.State != "OPEN" {
		t.Fatal("read exact native open gate")
	}
	before := read()
	if before.StartedAt.IsZero() || before.DeadlineAt.IsZero() || !before.DeadlineAt.After(time.Now()) || before.ActiveModels != 0 {
		t.Fatal("fixture did not retain a live root clock before waiting")
	}
	if planned {
		if before.PlannedOpen != 1 || len(before.EpisodeHistory) != 1 || before.EpisodeHistory[0].Reason != "RESULT_INVALID" || before.EpisodeHistory[0].State != "WAITING_OWNER" {
			t.Fatal("fixture did not retain exact RESULT_INVALID recovery with planned successor")
		}
	} else if before.RootAgentNodes != 0 || before.PlannedNodes != 0 || before.OpenGates != 2 {
		t.Fatal("all assigned anchors did not leave root without physical agent nodes")
	}
	independent := launch("independent")
	waitUntil := before.DeadlineAt
	if scenario == "all-materialized-earlier-gate" {
		if gate.ExpiresAt == nil || !gate.ExpiresAt.After(waitUntil) {
			t.Fatal("native gate clock did not follow root clock")
		}
		waitUntil = *gate.ExpiresAt
	}
	timer := time.NewTimer(time.Until(waitUntil.Add(150 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("native root deadline wait exhausted")
	case <-timer.C:
	}
	var fresh []map[string]any
	if strings.HasSuffix(scenario, "owner-decision") {
		decision := command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Позднее решение не должно открывать новую попытку."}
		_, err = mutate(command.ResolveOwnerGate, "late-owner", &gate.Version, decision)
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("late owner decision failed to commit exact runtime timeout", completionOutcomeClass(err))
		}
		closed := read()
		_, err = mutate(command.ResolveOwnerGate, "late-owner", &gate.Version, decision)
		if !errors.Is(err, errs.ErrForbidden) || !bytes.Equal(closed.Custody, read().Custody) || closed.Events != read().Events {
			t.Fatal("late owner replay changed historical outcome", completionOutcomeClass(err))
		}
	}
	fresh = claim("expiry-and-independent", 1)
	after := read()
	if after.RootState != "FAILED" || after.RootCode != "RUNTIME_TIMEOUT" || after.RootProcessState != "FAILED" || after.ActiveRuns+after.ActiveModels+after.ActiveInvocations+after.OpenGates != 0 || after.PlannedCancelled != before.PlannedNodes || !bytes.Equal(after.Custody, before.Custody) || !after.StartedAt.Equal(before.StartedAt) || !after.DeadlineAt.Equal(before.DeadlineAt) {
		t.Fatal("root timeout failed atomic materialization-safe closure and immutable custody")
	}
	if planned {
		if len(after.EpisodeHistory) != 1 || after.EpisodeHistory[0].State != "CANCELLED" || after.EpisodeHistory[0].Version != before.EpisodeHistory[0].Version+1 {
			t.Fatal("runtime timeout invented owner rejection or left recovery active")
		}
	}
	for _, ref := range current.GateRefs {
		closed, err := service.GetOwnerGate(ctx, owner, ref)
		if err != nil || closed.State != "CANCELLED" || closed.Decision != "CANCEL" {
			t.Fatal("root timeout left open or falsely expired owner gate")
		}
	}
	if len(fresh) != 1 || stringMap(fresh[0], "agentRef") != agent.Ref {
		t.Fatal("expired root blocked an independent fresh claim")
	}
	freshPhysical, err := service.GetRun(ctx, owner, stringMap(fresh[0], "runRef"))
	if err != nil || freshPhysical.RootRunRef != independent.Ref || freshPhysical.State != "RUNNING" {
		t.Fatal("independent claim belongs to another root or inactive execution")
	}
	independentNow, err := service.GetRun(ctx, owner, independent.Ref)
	if err != nil || independentNow.State != "RUNNING" {
		t.Fatal("independent native run did not retain its fresh claim")
	}
	execute(command.CancelRun, "cleanup-independent", &independentNow.Version, command.RunCommandInput{RunRef: independent.Ref})
	if len(claim("terminal-repoll", 1)) != 0 {
		t.Fatal("terminal root re-admitted model work")
	}
	final := read()
	if after.Events != final.Events || after.RootVersion != final.RootVersion || !bytes.Equal(after.Custody, final.Custody) {
		t.Fatal("terminal polling repeated graph closure")
	}
	_, err = mutate(command.ResolveOwnerGate, "closed-owner", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	if !errors.Is(err, errs.ErrAlreadyResolved) {
		t.Fatal("closed recovery gate reopened after root timeout", completionOutcomeClass(err))
	}
	if read().Events != final.Events {
		t.Fatal("closed gate changed timeout history")
	}
}
