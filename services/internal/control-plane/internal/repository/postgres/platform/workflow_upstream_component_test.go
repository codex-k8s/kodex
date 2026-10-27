package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowUpstreamProof observes private owner pins and floors without preparing business state.
// queryWorkflowUpstreamProof читает private owner pins/floors без подготовки business state.
//
//go:embed testdata/sql/workflow_upstream_proof.sql
var queryWorkflowUpstreamProof string

// queryWorkflowUpstreamForbiddenGate attempts a guarded negative transition only inside rollback.
// queryWorkflowUpstreamForbiddenGate пробует negative transition только внутри rollback.
//
//go:embed testdata/sql/workflow_upstream_forbidden_gate.sql
var queryWorkflowUpstreamForbiddenGate string

// TestWorkflowUpstreamReworkComponent proves actual author return, fresh dependent checks, immutable
// history and exact-once replay through public owner commands in isolated databases. No SQL writes
// construct an approval, result, invocation, lease, runtime revision or accepted dependency.
// TestWorkflowUpstreamReworkComponent доказывает возврат автора, новые dependent checks, immutable
// history и exact-once replay через native owner commands в isolated DB. SQL не создаёт approval,
// result, invocation, lease, runtime revision либо accepted dependency.
func TestWorkflowUpstreamReworkComponent(t *testing.T) {
	for _, scenario := range []string{"review-first", "sibling-gate-first", "parallel-rejections", "owner-request-changes", "cancel-after-approval", "revoked-author-before-approval", "reject-rework", "expire-rework"} {
		t.Run(scenario, func(t *testing.T) { testWorkflowUpstreamScenario(t, scenario) })
	}
}

