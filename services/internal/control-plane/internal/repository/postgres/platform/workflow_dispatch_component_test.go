package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowDispatchProof observes native dispatch without constructing planned nodes, leases,
// accepted receipts or other business state through SQL. Its only statement is a scoped SELECT.
// queryWorkflowDispatchProof наблюдает native dispatch без SQL-подготовки planned nodes, leases,
// принятых результатов или другого business state. Единственная команда — ограниченный SELECT.
//
//go:embed testdata/sql/workflow_dispatch_proof.sql
var queryWorkflowDispatchProof string

// workflowDispatchTrace reports only closed query names, row counts and SQLSTATE, never arguments.
// workflowDispatchTrace сообщает только закрытые имена запросов, число строк и SQLSTATE без аргументов.
type workflowDispatchTrace struct{ t *testing.T }

// workflowDispatchTraceKey isolates the safe query label in the trace context.
// workflowDispatchTraceKey изолирует безопасное имя запроса в контексте трассировки.
type workflowDispatchTraceKey struct{}

// TraceQueryStart selects only repository-owned workflow statements for bounded diagnostics.
// TraceQueryStart выбирает только принадлежащие репозиторию workflow-запросы для диагностики.
func (trace workflowDispatchTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for name, query := range map[string]string{
		"capture-context": queryWorkflowCaptureContext, "capture-record": queryWorkflowCaptureRecord,
		"completion": queryWorkflowDispatchCompletion, "result-lineage": queryWorkflowResultLineage,
		"journal-append": queryWorkflowAttemptAppend, "anchors": queryWorkflowDispatchAnchors,
		"materialize": queryWorkflowDispatchMaterialize,
	} {
		if data.SQL == query {
			return context.WithValue(ctx, workflowDispatchTraceKey{}, name)
		}
	}
	return ctx
}

// TraceQueryEnd excludes SQL text, arguments and database error details from test output.
// TraceQueryEnd исключает SQL, аргументы и подробности ошибок БД из вывода теста.
func (trace workflowDispatchTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	name, ok := ctx.Value(workflowDispatchTraceKey{}).(string)
	if !ok {
		return
	}
	code := "none"
	var failure *pgconn.PgError
	if errors.As(data.Err, &failure) {
		code = failure.Code
	}
	trace.t.Logf("workflow query=%s rows=%d failed=%t sqlstate=%s", name, data.CommandTag.RowsAffected(), data.Err != nil, code)
}

// workflowDispatchExecution retains owner-issued runtime coordinates for comparison with the claim.
// workflowDispatchExecution хранит назначенные владельцем координаты для сравнения с claim.
type workflowDispatchExecution struct {
	RevisionID, RevisionRef, TurnID, RunID, RootRunID, NodeID string
	LeaseID, LeaseRef, LeaseState                             string
	RuntimeAttempt                                            int32
	Generation                                                int64
}

// workflowDispatchInput preserves the complete accepted predecessor receipt, including its pins.
// workflowDispatchInput сохраняет полный принятый результат предшественника вместе с pins.
type workflowDispatchInput struct {
	StepKey, SourceNodeID string
	Receipt               entity.WorkflowAttemptResult
}

// workflowDispatchNode separates logical anchors, physical execution and immutable input evidence.
// workflowDispatchNode разделяет logical anchor, physical execution и неизменяемые входные данные.
type workflowDispatchNode struct {
	ID, Ref, StepKey, AgentRef, State, RunRef, SessionID, TurnID, TurnRef, MaterializationState string
	RuntimeAttempt, BusinessAttempt                                                             int32
	Inputs                                                                                      []workflowDispatchInput
	History                                                                                     []entity.WorkflowAttemptResult
	Capture                                                                                     map[string]any
	Executions                                                                                  []workflowDispatchExecution
}

// workflowDispatchProof records both root progress and the absence of any manager execution.
// workflowDispatchProof фиксирует состояние root и отсутствие любых исполнений manager.
type workflowDispatchProof struct {
	RootID, RootState                                    string
	ManagerExecutions, ActiveAgentExecutions, OpenLeases int64
	Anchors                                              []workflowDispatchNode
}

