package platform

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workflowEvidenceNativeFixture keeps a real owner-command graph and native integration worker
// boundary. Only credential metadata uses the established disposable synthetic fixture exception.
// workflowEvidenceNativeFixture хранит actual owner-command graph и native integration worker
// boundary. Только credential metadata использует established disposable synthetic exception.
type workflowEvidenceNativeFixture struct {
	f          *workflowRecoveryRegression
	connection entity.IntegrationConnection
	gateway    value.Principal
	serial     int
}

// TestWorkflowReviewQAEvidenceComponent proves capture, owner receipt admission, both completion
// orders, negative history and replay. Trusted IG completion supplies provider-shaped facts; this
// suite does not claim external GitHub delivery or automatic execution attestation of QA commands.
// TestWorkflowReviewQAEvidenceComponent доказывает capture, owner receipt admission, оба порядка
// completion, negative history и replay. Trusted IG completion задаёт provider-shaped facts;
// оснастка не заявляет внешний GitHub delivery либо автоматическую аттестацию QA commands.
func TestWorkflowReviewQAEvidenceComponent(t *testing.T) {
	for _, mode := range []string{"mixed-valid", "mixed-missing-comment", "mixed-changed-revision", "mixed-changed-digest", "review-effect-first", "review-model-first", "review-unknown", "review-cancel", "review-revoked", "qa-pass", "qa-changes-no-issue", "qa-issue-effect-first", "qa-issue-model-first", "qa-issue-retention-known-cancel", "qa-issue-retention-unknown-cancel", "qa-issue-retention-unknown-model-first", "qa-issue-retention-unknown-revoke", "qa-issue-retention-unknown-expiry", "qa-issue-retention-head-deleted", "qa-issue-retention-head-lock"} {
		t.Run(mode, func(t *testing.T) { runWorkflowEvidenceNative(t, mode) })
	}
}

// key assigns deterministic unique native mutation keys within one isolated graph.
// key назначает deterministic unique native mutation keys внутри isolated graph.
func (fixture *workflowEvidenceNativeFixture) key(prefix string) string {
	fixture.serial++
	return prefix + "-" + leftPad(fixture.serial, 3)
}

// claim requires the exact published employee and a fresh actual execution, never a fixture row.
// claim требует exact published employee и fresh actual execution, не fixture row.
func (fixture *workflowEvidenceNativeFixture) claim(agent string) map[string]any {
	fixture.f.t.Helper()
	previous := slog.Default()
	trace := &workflowEvidenceClaimDiagnostics{}
	slog.SetDefault(slog.New(trace))
	defer slog.SetDefault(previous)
	items := fixture.f.claims(fixture.key("claim"))
	if len(items) != 1 || stringMap(items[0], "agentRef") != agent {
		actual := "NONE"
		if len(items) > 0 && workflowLaunchRefPattern.MatchString(stringMap(items[0], "agentRef")) {
			actual = stringMap(items[0], "agentRef")
		}
		fixture.f.t.Fatalf("exact evidence employee was not claimable: count=%d expected=%s actual=%s diagnostic=%s", len(items), agent, actual, trace.summary())
	}
	return items[0]
}

// complete replays independently decoded original bytes and compares authoritative owner graph;
// mutation of prepared artifact buffers cannot silently change the replay's intent.
// complete повторяет независимо decoded original bytes и сравнивает authoritative owner graph;
// mutation prepared artifact buffers не меняет replay intent молча.
func (fixture *workflowEvidenceNativeFixture) complete(lease map[string]any, summary string, artifacts ...command.CompletedArtifact) command.Result {
	f := fixture.f
	f.t.Helper()
	payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: summary, Artifacts: artifacts}
	intent, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal("encode immutable evidence completion")
	}
	fresh := func() command.CompleteExecutionInput {
		var value command.CompleteExecutionInput
		if json.Unmarshal(intent, &value) != nil {
			f.t.Fatal("decode immutable evidence completion")
		}
		return value
	}
	key := fixture.key("complete")
	result := f.execute(command.CompleteExecution, key, nil, fresh())
	_, before, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
	if err != nil {
		f.t.Fatal("read evidence graph after completion")
	}
	replay := f.execute(command.CompleteExecution, key, nil, fresh())
	_, after, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
	if err != nil || !reflect.DeepEqual(result.CreatedRefs, replay.CreatedRefs) || !reflect.DeepEqual(before, after) {
		f.t.Fatal("completion replay changed evidence graph")
	}
	return result
}