// testWorkflowUpstreamScenario exercises both reviewer orderings and an explicit owner-request path.
// It obtains numeric artifact revisions from native owner reads; opaque refs never become numbers.
// testWorkflowUpstreamScenario проверяет оба порядка reviews и explicit owner-request path.
// Numeric artifact revisions получаются native owner read; opaque refs не становятся числами.
func testWorkflowUpstreamScenario(t *testing.T, scenario string) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated upstream database")
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
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "upstream-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native upstream command key=%s outcome=%s", key, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Typed upstream rework", Language: "ru"}).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "upstream-manager", "Менеджер разбора")
	agents := map[string]entity.Agent{}
	for _, key := range []string{"author", "r", "q", "independent"} {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "upstream-"+key, key)
		agents[key] = *execute(command.ChangeAgentCapability, "artifact-"+key, &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", ReworkPolicy: "OWNER_APPROVED_UPSTREAM_V1", Name: "Возврат автора и повтор проверок", Purpose: "Проверить точное происхождение результата и owner-approved rework.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 600, ResultSchema: map[string]any{}}
	for index, key := range []string{"author", "r", "q", "independent"} {
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agents[key].Ref, Instructions: "Проверить exact входы и вернуть закрытую декларацию.", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}}
		if key == "author" || key == "independent" {
			file := "spec.md"
			if key == "independent" {
				file = "independent.md"
				step.HumanGateAfter = true
				step.GateDecisions = []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}
			}
			step.ResultRequirements = []entity.WorkflowResultRequirement{{Key: "spec", Kind: "ARTIFACT", FileName: file, MediaType: "text/markdown"}}
		} else {
			step.DependsOn = []string{"author"}
			step.HumanGateAfter = true
			step.GateDecisions = []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}
			step.ResultRequirements = []entity.WorkflowResultRequirement{{Key: "review", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"}, {Key: "notes", Kind: "ARTIFACT", FileName: key + ".md", MediaType: "text/markdown"}}
		}
		if scenario == "expire-rework" && key == "r" {
			ttl := int32(1)
			step.ApprovalTimeoutSeconds = &ttl
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := execute(command.CreateWorkflow, "workflow", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Доработать файл по отказу reviewer и получить новые проверки.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	read := func() workflowCaptureProgressProof {
		t.Helper()
		var raw []byte
		if pool.QueryRow(ctx, queryWorkflowCaptureProgressProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref, "manager_ref": manager.Ref}).Scan(&raw) != nil {
			t.Fatal("read native upstream state")
		}
		var proof workflowCaptureProgressProof
		if json.Unmarshal(raw, &proof) != nil {
			t.Fatal("decode native upstream state")
		}
		return proof
	}
	history := func() ([]workflowExceptionEpisode, map[string]int32, map[string]string) {
		t.Helper()
		var episodes []workflowExceptionEpisode
		var floors map[string]int32
		var pins map[string]string
		if pool.QueryRow(ctx, queryWorkflowUpstreamProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref}).Scan(&episodes, &floors, &pins) != nil {
			t.Fatal("read native upstream pins")
		}
		return episodes, floors, pins
	}
	claim := func(key string) []map[string]any {
		return execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "workflow-upstream-fixture", Limit: 2}).RuntimeItems
	}
	completion := func(lease map[string]any, summary string, artifacts []command.CompletedArtifact) command.CompleteExecutionInput {
		return command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: summary, Artifacts: artifacts}
	}
	complete := func(key string, lease map[string]any, summary string, artifacts []command.CompletedArtifact) {
		execute(command.CompleteExecution, key, nil, completion(lease, summary, artifacts))
	}
	findGate := func(parentRef string) entity.OwnerGate {
		t.Helper()
		gates, _, _, err := service.ListOwnerGates(ctx, owner, query.Filter{ProjectRef: project.Ref, Page: query.Page{Size: 100}})
		if err != nil {
			t.Fatal("read owner gate list")
		}
		_, graph, err := service.GetRunGraphSnapshot(ctx, owner, run.Ref)
		if err != nil {
			t.Fatal("read owner graph before decision", completionOutcomeClass(err))
		}
		for _, gate := range gates {
			if gate.RunRef != run.Ref || gate.State != "OPEN" {
				continue
			}
			for _, node := range graph.Nodes {
				if node.Ref == gate.NodeRef && node.ParentNodeRef == parentRef {
					return gate
				}
			}
		}
		t.Fatal("exact producer gate not found")
		return entity.OwnerGate{}
	}
	initial := claim("author1")
	if len(initial) != 2 {
		t.Fatal("initial independent branches did not share the bounded frontier")
	}
	var author1, independent map[string]any
	for _, lease := range initial {
		switch stringMap(lease, "agentRef") {
		case agents["author"].Ref:
			author1 = lease
		case agents["independent"].Ref:
			independent = lease
		default:
			t.Fatal("dependent or manager admitted before author")
		}
	}
	if author1 == nil || independent == nil {
		t.Fatal("initial logical identities missing")
	}
	complete("independent-complete", independent, `{"schema":"kodex.workflow-result.v1","results":[{"key":"spec","source":{"kind":"CURRENT_COMPLETION"}}]}`, []command.CompletedArtifact{workflowCaptureProgressArtifact("independent.md", []byte("Независимый материал.\n"))})
	independentGate := findGate(stringMap(independent, "nodeRef"))
	body1 := []byte("Первый неизменяемый материал.\n")
	complete("author1-complete", author1, `{"schema":"kodex.workflow-result.v1","results":[{"key":"spec","source":{"kind":"CURRENT_COMPLETION"}}]}`, []command.CompletedArtifact{workflowCaptureProgressArtifact("spec.md", body1)})
	var authorAnchor workflowCaptureProgressNode
	for _, node := range read().Nodes {
		if node.StepKey == "author" && node.BusinessAttempt == 1 {
			authorAnchor = node
		}
	}
	if len(authorAnchor.History) != 1 || !authorAnchor.History[0].Accepted {
		t.Fatal("first author receipt missing")
	}
	firstReceipt := authorAnchor.History[0]
	firstResult := firstReceipt.Results[0]
	revision1, err := service.GetArtifactRevision(ctx, owner, firstResult.ArtifactRef, firstResult.ArtifactRevision)
	if err != nil {
		t.Fatal("read exact author revision")
	}
	reviewers := claim("reviewers1")
	if len(reviewers) != 2 {
		t.Fatal("independent reviewers were not admitted together")
	}
	leases := map[string]map[string]any{}
	for _, lease := range reviewers {
		for _, key := range []string{"r", "q"} {
			if stringMap(lease, "agentRef") == agents[key].Ref {
				leases[key] = lease
				assertWorkflowUpstreamFile(t, ctx, repository, service, lease, firstResult.ArtifactRef, "spec.md", revision1.Digest, revision1.Revision, body1)
			}
		}
	}
	if len(leases) != 2 {
		t.Fatal("reviewer identity mismatch")
	}
	reviewSummary := func(result entity.WorkflowAcceptedResult, revision int64, verdict string) string {
		raw, _ := json.Marshal(entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{{Key: "review", Source: &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: result.ArtifactRef, Revision: revision, Digest: "sha256:" + result.Digest}, Verdict: verdict}, {Key: "notes", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}}})
		return string(raw)
	}
	finishReview := func(key, verdict string) {
		complete(key+"1-complete", leases[key], reviewSummary(firstResult, revision1.Revision, verdict), []command.CompletedArtifact{workflowCaptureProgressArtifact(key+".md", []byte("Точные замечания reviewer "+key+".\n"))})
	}
	var oldGate entity.OwnerGate
	switch scenario {
	case "sibling-gate-first":
		finishReview("q", "PASS")
		oldGate = findGate(stringMap(leases["q"], "nodeRef"))
		finishReview("r", "CHANGES_REQUIRED")
	case "parallel-rejections":
		finishReview("r", "CHANGES_REQUIRED")
		finishReview("q", "CHANGES_REQUIRED")
	case "owner-request-changes":
		finishReview("r", "PASS")
		finishReview("q", "PASS")
		oldGate = findGate(stringMap(leases["q"], "nodeRef"))
		sourceGate := findGate(stringMap(leases["r"], "nodeRef"))
		execute(command.ResolveOwnerGate, "request-changes", &sourceGate.Version, command.GateResolutionInput{GateRef: sourceGate.Ref, Decision: "REQUEST_CHANGES", Comment: "Вернуть опубликованному автору; старое PASS не принимает новый материал."})
		stored, err := service.GetOwnerGate(ctx, owner, sourceGate.Ref)
		if err != nil || stored.State != "CHANGES_REQUESTED" || stored.ResolutionKind != "" {
			t.Fatal("owner REQUEST_CHANGES history reinterpreted")
		}
	default:
		finishReview("r", "CHANGES_REQUIRED")
		finishReview("q", "PASS")
		oldGate = findGate(stringMap(leases["q"], "nodeRef"))
	}
	episodes, _, _ := history()
	if len(episodes) != 1 {
		t.Fatal("review rejection created duplicate or absent owner episode")
	}
	managerClaims := claim("manager")
	if len(managerClaims) != 1 || stringMap(managerClaims[0], "agentRef") != manager.Ref {
		t.Fatal("review refusal did not admit the exact manager")
	}
	complete("manager-complete", managerClaims[0], "Автор должен исправить точный материал. Это анализ, не разрешение запуска.", nil)
	episodes, _, _ = history()
	if len(episodes) != 1 || episodes[0].Kind != "UPSTREAM_REWORK" || episodes[0].State != "WAITING_OWNER" {
		t.Fatal("typed review did not retain one closed upstream episode")
	}
	episode := episodes[0]
	gate, err := service.GetOwnerGate(ctx, owner, episode.GateRef)
	if err != nil || gate.ReworkPlan == nil || len(gate.ReworkPlan.Authors) != 1 || gate.ReworkPlan.Authors[0].StepKey != "author" || len(gate.ReworkPlan.Rechecks) != 2 {
		t.Fatal("owner preview did not name actual author and affected reviews")
	}
	if scenario == "reject-rework" {
		execute(command.ResolveOwnerGate, "reject-rework", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "REJECT", Comment: "Не разрешать новую попытку автора."})
		closed, err := service.GetOwnerGate(ctx, owner, gate.Ref)
		episodes, _, _ = history()
		if err != nil || closed.State != "REJECTED" || read().State != "FAILED" || episodes[0].State != "REJECTED" {
			t.Fatal("owner rejection did not close the full graph distinctly")
		}
		return
	}
	if scenario == "expire-rework" {
		if gate.ExpiresAt == nil {
			t.Fatal("configured recovery gate TTL missing")
		}
		wait := time.Until(*gate.ExpiresAt) + 50*time.Millisecond
		if wait > 2*time.Second {
			t.Fatal("unexpected native gate expiry clock")
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				t.Fatal("fixture expiry budget elapsed")
			case <-timer.C:
			}
		}
		if len(claim("expire-owner-gate")) != 0 {
			t.Fatal("expired gate admitted new model work")
		}
		closed, err := service.GetOwnerGate(ctx, owner, gate.Ref)
		episodes, _, _ = history()
		if err != nil || closed.State != "EXPIRED" || closed.Decision != "" || read().State != "FAILED" || episodes[0].State != "CANCELLED" {
			t.Fatal("system expiry fabricated owner rejection or left live work")
		}
		return
	}
	if scenario == "revoked-author-before-approval" {
		author, err := service.GetAgent(ctx, owner, agents["author"].Ref)
		if err != nil {
			t.Fatal("read exact author version before disable")
		}
		execute(command.SetAgentEnabled, "disable-author", &author.Version, command.AgentInput{Ref: author.Ref, Enabled: false})
		before := read()
		_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "upstream-denied-approve", ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"}})
		if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrConflict) && !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("revoked author admission did not fail closed")
		}
		after, readErr := service.GetOwnerGate(ctx, owner, gate.Ref)
		_, floors, _ := history()
		if readErr != nil || after.State != "OPEN" || after.Version != gate.Version || len(floors) != 0 || !reflect.DeepEqual(before, read()) {
			t.Fatal("denied approval partially changed gates, floors or attempts")
		}
		root, _, readErr := service.GetRunGraphSnapshot(ctx, owner, run.Ref)
		if readErr != nil {
			t.Fatal("read cancel version after denied approval")
		}
		execute(command.CancelRun, "cancel-denied-rework", &root.Version, command.RunCommandInput{RunRef: run.Ref})
		return
	}
	beforeApproval := read()
	if len(claim("before-approval")) != 0 || !reflect.DeepEqual(beforeApproval, read()) {
		t.Fatal("unapproved plan created work")
	}
	approval := command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Исправить исходный файл и повторить обе проверки."}
	execute(command.ResolveOwnerGate, "approve", &gate.Version, approval)
	afterApproval := read()
	episodes, floors, _ := history()
	if len(episodes) != 1 || episodes[0].State != "RECOVERED" || floors["author"] != 2 || floors["r"] != 2 || floors["q"] != 2 {
		t.Fatal("approved plan did not invalidate all old verdicts durably")
	}
	execute(command.ResolveOwnerGate, "approve", &gate.Version, approval)
	if !reflect.DeepEqual(afterApproval, read()) {
		t.Fatal("approval replay duplicated work or history")
	}
	if oldGate.Ref != "" {
		closed, err := service.GetOwnerGate(ctx, owner, oldGate.Ref)
		if err != nil || closed.State != "CANCELLED" || closed.ResolutionKind != "SUPERSEDED" || closed.Decision != "" || closed.Version != oldGate.Version+1 {
			t.Fatal("obsolete sibling gate was not distinctly superseded")
		}
	}
	// A neighboring OPEN result gate is not superseded by another author's approved plan.
	neighbor, err := service.GetOwnerGate(ctx, owner, independentGate.Ref)
	if err != nil || neighbor.State != "OPEN" || neighbor.Version != independentGate.Version || neighbor.ResolutionKind != "" {
		t.Fatal("neighbor gate was closed by an unrelated plan")
	}
	for _, forbidden := range []string{independentGate.Ref, gate.Ref} {
		before := read()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin rollback-only supersession probe")
		}
		_, cause := tx.Exec(ctx, queryWorkflowUpstreamForbiddenGate, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref, "gate_ref": forbidden})
		var pgError *pgconn.PgError
		closed := errors.As(cause, &pgError) && pgError.Code == "23514"
		rollbackWorkflowJournalProbe(t, ctx, tx)
		if !closed || !reflect.DeepEqual(before, read()) {
			t.Fatal("neighbor or terminal history accepted forbidden supersession")
		}
	}
	next := claim("author2")
	if len(next) != 1 || stringMap(next[0], "agentRef") != agents["author"].Ref || stringMap(next[0], "runRef") == stringMap(author1, "runRef") {
		t.Fatal("approval retried reviewer instead of creating a fresh author attempt")
	}
	author2 := next[0]
	_, _, pins := history()
	if pins[stringMap(author2, "nodeRef")] != gate.Ref {
		t.Fatal("fresh author lost exact approved gate pin")
	}
	// The author receives exact rejected-review notes as context, not as a new accepted input.
	var reviewNotes map[string]any
	for _, node := range read().Nodes {
		if node.Ref != stringMap(leases["r"], "nodeRef") {
			continue
		}
		items, _ := node.Capture["artifacts"].([]any)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if ok && stringMap(item, "fileName") == "r.md" {
				if reviewNotes != nil {
					t.Fatal("ambiguous reviewer notes capture")
				}
				reviewNotes = item
			}
		}
	}
	if reviewNotes == nil {
		t.Fatal("exact rejected reviewer capture missing")
	}
	assertWorkflowUpstreamFile(t, ctx, repository, service, author2, stringMap(reviewNotes, "ref"), "r.md", stringMap(reviewNotes, "digest"), runtimeRevisionMapInt64(reviewNotes, "revision"), []byte("Точные замечания reviewer r.\n"))
	if scenario == "cancel-after-approval" {
		root, _, err := service.GetRunGraphSnapshot(ctx, owner, run.Ref)
		if err != nil {
			t.Fatal("read before owner cancel")
		}
		execute(command.CancelRun, "cancel", &root.Version, command.RunCommandInput{RunRef: run.Ref})
		_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "upstream-late-after-cancel"}, Payload: completion(author2, `{"schema":"kodex.workflow-result.v1","results":[]}`, nil)})
		if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrConflict) {
			t.Fatal("cancel did not fence late completion")
		}
		if read().State != "CANCELLED" {
			t.Fatal("cancel reopened the workflow")
		}
		return
	}
	// A real technical continuation must inherit the same approval pin without consuming attempt3.
	complete("author2-progress", author2, `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["spec"]}`, []command.CompletedArtifact{workflowCaptureProgressArtifact("spec.md", []byte("Ранняя доработка.\n"))})
	continued := claim("author2-technical")
	if len(continued) != 1 || stringMap(continued[0], "runRef") != stringMap(author2, "runRef") {
		t.Fatal("technical continuation changed business run")
	}
	author2 = continued[0]
	_, _, pins = history()
	if pins[stringMap(author2, "nodeRef")] != gate.Ref {
		t.Fatal("technical continuation lost immutable rework gate")
	}
	body2 := []byte("Новый материал после замечаний.\n")
	complete("author2-complete", author2, `{"schema":"kodex.workflow-result.v1","results":[{"key":"spec","source":{"kind":"CURRENT_COMPLETION"}}]}`, []command.CompletedArtifact{workflowCaptureProgressArtifact("spec.md", body2)})
	proof := read()
	if proof.State == "SUCCEEDED" || proof.Managers != 1 {
		t.Fatal("old PASS completed new author work or re-opened covered sibling refusal")
	}
	var newAuthor workflowCaptureProgressNode
	for _, node := range proof.Nodes {
		if node.StepKey == "author" && node.OriginNodeID == authorAnchor.ID && len(node.History) == 2 {
			newAuthor = node
		}
	}
	if len(newAuthor.History) != 2 || !reflect.DeepEqual(newAuthor.History[0], firstReceipt) || !newAuthor.History[1].Accepted {
		t.Fatal("author history changed or second result missing")
	}
	secondResult := newAuthor.History[1].Results[0]
	revision2, err := service.GetArtifactRevision(ctx, owner, secondResult.ArtifactRef, secondResult.ArtifactRevision)
	if err != nil {
		t.Fatal("read second exact revision")
	}
	fresh := claim("reviewers2")
	if len(fresh) != 2 {
		t.Fatal("dependent checks were not physically rerun")
	}
	for index, lease := range fresh {
		key := ""
		for _, candidate := range []string{"r", "q"} {
			if stringMap(lease, "agentRef") == agents[candidate].Ref {
				key = candidate
			}
		}
		if key == "" || stringMap(lease, "runRef") == stringMap(leases[key], "runRef") {
			t.Fatal("reviewer reused old physical attempt")
		}
		assertWorkflowUpstreamFile(t, ctx, repository, service, lease, secondResult.ArtifactRef, "spec.md", revision2.Digest, revision2.Revision, body2)
		complete(fmt.Sprintf("review2-%d", index), lease, reviewSummary(secondResult, revision2.Revision, "PASS"), []command.CompletedArtifact{workflowCaptureProgressArtifact(key+".md", []byte("Повторная проверка нового материала.\n"))})
		newGate := findGate(stringMap(lease, "nodeRef"))
		execute(command.ResolveOwnerGate, fmt.Sprintf("final-approve-%d", index), &newGate.Version, command.GateResolutionInput{GateRef: newGate.Ref, Decision: "APPROVE"})
	}
	if read().State != "WAITING_HUMAN" {
		t.Fatal("unrelated unresolved owner gate was bypassed at root completion")
	}
	untouched, err := service.GetOwnerGate(ctx, owner, independentGate.Ref)
	if err != nil || untouched.State != "OPEN" || untouched.ResolutionKind != "" {
		t.Fatal("unrelated gate history changed")
	}
	execute(command.ResolveOwnerGate, "independent-approve", &untouched.Version, command.GateResolutionInput{GateRef: untouched.Ref, Decision: "APPROVE"})
	final := read()
	if final.State != "SUCCEEDED" || final.Managers != 1 || final.ClaimedLeases != 0 {
		t.Fatal("fresh author and reviews did not complete the exact workflow")
	}
	graph := assertWorkflowRetryOwnerSnapshot(t, ctx, service, owner, run.Ref, 3)
	for _, node := range graph.Nodes {
		for _, receipt := range node.WorkflowResults {
			if receipt.Provenance != nil || receipt.RuntimeRevisionID != "" || len(receipt.ReworkSubjects) > 0 {
				t.Fatal("public projection leaked private proof")
			}
			if receipt.Attempt == 1 && node.AgentRef != agents["independent"].Ref && receipt.EffectiveState != "HISTORICAL" {
				t.Fatal("old PASS still displayed as current")
			}
		}
	}
	old, err := service.GetArtifactRevision(ctx, owner, firstResult.ArtifactRef, firstResult.ArtifactRevision)
	if err != nil || old.Digest != revision1.Digest {
		t.Fatal("old author artifact was deleted or rewritten")
	}
	for _, lease := range fresh {
		if strings.Contains(stringMap(lease, "task"), "originNodeId") {
			t.Fatal("review prompt exposed private node authority")
		}
	}
}