// TestWorkflowDispatchComponent executes a published server DAG through native owner commands.
// One producer's admitted ARTIFACT enables two parallel consumers with their own ARTIFACT results;
// REVIEW semantics requiring an actual PR/head are deliberately outside this scenario. No SQL
// prepares business rows and no evaluator, journal or dispatch helper is invoked by the fixture.
// TestWorkflowDispatchComponent исполняет опубликованный серверный DAG через native owner commands.
// ARTIFACT производителя открывает двух параллельных потребителей с собственными ARTIFACT-результатами;
// REVIEW с настоящими PR/head проверяется отдельно. SQL не готовит business rows, оснастка не вызывает
// evaluator, journal или dispatch helper напрямую.
func TestWorkflowDispatchComponent(t *testing.T) {
	t.Run("artifact-to-two-reviewers", testWorkflowDispatchArtifactFanout)
}

// testWorkflowDispatchArtifactFanout keeps one isolated database and bounded context for all commands;
// completion replay reconstructs the original bytes so internal preparation cannot change its intent.
// testWorkflowDispatchArtifactFanout использует одну isolated DB и bounded context; повтор completion
// заново создаёт исходные bytes, чтобы внутренняя подготовка не изменила intent.
func testWorkflowDispatchArtifactFanout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated workflow dispatch database")
	}
	config.ConnConfig.Tracer = workflowDispatchTrace{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open isolated workflow dispatch database")
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
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "workflow-dispatch-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native workflow dispatch command: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil,
		command.ProjectInput{Name: "Native server dispatch", Language: "ru"}).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "dispatch-manager", "Менеджер без orchestration")
	agents := map[string]entity.Agent{}
	for _, key := range []string{"producer", "review-a", "review-b"} {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "dispatch-"+key, key)
		agents[key] = *execute(command.ChangeAgentCapability, "capability-"+key, &agent.Version,
			command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Материал и две независимые проверки",
		Purpose: "Проверить настоящий серверный dispatch без хода manager.", CoordinatorAgentRef: manager.Ref,
		Concurrency: 2, TimeoutSeconds: 240, CompletionCriteria: "Все три обязательных материала приняты владельцем.",
		ResultSchema: map[string]any{}}
	fileNames := map[string]string{"producer": "report.md", "review-a": "review-a.md", "review-b": "review-b.md"}
	for index, key := range []string{"producer", "review-a", "review-b"} {
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agents[key].Ref,
			Instructions:   "Вернуть назначенный текстовый материал и точную декларацию результата.",
			ExpectedResult: fileNames[key], TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability},
			ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: fileNames[key], MediaType: "text/markdown"}}}
		if key != "producer" {
			step.DependsOn, step.Parallel, step.ParallelGroup = []string{"producer"}, true, 1
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := execute(command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref,
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	if workflow.Published == nil || workflow.Published.ExecutionMode != "SERVER_DAG_V1" {
		t.Fatal("native publication lost the server execution policy")
	}
	run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
		Task:   "Создать материал и получить два независимых материала проверки.",
		Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	read := func() workflowDispatchProof {
		t.Helper()
		var raw []byte
		if err := pool.QueryRow(ctx, queryWorkflowDispatchProof, pgx.StrictNamedArgs{
			"run_ref": run.Ref, "project_ref": project.Ref, "manager_ref": manager.Ref,
		}).Scan(&raw); err != nil {
			t.Fatal("read native workflow dispatch proof", completionOutcomeClass(err))
		}
		var proof workflowDispatchProof
		if json.Unmarshal(raw, &proof) != nil || len(proof.Anchors) != 3 || proof.ManagerExecutions != 0 {
			t.Fatal("native dispatch changed the anchor set or executed the manager")
		}
		return proof
	}
	claim := func(key string, limit int32) []map[string]any {
		return execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "workflow-dispatch-fixture", Limit: limit}).RuntimeItems
	}
	initial := read()
	if initial.RootState != "RUNNING" {
		t.Fatal("native server workflow was not started")
	}
	producerItems := claim("producer-claim", 2)
	if len(producerItems) != 1 || stringMap(producerItems[0], "agentRef") != agents["producer"].Ref {
		t.Fatal("dispatch ran a dependent or manager before the producer result")
	}
	producerClaim := producerItems[0]
	assertWorkflowDispatchClaim(t, read(), "producer", producerClaim)
	if len(claim("producer-exclusive", 2)) != 0 {
		t.Fatal("dependent execution became claimable before an accepted producer receipt")
	}
	complete := func(key string, lease map[string]any, file string) command.Result {
		t.Helper()
		body := []byte("Проверенный native материал: " + file + "\n")
		digest := sha256.Sum256(body)
		return execute(command.CompleteExecution, key, nil, command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"),
			Success: true, Usage: turnUsageFixture(),
			ResultSummary: `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`,
			Artifacts: []command.CompletedArtifact{{FileName: file, MediaType: "text/markdown", Content: body,
				SizeBytes: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}}})
	}
	complete("producer-complete", producerClaim, fileNames["producer"])
	afterProducer := read()
	producer := assertWorkflowDispatchAccepted(t, afterProducer, "producer", fileNames["producer"])
	if afterProducer.RootState != "RUNNING" {
		t.Fatal("root completed before both mandatory dependent results")
	}
	complete("producer-complete", producerClaim, fileNames["producer"])
	if !reflect.DeepEqual(afterProducer, read()) {
		t.Fatal("same-intent completion replay changed dispatch or accepted provenance")
	}
	reviewItems := claim("parallel-review-claims", 2)
	if len(reviewItems) != 2 {
		t.Fatal("both independent reviewers were not claimable together")
	}
	reviewClaims := map[string]map[string]any{}
	for _, lease := range reviewItems {
		for _, key := range []string{"review-a", "review-b"} {
			if stringMap(lease, "agentRef") == agents[key].Ref {
				if reviewClaims[key] != nil {
					t.Fatal("parallel dispatch duplicated one reviewer")
				}
				reviewClaims[key] = lease
			}
		}
	}
	parallel := read()
	if len(reviewClaims) != 2 || parallel.ActiveAgentExecutions != 2 || parallel.OpenLeases != 2 {
		t.Fatal("parallel dispatch did not retain two distinct active executions")
	}
	manifestReader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	metadataReader := runtimeFilesTestPrincipal(t, ctx, repository, "metadata")
	bodyReader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller",
		Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	for _, key := range []string{"review-a", "review-b"} {
		lease := reviewClaims[key]
		assertWorkflowDispatchClaim(t, parallel, key, lease)
		node := workflowDispatchStep(t, parallel, key)
		if len(node.Inputs) != 1 || node.Inputs[0].StepKey != "producer" || node.Inputs[0].SourceNodeID != producer.ID ||
			!reflect.DeepEqual(node.Inputs[0].Receipt, producer.History[0]) {
			t.Fatal("dependent input does not pin the exact accepted producer receipt")
		}
		accepted := producer.History[0].Results[0]
		task := stringMap(lease, "task")
		for _, pin := range []string{accepted.ArtifactRef, accepted.ArtifactRevision, accepted.Digest} {
			if pin == "" || !strings.Contains(task, pin) {
				t.Fatal("reviewer task omitted an exact accepted predecessor artifact pin")
			}
		}
		for _, private := range []string{producer.History[0].RuntimeRevisionID, producer.Executions[0].LeaseID,
			`"provenance"`, `"runtimeRevisionId"`, `"leaseId"`} {
			if private != "" && strings.Contains(task, private) {
				t.Fatal("reviewer prompt exposed private predecessor execution provenance")
			}
		}
		catalog, ok := lease["fileCatalog"].(runtimecontract.RuntimeFileCatalog)
		if !ok || !slices.Contains(catalog.Purposes, runtimecontract.FilePurposeRunResult) {
			t.Fatal("reviewer claim omitted its accepted predecessor file catalog")
		}
		execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
		page, err := service.GetExecutionFileManifest(ctx, manifestReader, execution, query.Page{Size: 100})
		if err != nil || page.Catalog.Ref != catalog.Ref || page.Catalog.Digest != catalog.Digest || len(page.Items) != 1 || page.Total != 1 {
			t.Fatal("accepted predecessor was not materialized into the exact reviewer file catalog")
		}
		file := page.Items[0]
		revision, err := service.GetArtifactRevision(ctx, owner, accepted.ArtifactRef, accepted.ArtifactRevision)
		if err != nil || file.ArtifactRef != accepted.ArtifactRef || file.Revision != revision.Revision ||
			file.Digest != "sha256:"+accepted.Digest || file.Name != "report.md" {
			t.Fatal("reviewer catalog substituted an accepted predecessor revision")
		}
		exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
		if _, err := service.GetExecutionFileMetadata(ctx, metadataReader, execution, exact); err != nil {
			t.Fatal("reviewer could not read its accepted predecessor metadata", completionOutcomeClass(err))
		}
		download, err := service.ReadExecutionArtifact(ctx, bodyReader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef)
		if err != nil {
			t.Fatal("reviewer could not read its accepted predecessor bytes", completionOutcomeClass(err))
		}
		body, readErr := io.ReadAll(io.LimitReader(download.Reader, 1024))
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil || string(body) != "Проверенный native материал: report.md\n" {
			t.Fatal("reviewer read changed the exact admitted predecessor bytes")
		}
	}
	complete("review-a-complete", reviewClaims["review-a"], fileNames["review-a"])
	afterFirstReview := read()
	assertWorkflowDispatchAccepted(t, afterFirstReview, "review-a", fileNames["review-a"])
	if afterFirstReview.RootState != "RUNNING" || afterFirstReview.OpenLeases != 1 {
		t.Fatal("root completed while the other mandatory reviewer was active")
	}
	complete("review-b-complete", reviewClaims["review-b"], fileNames["review-b"])
	final := read()
	if final.RootState != "SUCCEEDED" || final.OpenLeases != 0 || final.ActiveAgentExecutions != 0 {
		t.Fatal("accepted DAG results did not atomically finish the root and close execution leases")
	}
	childRuns, sessions := map[string]bool{}, map[string]bool{}
	for _, key := range []string{"producer", "review-a", "review-b"} {
		node := assertWorkflowDispatchAccepted(t, final, key, fileNames[key])
		if childRuns[node.RunRef] || sessions[node.SessionID] || node.RunRef == run.Ref || node.SessionID == "" {
			t.Fatal("independent DAG stages reused a root or another stage's physical session")
		}
		childRuns[node.RunRef], sessions[node.SessionID] = true, true
		result := node.History[0].Results[0]
		artifact, err := service.GetArtifactRevision(ctx, owner, result.ArtifactRef, result.ArtifactRevision)
		if err != nil || artifact.ScanState != "CLEAN" || artifact.Digest != "sha256:"+result.Digest {
			t.Fatal("accepted result is not the exact CLEAN owner artifact revision")
		}
	}
	publicRun, graph, err := service.GetRunGraphSnapshot(ctx, owner, run.Ref)
	if err != nil || publicRun.State != "SUCCEEDED" {
		t.Fatal("owner graph did not expose terminal server dispatch")
	}
	accepted := 0
	for _, node := range graph.Nodes {
		if node.AgentRef == manager.Ref && node.Type == "AGENT_EXECUTION" {
			t.Fatal("public graph contains a manager execution")
		}
		if len(node.WorkflowResults) != 0 {
			if len(node.WorkflowResults) != 1 || !node.WorkflowResults[0].Accepted ||
				node.WorkflowResults[0].RuntimeRevisionID != "" || node.WorkflowResults[0].Provenance != nil {
				t.Fatal("public graph changed the accepted business history")
			}
			accepted++
		}
	}
	if accepted != 3 {
		t.Fatalf("terminal DAG public accepted histories: got=%d want=3", accepted)
	}
	if len(claim("terminal-claim", 2)) != 0 {
		t.Fatal("terminal DAG admitted another execution")
	}
	complete("review-b-complete", reviewClaims["review-b"], fileNames["review-b"])
	if !reflect.DeepEqual(final, read()) {
		t.Fatal("terminal completion replay duplicated or changed DAG outcomes")
	}
}

