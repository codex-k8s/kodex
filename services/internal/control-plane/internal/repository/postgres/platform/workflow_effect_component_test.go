package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowEffectProof observes only exact root counters in the existing native invocation/receipt ledger.
// queryWorkflowEffectProof читает только exact root counters существующего native invocation/receipt ledger.
//
//go:embed testdata/sql/workflow_effect_proof.sql
var queryWorkflowEffectProof string

// queryWorkflowEffectDiagnostic observes bounded state/count/barrier metadata in the disposable fixture, never payloads or raw errors.
// queryWorkflowEffectDiagnostic читает bounded state/count/barrier metadata disposable fixture без payloads либо raw errors.
//
//go:embed testdata/sql/workflow_effect_diagnostic.sql
var queryWorkflowEffectDiagnostic string

// workflowEffectDiagnosticReason normalizes only server-owned failure/state enums; unknown text is never logged.
// workflowEffectDiagnosticReason нормализует только server-owned failure/state enums; неизвестный текст не логируется.
func workflowEffectDiagnosticReason(value string) string {
	if value == "" {
		return "NONE"
	}
	if contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "SUCCEEDED", "FAILED", "CANCELLED", workflowArtifactRequired, workflowDeclarationInvalid, workflowReceiptRequired, workflowReceiptLineageInvalid, workflowReceiptLineageAmbiguous, workflowReceiptScopeMismatch, workflowReceiptInvalid, workflowPullRequestRequired, "CAPTURE_CONTINUATION_UNAVAILABLE", "DISPATCH_CONFIGURATION_UNAVAILABLE", "OWNER_CHANGES_REQUESTED"}, value) {
		return value
	}
	return "OTHER"
}