// resolve follows the existing runtime-controller invocation path with server-owned lease context.
// resolve использует existing runtime-controller invocation path с server-owned lease context.
func (fixture *workflowEvidenceNativeFixture) resolve(lease map[string]any, key, operation string, input map[string]any) (map[string]any, error) {
	return fixture.f.service.ResolveIntegrationInvocation(fixture.f.ctx, fixture.f.worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": fixture.connection.Ref, "capability_key": operation, "idempotency_key": key}, input)
}

// finishEffect approves only an existing effect gate if needed, claims the exact native invocation
// and stores one trusted provider-shaped receipt; it performs no external HTTP request.
// finishEffect согласует только existing effect Gate при необходимости, claim-ит exact native
// invocation и сохраняет trusted provider-shaped receipt без внешнего HTTP request.
func (fixture *workflowEvidenceNativeFixture) finishEffect(invocation map[string]any, provider, summary string) string {
	f := fixture.f
	f.t.Helper()
	if gateRef := stringMap(invocation, "gateRef"); gateRef != "" {
		gate, err := f.service.GetOwnerGate(f.ctx, f.owner, gateRef)
		if err != nil {
			f.t.Fatal("read exact native effect gate")
		}
		f.execute(command.ResolveOwnerGate, fixture.key("approve-effect"), &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Only the exact native fixture effect."})
	}
	work := []map[string]any{fixture.claimEffect(invocation)}
	var err error
	_, err = f.service.Execute(f.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: fixture.gateway, Mutation: value.Mutation{IdempotencyKey: fixture.key("native-complete")}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: provider, ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}})
	if err != nil {
		f.t.Fatal("persist native evidence receipt", completionOutcomeClass(err))
	}
	state, err := f.service.GetIntegrationInvocation(f.ctx, f.worker, stringMap(invocation, "invocationRef"))
	if err != nil || stringMap(state, "state") != "SUCCEEDED" || stringMap(state, "effectReceiptRef") == "" {
		f.t.Fatal("native evidence receipt missing")
	}
	return stringMap(state, "effectReceiptRef")
}

// claimEffect obtains the exact actual worker lease, never synthesizing ledger state.
// claimEffect получает exact actual worker lease без синтеза ledger state.
func (fixture *workflowEvidenceNativeFixture) claimEffect(invocation map[string]any) map[string]any {
	f := fixture.f
	f.t.Helper()
	work, err := f.service.ClaimIntegrationInvocations(f.ctx, fixture.gateway, "workflow-evidence-native", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") {
		f.t.Fatal("exact evidence effect not claimable", completionOutcomeClass(err))
	}
	return work[0]
}