// assertWorkflowUpstreamFile resolves one exact descriptor among bounded current/rework-context
// pins, then reads exact native metadata and bytes. Additional context never permits substitution.
// assertWorkflowUpstreamFile находит exact descriptor среди bounded current/rework-context pins,
// затем читает native metadata/bytes. Дополнительный context не разрешает подмену ревизии.
func assertWorkflowUpstreamFile(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, lease map[string]any, ref, name, digest string, revision int64, expected []byte) {
	t.Helper()
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
	page, err := service.GetExecutionFileManifest(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "manifest"), execution, query.Page{Size: 100})
	if err != nil || len(page.Items) < 1 || len(page.Items) > 100 || page.Total != int64(len(page.Items)) || page.Next != "" || page.Catalog.Ref != execution.CatalogRef || page.Catalog.Digest != execution.CatalogDigest {
		t.Fatal("upstream exact catalog unavailable")
	}
	var file entity.ExecutionFileDescriptor
	matches := 0
	for _, candidate := range page.Items {
		if candidate.ArtifactRef == ref && candidate.Revision == revision && candidate.Digest == digest {
			file = candidate
			matches++
		}
	}
	if matches != 1 || file.Name != name || file.SizeBytes != int64(len(expected)) || file.Source != "AGENT_RESULT" {
		t.Fatal("upstream catalog substituted exact artifact pins")
	}
	exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
	if _, err := service.GetExecutionFileMetadata(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "metadata"), execution, exact); err != nil {
		t.Fatal("upstream exact metadata denied")
	}
	reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	download, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, ref)
	if err != nil {
		t.Fatal("upstream exact bytes denied")
	}
	body, readErr := io.ReadAll(io.LimitReader(download.Reader, int64(len(expected))+1))
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(body, expected) {
		t.Fatal("upstream bytes changed")
	}
}