// workflowDispatchStep requires one exact immutable anchor in the independently read graph.
// workflowDispatchStep требует один точный неизменяемый anchor в независимо прочитанном графе.
func workflowDispatchStep(t *testing.T, proof workflowDispatchProof, key string) workflowDispatchNode {
	t.Helper()
	for _, node := range proof.Anchors {
		if node.StepKey == key {
			return node
		}
	}
	t.Fatal("exact server-assigned workflow anchor missing")
	return workflowDispatchNode{}
}

// assertWorkflowDispatchClaim binds the returned fence/generation scope to stored first-attempt rows.
// assertWorkflowDispatchClaim связывает возвращённые fence/generation с rows первой physical attempt.
func assertWorkflowDispatchClaim(t *testing.T, proof workflowDispatchProof, key string, claim map[string]any) {
	t.Helper()
	node := workflowDispatchStep(t, proof, key)
	if node.BusinessAttempt != 1 || node.RuntimeAttempt != 1 || node.MaterializationState != "MATERIALIZED" || len(node.Executions) != 1 {
		t.Fatal("claim did not originate from a materialized first business attempt")
	}
	execution := node.Executions[0]
	if node.Ref != stringMap(claim, "nodeRef") || node.RunRef != stringMap(claim, "runRef") || node.AgentRef != stringMap(claim, "agentRef") ||
		node.TurnRef != stringMap(claim, "turnRef") || execution.RevisionRef != stringMap(claim, "runtimeRevisionRef") ||
		execution.LeaseRef != stringMap(claim, "leaseRef") || execution.Generation != runtimeRevisionMapInt64(claim, "generation") ||
		execution.NodeID != node.ID || execution.TurnID != node.TurnID || execution.RootRunID != proof.RootID ||
		execution.RuntimeAttempt != 1 || execution.LeaseState != "CLAIMED" || len(node.History) != 0 || node.Capture != nil {
		t.Fatal("claim lost exact node/run/turn/revision/lease/generation provenance")
	}
}