// stopPendingReview proves that unknown, cancellation and current grant revocation cannot
// allocate another WRITE or revive a completed model producer; only owner cancellation closes it.
// stopPendingReview доказывает, что UNKNOWN, отмена и отзыв current grant не создают новый WRITE
// либо не оживляют completed model producer; завершение выполняет штатная owner cancellation.
func (fixture *workflowEvidenceNativeFixture) stopPendingReview(mode string, lease, invocation map[string]any) {
	f := fixture.f
	f.t.Helper()
	var work map[string]any
	if mode != "review-revoked" {
		work = fixture.claimEffect(invocation)
	}
	fixture.complete(lease, "Native COMMENT outcome is unresolved; do not fabricate a receipt.")
	if len(f.read().journals["check"]) != 0 {
		f.t.Fatal("pending outcome produced business acceptance")
	}
	if mode == "review-unknown" {
		_, err := f.service.Execute(f.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: fixture.gateway, Mutation: value.Mutation{IdempotencyKey: fixture.key("unknown")}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), UnknownOutcome: true, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN"}})
		if err != nil {
			f.t.Fatal("persist unknown evidence outcome", completionOutcomeClass(err))
		}
	}
	if mode == "review-revoked" {
		connection, err := f.service.GetIntegrationConnection(f.ctx, f.owner, fixture.connection.Ref)
		if err != nil {
			f.t.Fatal("read evidence connection before revoke")
		}
		f.execute(command.ChangeIntegrationGrant, fixture.key("revoke"), &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: f.agents["check"], CapabilityKey: "github.pull_request.review.create", ApprovalPolicy: "NONE", Enabled: false})
	}
	if mode != "review-cancel" {
		if items, err := f.service.ClaimIntegrationInvocations(f.ctx, fixture.gateway, "evidence-denied-reclaim", 1); err != nil || len(items) != 0 {
			f.t.Fatal("unknown or revoked evidence WRITE was reissued")
		}
	}
	if items := f.claims(fixture.key("no-model-join")); len(items) != 0 {
		f.t.Fatal("pending evidence created a fresh model turn")
	}
	current, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil {
		f.t.Fatal("read pending evidence root")
	}
	f.execute(command.CancelRun, fixture.key("cancel-pending"), &current.Version, command.RunCommandInput{RunRef: current.Ref, Reason: "Close unresolved native fixture without replaying external WRITE"})
	state, err := f.service.GetIntegrationInvocation(f.ctx, f.worker, stringMap(invocation, "invocationRef"))
	want := "UNKNOWN_OUTCOME"
	if mode == "review-revoked" {
		want = "CANCELLED"
	}
	if err != nil || stringMap(state, "state") != want || stringMap(state, "effectReceiptRef") != "" {
		f.t.Fatal("cancellation lost unknown outcome or fabricated a receipt")
	}
	if work != nil {
		_, err = f.service.Execute(f.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: fixture.gateway, Mutation: value.Mutation{IdempotencyKey: fixture.key("late-completion")}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}})
		if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
			f.t.Fatal("late evidence completion resurrected cancelled graph", completionOutcomeClass(err))
		}
	}
	if items, err := f.service.ClaimIntegrationInvocations(f.ctx, fixture.gateway, "evidence-after-cancel", 1); err != nil || len(items) != 0 {
		f.t.Fatal("cancelled evidence graph reissued WRITE")
	}
}

// captured reads an actual owner artifact created by completion, retaining immutable revision pins.
// captured читает actual owner artifact, созданный completion, сохраняя immutable revision pins.
func (fixture *workflowEvidenceNativeFixture) captured(result command.Result) entity.Artifact {
	fixture.f.t.Helper()
	if len(result.CreatedRefs) != 1 {
		fixture.f.t.Fatal("capture did not create exactly one artifact")
	}
	artifact, err := fixture.f.service.GetArtifact(fixture.f.ctx, fixture.f.owner, result.CreatedRefs[0])
	if err != nil || artifact.ScanState != "CLEAN" || artifact.Revision != 1 {
		fixture.f.t.Fatal("captured CLEAN artifact unavailable")
	}
	return artifact
}

// evidenceCapturedSource selects one exact previously captured file without asserting authority.
// evidenceCapturedSource выбирает exact previously captured file без утверждения authority.
func evidenceCapturedSource(artifact entity.Artifact) *entity.WorkflowArtifactSource {
	return &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: artifact.Ref, Revision: artifact.Revision, Digest: artifact.Digest}
}

// evidenceDeclaration encodes only the approved runtime declaration, not published requirement fields.
// evidenceDeclaration кодирует только approved runtime declaration, не published requirement fields.
func evidenceDeclaration(results ...entity.WorkflowDeclaredResult) string {
	return string(asJSON(entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: results}))
}