// workflowEffectFailureDiagnostic reads exact owner rows after a failed assertion and reports only closed reasons, counts and pending flags.
// workflowEffectFailureDiagnostic читает exact owner rows после failed assertion и сообщает только closed reasons, counts и pending flags.
func workflowEffectFailureDiagnostic(t *testing.T, ctx context.Context, pool *pgxpool.Pool, origin map[string]any, receiptRef string, snapshot workflowRecoverySnapshot) string {
	t.Helper()
	current, lease, _ := workflowJournalFixture(t, ctx, pool, stringMap(origin, "nodeRef"), stringMap(origin, "leaseRef"))
	var rootState, physicalState, nodeState string
	var revisions, activePublications, blocked, models, callbacks, invocations, continues int64
	var callbackPending bool
	err := pool.QueryRow(ctx, queryWorkflowEffectDiagnostic, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": lease["rootRunID"], "node_id": lease["nodeID"]}).Scan(&rootState, &physicalState, &nodeState, &revisions, &callbackPending, &activePublications, &blocked, &models, &callbacks, &invocations, &continues)
	if err != nil {
		return "diagnostic_read=" + completionOutcomeClass(err)
	}
	journalReason, episodeReason := "", ""
	journal := snapshot.journals["a"]
	if len(journal) > 0 {
		journalReason = journal[len(journal)-1].Reason
	}
	if len(snapshot.episodes) > 0 {
		episodeReason = snapshot.episodes[len(snapshot.episodes)-1].Reason
	}
	receiptReason, receiptRead := "", "NONE"
	tx, err := pool.Begin(ctx)
	if err == nil {
		_, receiptReason, err = readWorkflowIntegrationResultTx(ctx, tx, current, lease, receiptRef)
		rollbackWorkflowJournalProbe(t, ctx, tx)
	}
	if err != nil {
		receiptRead = completionOutcomeClass(err)
	}
	return fmt.Sprintf("root=%s physical=%s node=%s journal_count=%d journal_reason=%s episode_count=%d episode_reason=%s receipt_reason=%s receipt_read=%s rr_count=%d bundle_callback_pending=%t active_publications=%d delivery_blocked=%d active_models=%d undelivered_callbacks=%d pending_invocations=%d continues=%d",
		workflowEffectDiagnosticReason(rootState), workflowEffectDiagnosticReason(physicalState), workflowEffectDiagnosticReason(nodeState), len(journal), workflowEffectDiagnosticReason(journalReason), len(snapshot.episodes), workflowEffectDiagnosticReason(episodeReason), workflowEffectDiagnosticReason(receiptReason), receiptRead, revisions, callbackPending, activePublications, blocked, models, callbacks, invocations, continues)
}

// TestWorkflowEffectRejoinComponent proves a known successful native WRITE survives business rejection
// and owner retry as a read-only receipt rejoin. Credential metadata is the existing synthetic fixture
// exception; every workflow, grant, lease, invocation, gate and result is created through native APIs.
// The synthetic gateway reports one provider effect; neither a real GitHub call nor live PDLC is claimed.
// TestWorkflowEffectRejoinComponent доказывает read-only rejoin успешного WRITE после business rejection
// и owner retry. Credential metadata — существующее synthetic fixture исключение; workflows, grants,
// leases, invocations, gates и results создаются native APIs. Synthetic gateway сообщает один эффект;
// настоящий GitHub вызов либо live PDLC эта проверка не заявляет.
func TestWorkflowEffectRejoinComponent(t *testing.T) {
	for _, name := range []string{"known-success", "current-grant-revoked", "bundle-new-source-refs", "same-node-new-generation", "same-business-continues"} {
		t.Run(name, func(t *testing.T) { runWorkflowEffectRejoinScenario(t, name) })
	}
}

// TestWorkflowServerGateIsolationComponent proves two open effect gates do not revoke independent live models, while new own-gated claims remain closed across expiry and decisions.
// TestWorkflowServerGateIsolationComponent доказывает изоляцию двух effect Gates от независимых моделей и закрытый own-gated claim при expiry/решениях.
func TestWorkflowServerGateIsolationComponent(t *testing.T) {
	runWorkflowEffectRejoinScenario(t, "parallel-open-gates")
}

// runWorkflowEffectRejoinScenario creates one isolated native graph; revocation is an owner command, never fixture SQL.
// runWorkflowEffectRejoinScenario создаёт isolated native graph; revoke выполняется owner command, а не fixture SQL.
func runWorkflowEffectRejoinScenario(t *testing.T, mode string) {
	revoke, bundle := mode == "current-grant-revoked", mode == "bundle-new-source-refs" || mode == "parallel-open-gates"
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated effect rejoin database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	project := f.execute(command.CreateProject, "effect-project", nil, command.ProjectInput{Name: "Exact effect rejoin", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "effect-manager", "Менеджер разбора")
	writer := createLifecycleAgent(t, ctx, service, owner, project.Ref, "effect-writer", "Автор PR")
	f.managerRef, f.agents["a"] = manager.Ref, writer.Ref
	writer = *f.execute(command.ChangeAgentCapability, "effect-artifact", &writer.Version, command.AgentBindingInput{AgentRef: writer.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	var independent entity.Agent
	if mode == "parallel-open-gates" {
		independent = createLifecycleAgent(t, ctx, service, owner, project.Ref, "effect-independent", "Независимый исполнитель")
		independent = *f.execute(command.ChangeAgentCapability, "independent-artifact", &independent.Version, command.AgentBindingInput{AgentRef: independent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		f.agents["b"] = independent.Ref
	}
	connection := *f.execute(command.CreateConnection, "effect-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Rejoin fixture", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("seed existing synthetic credential metadata")
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal("read current connection")
	}
	capabilities := []string{"github.pull_request.create", "github.issue.comment.create"}
	if bundle {
		capabilities = append(capabilities, repositoryBundleOperation)
	}
	for _, capability := range capabilities {
		connection = *f.execute(command.ChangeIntegrationGrant, "effect-grant-"+capability, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: writer.Ref, CapabilityKey: capability, ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
	}
	var bundleSources []entity.Artifact
	var bundleArchive, bundleManifest []byte
	if bundle {
		bundleArchive, bundleManifest, err = repositorybundle.CanonicalContainer([]repositorybundle.File{{Entry: repositorybundle.Entry{Path: "main.go", Mode: "100644"}, Bytes: []byte("package main\nfunc main() {}\n")}})
		if err != nil {
			t.Fatal("build exact typed source fixture")
		}
		for pair := 0; pair < 2; pair++ {
			for index, raw := range [][]byte{bundleArchive, bundleManifest} {
				name := []string{"old-archive.json", "old-manifest.json", "new-archive.json", "new-manifest.json"}[pair*2+index]
				artifact, uploadErr := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "effect-source-" + name}, port.ArtifactUpload{ProjectRef: project.Ref, FileName: name, MediaType: "application/json", SizeBytes: int64(len(raw)), Reader: bytes.NewReader(raw)})
				if uploadErr != nil || artifact.ScanState != "CLEAN" {
					t.Fatal("native exact CLEAN source upload", completionOutcomeClass(uploadErr))
				}
				f.execute(command.ChangeArtifactBinding, "effect-bind-"+name, &artifact.Version, command.ArtifactBindingInput{ArtifactRef: artifact.Ref, AgentRef: writer.Ref, Enabled: true})
				bundleSources = append(bundleSources, artifact)
			}
		}
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Receipt recovery", Purpose: "Сохранить эффект при исправлении обязательного файла.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "a", Position: 1, Name: "PR", AgentRef: writer.Ref, Instructions: "Создать PR и обязательный report.md; повторно не писать Git при восстановлении.", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability, "github.pull_request.create", "github.issue.comment.create"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "fixture/repository"}, {Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}}}}
	if bundle {
		draft.Steps[0].RequiredCapabilityKeys = append(draft.Steps[0].RequiredCapabilityKeys, repositoryBundleOperation)
	}
	if mode == "parallel-open-gates" {
		draft.Steps[0].Parallel, draft.Steps[0].ParallelGroup = true, 1
		draft.Steps = append(draft.Steps, entity.WorkflowStep{Key: "b", Position: 2, Name: "Независимый материал", AgentRef: independent.Ref, Instructions: "Создать independent.md.", TimeoutSeconds: 120, Parallel: true, ParallelGroup: 1, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "independent.md", MediaType: "text/markdown"}}})
	}
	workflow := f.execute(command.CreateWorkflow, "effect-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "effect-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "effect-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := f.execute(command.LaunchRun, "effect-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать PR и полный отчёт.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	f.runRef = run.Ref
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve verified owner principal")
	}
	current, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal("resolve exact owner proof scope")
	}
	claimed := f.claims("effect-origin")
	var origin, independentOrigin map[string]any
	for _, item := range claimed {
		if stringMap(item, "agentRef") == writer.Ref {
			origin = item
		}
		if stringMap(item, "agentRef") == independent.Ref {
			independentOrigin = item
		}
	}
	if origin == nil || (mode != "parallel-open-gates" && len(claimed) != 1) || (mode == "parallel-open-gates" && (len(claimed) != 2 || independentOrigin == nil)) {
		t.Fatal("exact published executions were not dispatched")
	}
	readProof := func() [6]int64 {
		t.Helper()
		var proof [6]int64
		if err := pool.QueryRow(ctx, queryWorkflowEffectProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "organization_id": current.organizationID}).Scan(&proof[0], &proof[1], &proof[2], &proof[3], &proof[4], &proof[5]); err != nil {
			t.Fatal("read exact effect counters", completionOutcomeClass(err))
		}
		return proof
	}
	resolve := func(lease map[string]any, key, capability string, input map[string]any) (map[string]any, error) {
		return service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connection.Ref, "capability_key": capability, "idempotency_key": key}, input)
	}
	var oldPublication entity.RepositoryBundlePublication
	var oldBundleInput port.RepositoryBundlePrepare
	var bundleFixture *repositoryBundleFixture
	prepareBundleInput := func(lease map[string]any, pair int, key string) port.RepositoryBundlePrepare {
		t.Helper()
		input := port.RepositoryBundlePrepare{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), BaseCommitSHA: strings.Repeat("a", 40), IdempotencyKey: key}
		for _, grant := range connection.Grants {
			if grant.CapabilityKey == repositoryBundleOperation {
				input.GrantRef = grant.Ref
			}
		}
		for index, pin := range []*entity.RepositoryBundleArtifactPin{&input.Archive, &input.Manifest} {
			reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
			download, readErr := service.ReadExecutionArtifact(ctx, reader, input.LeaseRef, input.Fence, input.Generation, bundleSources[pair*2+index].Ref)
			if readErr != nil {
				t.Fatal("read exact source pins in current runtime", completionOutcomeClass(readErr))
			}
			_ = download.Reader.Close()
			a := download.Artifact
			*pin = entity.RepositoryBundleArtifactPin{Ref: a.Ref, Revision: a.Revision, Version: a.Version, Size: a.SizeBytes, SHA256: strings.TrimPrefix(a.Digest, "sha256:")}
		}
		return input
	}
	if bundle {
		store := &bundleFixtureObjects{Store: repository.objects}
		repository.objects = store
		bundleFixture = &repositoryBundleFixture{t: t, ctx: ctx, pool: pool, r: repository, service: service, owner: owner, worker: worker, connection: connection, agent: writer, project: project.Ref, origin: origin, archive: bundleArchive, manifest: bundleManifest, store: store, initialPuts: store.puts}
		oldBundleInput = prepareBundleInput(origin, 0, "effect-bundle-first")
		oldPublication, err = service.PrepareRepositoryBundle(ctx, bundleFixture.principal("runtime-controller", "platform.repository-bundles.prepare"), oldBundleInput)
		if err != nil {
			t.Fatal("prepare SERVER published bundle slot", completionOutcomeClass(err))
		}
		bundleFixture.input, bundleFixture.proposal = oldBundleInput, oldPublication
		if mode != "parallel-open-gates" {
			bundleFixture.decide("APPROVE")
			effect := bundleFixture.claim()
			bundleFixture.begin(effect)
			bundleFixture.complete(effect)
		}
	}
	input := map[string]any{"title": "One exact PR", "body": "Native owner receipt", "head": "qa-effect", "base": "main", "draft": false}
	invocation, err := resolve(origin, "effect-first-call", "github.pull_request.create", input)
	if err != nil {
		t.Fatal("admit published effect", completionOutcomeClass(err))
	}
	before := readProof()
	effects := int64(1)
	if bundle {
		effects = 2
	}
	if before[0] != effects || before[2] != effects || before[4] != effects {
		t.Fatal("first effect did not atomically bind one slot and Gate")
	}
	if _, err := resolve(origin, "effect-pending-new-call", "github.pull_request.create", input); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("pending effect admitted another physical key")
	}
	if _, err := resolve(origin, "unbound-comment", "github.issue.comment.create", map[string]any{"issue_number": 1, "body": "must not write"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound SERVER write admitted")
	}
	if readProof() != before {
		t.Fatal("rejected admission changed native ledger")
	}
	if mode == "parallel-open-gates" {
		runWorkflowGateIsolationScenario(t, f, origin, independentOrigin, invocation, oldPublication, resolve, input)
		return
	}
	gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
	if err != nil {
		t.Fatal("read exact effect Gate")
	}
	assertOwnerGateApprovalConsequence(t, gate, false, true)
	f.execute(command.ResolveOwnerGate, "effect-approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	gateway := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "effect-gateway", 1)
	if err != nil || len(work) != 1 {
		t.Fatal("approved effect was not claimable")
	}
	summary := `{"number":7,"state":"open","sha":"` + strings.Repeat("a", 40) + `","draft":false}`
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "effect-native-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "github-pull:7", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}})
	if err != nil {
		t.Fatal("commit known provider receipt", completionOutcomeClass(err))
	}
	state, err := service.GetIntegrationInvocation(ctx, worker, stringMap(invocation, "invocationRef"))
	receipt := stringMap(state, "effectReceiptRef")
	if err != nil || receipt == "" {
		t.Fatal("known native receipt locator unavailable")
	}
	declaration := `{"schema":"kodex.workflow-result.v1","results":[{"key":"pr","receiptRef":"` + receipt + `"},{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`
	if mode == "same-node-new-generation" {
		var expiresAt time.Time
		if raw, err := json.Marshal(origin["expiresAt"]); err != nil || json.Unmarshal(raw, &expiresAt) != nil || expiresAt.IsZero() {
			t.Fatal("native lease expiry is missing")
		}
		wait := time.Until(expiresAt) + 150*time.Millisecond
		if wait < 0 || wait > 35*time.Second {
			t.Fatal("native lease expiry wait is outside its bounded fixture budget")
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("native expiry wait exceeded context")
		case <-timer.C:
		}
		fresh := f.claims("effect-expired-claim")
		if len(fresh) != 1 || stringMap(fresh[0], "nodeRef") != stringMap(origin, "nodeRef") || stringMap(fresh[0], "runRef") != stringMap(origin, "runRef") || stringMap(fresh[0], "runtimeRevisionRef") == stringMap(origin, "runtimeRevisionRef") || stringMap(fresh[0], "leaseRef") == stringMap(origin, "leaseRef") || runtimeRevisionMapInt64(fresh[0], "generation") <= runtimeRevisionMapInt64(origin, "generation") {
			t.Fatal("canonical expiry did not reclaim the same node with a fresh immutable execution")
		}
		retry := fresh[0]
		before := readProof()
		rejoined, err := resolve(retry, "effect-generation-rejoin", "github.pull_request.create", input)
		if err != nil || stringMap(rejoined, "receiptRef") != receipt || stringMap(rejoined, "invocationRef") != stringMap(invocation, "invocationRef") {
			t.Fatal("same-node generation did not create exact receipt bridge", completionOutcomeClass(err))
		}
		expected := before
		expected[3]++
		if readProof() != expected {
			t.Fatal("same-node bridge allocated an external effect")
		}
		f.execute(command.CompleteExecution, "effect-generation-complete", nil, command.CompleteExecutionInput{LeaseRef: stringMap(retry, "leaseRef"), Fence: stringMap(retry, "fence"), Generation: runtimeRevisionMapInt64(retry, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: declaration, Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact("report.md", []byte("Тот же Git receipt после настоящего lease expiry.\n"))}})
		final := f.read()
		if final.state != "SUCCEEDED" || len(final.journals["a"]) != 1 || !final.journals["a"][0].Accepted || final.journals["a"][0].Results[0].ReceiptRef != receipt {
			t.Fatal("validated execution bridge did not precede ambiguous legacy lineage")
		}
		proofScope, lease, _ := workflowJournalFixture(t, ctx, pool, stringMap(retry, "nodeRef"), stringMap(retry, "leaseRef"))
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin legacy ambiguity read-only proof")
		}
		pins := workflowResultPins(proofScope, lease)
		pins["receipt_ref"] = receipt
		var legacy workflowIntegrationResult
		err = tx.QueryRow(ctx, queryWorkflowResultIntegration, pins).Scan(&legacy.Operation, &legacy.Input, &legacy.Scope, &legacy.Summary, &legacy.Digest, &legacy.ProviderRef, &legacy.LineageFailure)
		rollbackWorkflowJournalProbe(t, ctx, tx)
		if err != nil || legacy.LineageFailure != workflowReceiptLineageAmbiguous {
			t.Fatal("legacy reader lost its no-bridge ambiguity denial")
		}
		if work, err := service.ClaimIntegrationInvocations(ctx, gateway, "effect-no-generation-write", 1); err != nil || len(work) != 0 {
			t.Fatal("same-node retry issued another WRITE")
		}
		return
	}
	f.execute(command.CompleteExecution, "effect-missing-artifact", nil, command.CompleteExecutionInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: declaration})
	completedOrigin := origin
	if bundle {
		deferred := f.read()
		if len(deferred.journals["a"]) != 0 || len(deferred.episodes) != 0 {
			t.Fatal("bundle owner ACK was bypassed by premature business finalization")
		}
		acknowledgements := f.claims("effect-bundle-owner-ack")
		if len(acknowledgements) != 1 || stringMap(acknowledgements[0], "runRef") != stringMap(origin, "runRef") || stringMap(acknowledgements[0], "nodeRef") == stringMap(origin, "nodeRef") || stringMap(acknowledgements[0], "runtimeRevisionRef") == stringMap(origin, "runtimeRevisionRef") || stringMap(acknowledgements[0], "leaseRef") == stringMap(origin, "leaseRef") {
			t.Fatal("bundle ACK did not create exact fresh same-business execution")
		}
		ack := acknowledgements[0]
		bundleState, err := service.GetIntegrationInvocation(ctx, worker, oldPublication.InvocationRef)
		if err != nil || stringMap(bundleState, "effectReceiptRef") == "" || !strings.Contains(stringMap(ack, "task"), stringMap(bundleState, "effectReceiptRef")) || !strings.Contains(stringMap(ack, "task"), strings.Repeat("b", 40)) {
			t.Fatal("bundle ACK lost canonical immutable receipt and commit")
		}
		ackReceipt, err := resolve(ack, "effect-owner-ack-pr-rejoin", "github.pull_request.create", input)
		if err != nil || stringMap(ackReceipt, "receiptRef") != receipt || stringMap(ackReceipt, "invocationRef") != stringMap(invocation, "invocationRef") {
			t.Fatal("fresh ACK execution did not rejoin exact previous PR receipt", completionOutcomeClass(err))
		}
		f.execute(command.CompleteExecution, "effect-bundle-ack-missing-artifact", nil, command.CompleteExecutionInput{LeaseRef: stringMap(ack, "leaseRef"), Fence: stringMap(ack, "fence"), Generation: runtimeRevisionMapInt64(ack, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: declaration})
		completedOrigin = ack
	}
	snapshot := f.read()
	if len(snapshot.episodes) != 1 || snapshot.episodes[0].Reason != workflowArtifactRequired {
		t.Fatal("known WRITE did not survive distinct artifact rejection:", workflowEffectFailureDiagnostic(t, ctx, pool, completedOrigin, receipt, snapshot))
	}
	managerWork := f.claims("effect-manager")
	if len(managerWork) != 1 || stringMap(managerWork[0], "agentRef") != manager.Ref {
		t.Fatal("recovery manager missing")
	}
	f.complete("effect-manager-analysis", managerWork[0], "", false)
	recovery := f.recoveryGate(f.read())
	f.execute(command.ResolveOwnerGate, "effect-recovery", &recovery.Version, command.GateResolutionInput{GateRef: recovery.Ref, Decision: "APPROVE"})
	retries := f.claims("effect-retry")
	if len(retries) != 1 || stringMap(retries[0], "nodeRef") == stringMap(origin, "nodeRef") || stringMap(retries[0], "runRef") == stringMap(origin, "runRef") {
		t.Fatal("owner retry did not create fresh physical execution")
	}
	retry := retries[0]
	before = readProof()
	if bundle {
		freshInput := prepareBundleInput(retry, 1, "effect-bundle-fresh")
		if freshInput.Archive.Ref == oldBundleInput.Archive.Ref || freshInput.Manifest.Ref == oldBundleInput.Manifest.Ref || freshInput.Archive.SHA256 != oldBundleInput.Archive.SHA256 || freshInput.Manifest.SHA256 != oldBundleInput.Manifest.SHA256 {
			t.Fatal("bundle fixture did not replace only opaque source locators")
		}
		wrong := freshInput
		wrong.BaseCommitSHA = strings.Repeat("d", 40)
		if _, err := service.PrepareRepositoryBundle(ctx, bundleFixture.principal("runtime-controller", "platform.repository-bundles.prepare"), wrong); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("changed Git base silently reused publication")
		}
		if readProof() != before {
			t.Fatal("changed bundle allocated a new effect")
		}
		rejoined, err := service.PrepareRepositoryBundle(ctx, bundleFixture.principal("runtime-controller", "platform.repository-bundles.prepare"), freshInput)
		if err != nil || rejoined.Ref != oldPublication.Ref || rejoined.InvocationRef != oldPublication.InvocationRef || rejoined.State != "SUCCEEDED" {
			t.Fatal("new source refs did not rejoin exact old Git fact", completionOutcomeClass(err))
		}
		if rejoined.Rejoin == nil || rejoined.OriginRunRef != oldPublication.OriginRunRef || rejoined.OriginRuntimeRevisionRef != oldPublication.OriginRuntimeRevisionRef || rejoined.Archive != oldPublication.Archive || rejoined.Manifest != oldPublication.Manifest || rejoined.Rejoin.RunRef != stringMap(retry, "runRef") || rejoined.Rejoin.NodeRef != stringMap(retry, "nodeRef") || rejoined.Rejoin.RuntimeRevisionRef != stringMap(retry, "runtimeRevisionRef") || rejoined.Rejoin.LeaseRef != freshInput.LeaseRef || rejoined.Rejoin.Generation != freshInput.Generation || rejoined.Rejoin.Archive != freshInput.Archive || rejoined.Rejoin.Manifest != freshInput.Manifest || rejoined.Rejoin.ReceiptRef == "" || !repositorybundle.ValidDigest(rejoined.Rejoin.ReceiptDigest) {
			t.Fatal("typed rejoin replaced old pins or lost exact current execution/source proof")
		}
		expected := before
		expected[3]++
		if readProof() != expected {
			t.Fatal("bundle rejoin allocated native invocation, Gate or receipt")
		}
		if work, err := service.ClaimRepositoryBundles(ctx, bundleFixture.principal("integration-gateway", "platform.repository-bundles.work.claim"), "effect-no-second-bundle", 1); err != nil || len(work) != 0 {
			t.Fatal("read-only bundle rejoin issued another worker lease")
		}
		bundleFixture.noObjectEffects()
		before = readProof()
	}
	changed := map[string]any{"title": "Changed PR", "body": "Native owner receipt", "head": "qa-effect", "base": "main", "draft": false}
	if _, err := resolve(retry, "effect-changed-call", "github.pull_request.create", changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("recovery approval authorized changed WRITE")
	}
	if readProof() != before {
		t.Fatal("changed WRITE allocated native work")
	}
	if revoke {
		connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal("read fresh owner grant version")
		}
		f.execute(command.ChangeIntegrationGrant, "effect-revoke", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: writer.Ref, CapabilityKey: "github.pull_request.create", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: false})
		if _, err := resolve(retry, "effect-revoked-rejoin", "github.pull_request.create", input); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("historical success replaced current grant authority")
		}
		if readProof() != before {
			t.Fatal("revoked rejoin changed original receipt or allocated work")
		}
		fresh, err := service.GetRun(ctx, owner, run.Ref)
		if err != nil {
			t.Fatal("read owner cancellation version")
		}
		f.execute(command.CancelRun, "effect-revoked-cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Closed current authority regression"})
		if proof := readProof(); proof[0] != 1 || proof[1] != 1 || proof[2] != 1 || proof[3] != 0 || proof[5] != 0 {
			t.Fatal("cancellation lost known effect or recreated write")
		}
		return
	}
	rejoined, err := resolve(retry, "effect-fresh-call", "github.pull_request.create", input)
	if err != nil || stringMap(rejoined, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(rejoined, "receiptRef") != receipt {
		t.Fatal("fresh attempt did not read-only rejoin exact old receipt", completionOutcomeClass(err))
	}
	after := readProof()
	expected := before
	expected[3]++
	if after != expected {
		t.Fatal("rejoin changed native invocation, Gate, receipt or pending counters")
	}
	if _, err := resolve(retry, "effect-rejoin-replay", "github.pull_request.create", input); err != nil || readProof() != after {
		t.Fatal("same-attempt rejoin was not idempotent")
	}
	if work, err := service.ClaimIntegrationInvocations(ctx, gateway, "effect-gateway-no-retry", 1); err != nil || len(work) != 0 {
		t.Fatal("rejoin admitted another provider write")
	}
	if mode == "same-business-continues" {
		f.execute(command.CompleteExecution, "effect-capture-progress", nil, command.CompleteExecutionInput{LeaseRef: stringMap(retry, "leaseRef"), Fence: stringMap(retry, "fence"), Generation: runtimeRevisionMapInt64(retry, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["report"]}`, Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact("report.md", []byte("EARLY capture перед final declaration.\n"))}})
		technical := f.claims("effect-technical-continuation")
		if len(technical) != 1 || stringMap(technical[0], "nodeRef") == stringMap(retry, "nodeRef") || stringMap(technical[0], "runRef") != stringMap(retry, "runRef") {
			t.Fatal("capture did not create exact same-business CONTINUES")
		}
		retry = technical[0]
		before := readProof()
		rejoined, err := resolve(retry, "effect-technical-rejoin", "github.pull_request.create", input)
		if err != nil || stringMap(rejoined, "receiptRef") != receipt {
			t.Fatal("technical continuation did not rejoin the same immutable receipt", completionOutcomeClass(err))
		}
		expected := before
		expected[3]++
		if readProof() != expected {
			t.Fatal("technical receipt bridge recreated an effect")
		}
	}
	f.execute(command.CompleteExecution, "effect-retry-complete", nil, command.CompleteExecutionInput{LeaseRef: stringMap(retry, "leaseRef"), Fence: stringMap(retry, "fence"), Generation: runtimeRevisionMapInt64(retry, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: declaration, Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact("report.md", []byte("Полный обязательный отчёт.\n"))}})
	final := f.read()
	if final.state != "SUCCEEDED" || len(final.journals["a"]) != 2 || !final.journals["a"][1].Accepted || len(final.journals["a"][1].Results) != 2 || final.journals["a"][1].Results[0].ReceiptRef != receipt {
		t.Fatal("new attempt evaluator did not accept exact previous receipt through rejoin")
	}
	if _, err := resolve(retry, "effect-terminal-call", "github.pull_request.create", input); err == nil {
		t.Fatal("terminal root admitted receipt rejoin")
	}
}