// assertWorkflowDispatchAccepted compares owner capture, committed journal and immutable content pins.
// assertWorkflowDispatchAccepted сравнивает owner capture, committed journal и immutable content pins.
func assertWorkflowDispatchAccepted(t *testing.T, proof workflowDispatchProof, key, file string) workflowDispatchNode {
	t.Helper()
	node := workflowDispatchStep(t, proof, key)
	if node.State != "SUCCEEDED" || len(node.History) != 1 || len(node.Executions) != 1 || node.Capture == nil {
		t.Fatal("completed workflow step has no single captured accepted history")
	}
	entry, execution := node.History[0], node.Executions[0]
	if !entry.Accepted || entry.Reason != "" || entry.Attempt != 1 || entry.RunRef != node.RunRef ||
		entry.RuntimeRevisionID != execution.RevisionID || len(entry.Results) != 1 || entry.Provenance == nil ||
		entry.Provenance.RootRunID != proof.RootID || entry.Provenance.OriginNodeID != node.ID || entry.Provenance.NodeID != node.ID ||
		entry.Provenance.LeaseID != execution.LeaseID || entry.Provenance.Generation != execution.Generation ||
		entry.Provenance.TurnID != node.TurnID || entry.Provenance.RuntimeAttempt != 1 || execution.LeaseState != "COMPLETED" {
		t.Fatal("accepted workflow receipt lost exact owner-issued completed provenance")
	}
	entries, ok := node.Capture["artifacts"].([]any)
	if !ok || len(entries) != 1 || node.Capture["schema"] != "kodex.workflow-capture.v1" ||
		node.Capture["runtimeRevisionId"] != execution.RevisionID || node.Capture["leaseId"] != execution.LeaseID ||
		node.Capture["generation"] != float64(execution.Generation) {
		t.Fatal("capture does not prove the completed immutable artifact batch")
	}
	pin, ok := entries[0].(map[string]any)
	result := entry.Results[0]
	body := []byte("Проверенный native материал: " + file + "\n")
	digest := sha256.Sum256(body)
	if !ok || result.Key != "report" || result.Kind != "ARTIFACT" || result.ArtifactRef != pin["ref"] ||
		result.ArtifactRevision != pin["revisionRef"] || result.Digest != hex.EncodeToString(digest[:]) ||
		"sha256:"+result.Digest != pin["digest"] || pin["fileName"] != file || pin["mediaType"] != "text/markdown" ||
		pin["sizeBytes"] != float64(len(body)) {
		t.Fatal("accepted workflow result substituted the declared immutable file")
	}
	return node
}