// runWorkflowEvidenceNative composes a real accepted PR predecessor with the opt-in REVIEW or QA
// employee. No SQL inserts business states, accepted inputs, captures, receipts, leases or gates.
// runWorkflowEvidenceNative связывает actual accepted PR predecessor с opt-in REVIEW/QA employee.
// SQL не создаёт business states, accepted inputs, captures, receipts, leases либо Gates.
func runWorkflowEvidenceNative(t *testing.T, mode string) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated evidence database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	fixture := &workflowEvidenceNativeFixture{f: f}
	project := f.execute(command.CreateProject, "evidence-project", nil, command.ProjectInput{Name: "Native review QA evidence", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "evidence-manager", "Менеджер")
	author := createLifecycleAgent(t, ctx, service, owner, project.Ref, "evidence-author", "Разработчик")
	mixed := strings.HasPrefix(mode, "mixed-")
	if mixed {
		author = *f.execute(command.ChangeAgentCapability, "author-artifact", &author.Version, command.AgentBindingInput{AgentRef: author.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	}
	reviewer := createLifecycleAgent(t, ctx, service, owner, project.Ref, "evidence-reviewer", "Независимая проверка")
	reviewer = *f.execute(command.ChangeAgentCapability, "evidence-artifact", &reviewer.Version, command.AgentBindingInput{AgentRef: reviewer.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	f.managerRef, f.agents["author"], f.agents["check"] = manager.Ref, author.Ref, reviewer.Ref
	fixture.connection = *f.execute(command.CreateConnection, "evidence-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Evidence fixture", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, fixture.connection.Ref); err != nil {
		t.Fatal("seed established synthetic credential metadata")
	}
	fixture.connection, err = service.GetIntegrationConnection(ctx, owner, fixture.connection.Ref)
	if err != nil {
		t.Fatal("read current evidence connection")
	}
	for _, binding := range []struct{ agent, operation, policy string }{{author.Ref, "github.pull_request.create", "HUMAN_EACH_EFFECT"}, {reviewer.Ref, "github.pull_request.review.create", "NONE"}, {reviewer.Ref, "github.issue.create", "HUMAN_EACH_EFFECT"}} {
		fixture.connection = *f.execute(command.ChangeIntegrationGrant, fixture.key("grant"), &fixture.connection.Version, command.IntegrationGrantInput{ConnectionRef: fixture.connection.Ref, AgentRef: binding.agent, CapabilityKey: binding.operation, ApprovalPolicy: binding.policy, Enabled: true}).Connection
	}
	fixture.gateway = resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	isQA := strings.HasPrefix(mode, "qa-")
	modelFirst := strings.Contains(mode, "model-first")
	retention := strings.Contains(mode, "retention")
	issue := strings.Contains(mode, "issue")
	check := entity.WorkflowStep{Key: "check", Position: 2, Name: "Проверка", Instructions: "Проверить exact PR/head; сохранить фактические результаты.", ExpectedResult: "Проверяемые доказательства", AgentRef: reviewer.Ref, TimeoutSeconds: 180, DependsOn: []string{"author"}, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability, "github.pull_request.review.create", "github.issue.create"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "author"}}}
	if isQA {
		check.ResultRequirements = []entity.WorkflowResultRequirement{{Key: "log", Kind: "ARTIFACT", FileName: "test-output.txt", MediaType: "text/markdown"}, {Key: "report", Kind: "ARTIFACT", FileName: "qa-report.json", MediaType: "application/json"}, {Key: "qa", Kind: "QA", SubjectStepKey: "author", EvidenceKey: "report"}}
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", ResultEvidenceProfile: workflowservice.GitHubReviewQAV1, Name: "Native evidence", Purpose: "Проверить owner pins, не external delivery.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "author", Position: 1, Name: "PR", Instructions: "Создать точный PR.", AgentRef: author.Ref, TimeoutSeconds: 120, RequiredCapabilityKeys: []string{"github.pull_request.create"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "fixture/repository"}}}, check}}
	if mixed {
		draft.ReworkPolicy = workflowservice.OwnerApprovedUpstreamV1
		draft.Steps[0].RequiredCapabilityKeys = append(draft.Steps[0].RequiredCapabilityKeys, runtimecontract.ArtifactCapability)
		draft.Steps[0].ResultRequirements = append(draft.Steps[0].ResultRequirements, entity.WorkflowResultRequirement{Key: "spec", Kind: "ARTIFACT", FileName: "spec.md", MediaType: "text/markdown"})
		draft.Steps[1].ResultRequirements = append([]entity.WorkflowResultRequirement{{Key: "design", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"}}, draft.Steps[1].ResultRequirements...)
	}
	if strings.HasSuffix(mode, "unknown-expiry") {
		draft.TimeoutSeconds, draft.Steps[0].TimeoutSeconds, draft.Steps[1].TimeoutSeconds = 45, 40, 40
	}
	if retention {
		draft.Steps[1].ResultRequirements = append(draft.Steps[1].ResultRequirements, entity.WorkflowResultRequirement{Key: "unrelated", Kind: "ARTIFACT", FileName: "unrelated-output.txt", MediaType: "text/markdown"})
	}
	workflow := f.execute(command.CreateWorkflow, "evidence-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "evidence-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "evidence-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := f.execute(command.LaunchRun, "evidence-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Проверить native REVIEW/QA evidence.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	f.runRef = run.Ref
	authorLease := fixture.claim(author.Ref)
	pr, err := fixture.resolve(authorLease, "evidence-pr", "github.pull_request.create", map[string]any{"title": "Exact evidence PR", "body": "Native fixture, not external delivery", "head": "qa-evidence", "base": "main", "draft": false})
	if err != nil {
		t.Fatal("admit exact author PR", completionOutcomeClass(err))
	}
	sha := strings.Repeat("a", 40)
	prReceipt := fixture.finishEffect(pr, "github-pull:7", `{"number":7,"state":"open","sha":"`+sha+`","draft":false}`)
	var subjectArtifact entity.Artifact
	if mixed {
		completed := fixture.complete(authorLease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "pr", ReceiptRef: prReceipt}, entity.WorkflowDeclaredResult{Key: "spec", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}), workflowCaptureProgressArtifact("spec.md", []byte("Owner captured source requiring design changes.")))
		subjectArtifact = fixture.captured(completed)
	} else {
		fixture.complete(authorLease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "pr", ReceiptRef: prReceipt}))
	}
	lease := fixture.claim(reviewer.Ref)
	if !strings.Contains(stringMap(lease, "task"), "GITHUB_REVIEW_QA_V1") {
		t.Fatal("evidence profile guidance missing from actual task")
	}
	if !isQA {
		input := map[string]any{"pull_request_number": 7, "sha": sha, "event": "COMMENT", "body": "Native independent COMMENT"}
		for _, event := range []string{"APPROVE", "REQUEST_CHANGES"} {
			bad := map[string]any{"pull_request_number": 7, "sha": sha, "event": event, "body": "Denied event"}
			if _, err := fixture.resolve(lease, fixture.key("deny-review"), "github.pull_request.review.create", bad); err == nil {
				t.Fatal("self approval event admitted")
			}
		}
		invocation, err := fixture.resolve(lease, "evidence-review", "github.pull_request.review.create", input)
		if err != nil {
			t.Fatal("admit COMMENT", completionOutcomeClass(err))
		}
		replay, err := fixture.resolve(lease, "evidence-review", "github.pull_request.review.create", input)
		if err != nil || stringMap(replay, "invocationRef") != stringMap(invocation, "invocationRef") {
			t.Fatal("exact COMMENT replay allocated another invocation")
		}
		for _, changed := range []map[string]any{
			{"pull_request_number": 8, "sha": sha, "event": "COMMENT", "body": "Native independent COMMENT"},
			{"pull_request_number": 7, "sha": strings.Repeat("b", 40), "event": "COMMENT", "body": "Native independent COMMENT"},
			{"pull_request_number": 7, "sha": sha, "event": "COMMENT", "body": "Changed semantic COMMENT"},
		} {
			if _, err := fixture.resolve(lease, fixture.key("semantic-drift"), "github.pull_request.review.create", changed); err == nil {
				t.Fatal("COMMENT semantic drift admitted")
			}
		}
		if mode == "review-unknown" || mode == "review-cancel" || mode == "review-revoked" {
			fixture.stopPendingReview(mode, lease, invocation)
			return
		}
		if modelFirst {
			fixture.complete(lease, "The COMMENT is still pending; this is not a final evidence claim.")
			if len(f.read().journals["check"]) != 0 {
				t.Fatal("pending COMMENT accepted early")
			}
		}
		receipt := fixture.finishEffect(invocation, "github-review:91", `{"id":91,"body":"Native independent COMMENT","state":"COMMENTED","commit_id":"`+sha+`"}`)
		if modelFirst {
			lease = fixture.claim(reviewer.Ref)
			rejoined, err := fixture.resolve(lease, "evidence-review-rejoin", "github.pull_request.review.create", input)
			if err != nil || stringMap(rejoined, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(rejoined, "receiptRef") != receipt {
				t.Fatal("fresh COMMENT continuation repeated or lost native fact", completionOutcomeClass(err))
			}
		}
		if mixed {
			source := evidenceCapturedSource(subjectArtifact)
			if mode == "mixed-changed-revision" {
				source.Revision++
			}
			if mode == "mixed-changed-digest" {
				source.Digest = "sha256:" + strings.Repeat("b", 64)
			}
			if mode == "mixed-missing-comment" {
				receipt = ""
			}
			fixture.complete(lease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "design", Source: source, Verdict: "CHANGES_REQUIRED"}, entity.WorkflowDeclaredResult{Key: "review", HeadSHA: sha, Verdict: "PASS", ReceiptRef: receipt}))
		} else {
			fixture.complete(lease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "review", HeadSHA: sha, Verdict: "PASS", ReceiptRef: receipt}))
		}
	} else {
		logProgress := `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["log"]}`
		logFiles := []command.CompletedArtifact{workflowCaptureProgressArtifact("test-output.txt", []byte("Fixture-reported command output; no real business tests are claimed.\n"))}
		if retention {
			logProgress = `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["log","unrelated"]}`
			logFiles = append(logFiles, workflowCaptureProgressArtifact("unrelated-output.txt", []byte("Captured by the same employee, absent from the QA report log selectors.")))
		}
		logResult := fixture.complete(lease, logProgress, logFiles...)
		var log, unrelated entity.Artifact
		if retention {
			if len(logResult.CreatedRefs) != 2 {
				t.Fatal("retention capture did not create exact files")
			}
			for _, ref := range logResult.CreatedRefs {
				file, err := service.GetArtifact(ctx, owner, ref)
				if err != nil || file.ScanState != "CLEAN" {
					t.Fatal("retention capture metadata unavailable")
				}
				if file.FileName == "test-output.txt" {
					log = file
				} else if file.FileName == "unrelated-output.txt" {
					unrelated = file
				} else {
					t.Fatal("unexpected retention capture")
				}
			}
			if log.Ref == "" || unrelated.Ref == "" {
				t.Fatal("retention captures incomplete")
			}
		} else {
			log = fixture.captured(logResult)
		}
		if history := f.read().journals["check"]; len(history) != 0 {
			t.Fatalf("capture progress unexpectedly appended business history: count=%d accepted=%t", len(history), history[len(history)-1].Accepted)
		}
		lease = fixture.claim(reviewer.Ref)
		report := workflowservice.QAReport{Schema: "kodex.workflow-qa-report.v1", Repository: "fixture/repository", Number: 7, HeadSHA: sha, Verdict: "PASS", Tests: []workflowservice.QATest{{Name: "Native report fixture", Command: "fixture-output-only", ExitCode: 0, Log: evidenceCapturedSource(log)}}, Findings: []workflowservice.QAFinding{}}
		negativeQA := issue || mode == "qa-changes-no-issue"
		if negativeQA {
			report.Verdict = "CHANGES_REQUIRED"
			report.Findings = []workflowservice.QAFinding{{Key: "fixture-defect", Summary: "Isolated native fixture finding; not a real external bug."}}
		}
		if issue {
			report.Issue = &workflowservice.QAIssue{Title: "Native fixture finding", Body: "Isolated evidence publication, no external request."}
		}
		body := asJSON(report)
		artifact := workflowCaptureProgressArtifact("qa-report.json", body)
		artifact.MediaType = "application/json"
		if !issue {
			fixture.complete(lease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "log", Source: evidenceCapturedSource(log)}, entity.WorkflowDeclaredResult{Key: "report", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}, entity.WorkflowDeclaredResult{Key: "qa", HeadSHA: sha, Verdict: report.Verdict}), artifact)
		} else {
			if _, err := fixture.resolve(lease, "evidence-too-early-issue", "github.issue.create", map[string]any{"title": report.Issue.Title, "body": report.Issue.Body}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("Issue admitted before report capture", completionOutcomeClass(err))
			}
			reportResult := fixture.complete(lease, `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["report"]}`, artifact)
			captured := fixture.captured(reportResult)
			lease = fixture.claim(reviewer.Ref)
			input := map[string]any{"title": report.Issue.Title, "body": report.Issue.Body}
			if retention {
				fixture.checkIssueRetention(mode, lease, captured, log, unrelated, input)
				return
			}
			invocation, err := fixture.resolve(lease, "evidence-issue", "github.issue.create", input)
			if err != nil {
				t.Fatal("admit captured finding Issue", completionOutcomeClass(err))
			}
			if modelFirst {
				fixture.complete(lease, "The Issue is still pending; no fabricated receipt.")
				if len(f.read().journals["check"]) != 0 {
					t.Fatal("pending Issue accepted early")
				}
			}
			receipt := fixture.finishEffect(invocation, "github-issue:17", `{"number":17,"title":"Native fixture finding","state":"open"}`)
			if modelFirst {
				lease = fixture.claim(reviewer.Ref)
				rejoined, err := fixture.resolve(lease, "evidence-issue-rejoin", "github.issue.create", input)
				if err != nil || stringMap(rejoined, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(rejoined, "receiptRef") != receipt {
					t.Fatal("fresh Issue continuation repeated or lost native fact", completionOutcomeClass(err))
				}
			}
			fixture.complete(lease, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "log", Source: evidenceCapturedSource(log)}, entity.WorkflowDeclaredResult{Key: "report", Source: evidenceCapturedSource(captured)}, entity.WorkflowDeclaredResult{Key: "qa", HeadSHA: sha, Verdict: "CHANGES_REQUIRED", ReceiptRef: receipt}))
		}
	}
	current, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal("read final evidence owner graph")
	}
	found := false
	for _, node := range graph.Nodes {
		for _, attempt := range node.WorkflowResults {
			if attempt.RunRef == stringMap(lease, "runRef") {
				found = true
				if mixed {
					expected := workflowChangesRequired
					facts := 2
					if mode == "mixed-missing-comment" {
						expected = workflowDeclarationInvalid
						facts = 0
					}
					if mode == "mixed-changed-revision" || mode == "mixed-changed-digest" {
						expected = workflowReviewSubjectMismatch
						facts = 0
					}
					if attempt.Accepted || attempt.Reason != expected || len(attempt.Results) != 0 || len(attempt.ReviewEvidence) != facts {
						t.Fatal("mixed mandatory predicates or negative owner facts changed", attempt.Reason)
					}
				} else if issue || mode == "qa-changes-no-issue" {
					if attempt.Accepted || attempt.Reason != "CHANGES_REQUIRED" || len(attempt.Results) != 0 || len(attempt.ReviewEvidence) != 3 {
						t.Fatal("negative report facts became acceptance or were lost")
					}
				} else if !attempt.Accepted {
					t.Fatal("valid evidence rejected")
				}
			}
		}
	}
	if !found {
		t.Fatal("business evidence history missing")
	}
	if mixed || issue || mode == "qa-changes-no-issue" {
		if current.State != "RUNNING" && current.State != "WAITING_HUMAN" {
			t.Fatal("negative QA did not reach canonical exception")
		}
		f.execute(command.CancelRun, fixture.key("cancel"), &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Native evidence fixture cleanup"})
	} else if current.State != "SUCCEEDED" {
		t.Fatal("verified evidence did not complete DAG")
	}
	if work, err := service.ClaimIntegrationInvocations(ctx, fixture.gateway, "evidence-no-repeat", 1); err != nil || len(work) != 0 {
		t.Fatal("finished evidence allocated another native WRITE")
	}
}
