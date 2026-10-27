package platform

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestSSHComposeWorkflowComponent proves native accepted merge dependency, safe Gate,
// exact deployment acceptance and read-only recovery without SSH, Docker or model calls.
// Synthetic provider completions exercise the owner boundary; they do not prove live health.
// TestSSHComposeWorkflowComponent проверяет native accepted merge, безопасный Gate,
// exact deployment acceptance и read-only recovery без SSH, Docker либо модели.
// Synthetic provider completions проверяют owner boundary, но не live health.
func TestSSHComposeWorkflowComponent(t *testing.T) {
	for _, mode := range []string{"accepted-deployment", "origin-first", "known-success-rejoin", "unknown-readonly", "source-acl-revoked"} {
		t.Run(mode, func(t *testing.T) { testSSHComposeWorkflow(t, mode) })
	}
}

// testSSHComposeWorkflow creates all workflow, connection, secret, grant and receipt
// state through native domain commands; only the existing Git credential metadata fixture is seeded.
// testSSHComposeWorkflow создаёт workflow, connection, secret, grant и receipt штатными
// domain commands; SQL seed ограничен существующей Git credential metadata fixture.
func testSSHComposeWorkflow(t *testing.T, mode string) {
	fixture := newSSHComposeComponentFixture(t)
	public := fixture.enableSSHComposeComponentDefinition(t)
	configureComponentArtifactScanner(t, fixture.repository)
	worker := fixture.principal(t, "runtime-controller", "platform.runtime.execution.claim")
	f := &workflowRecoveryRegression{t: t, ctx: fixture.ctx, pool: fixture.pool, repository: fixture.repository, service: fixture.service, owner: fixture.owner, worker: worker, projectRef: fixture.projectRef, agents: map[string]string{}}
	manager := createLifecycleAgent(t, f.ctx, f.service, f.owner, f.projectRef, "ssh-workflow-manager", "SSH recovery manager")
	f.managerRef = manager.Ref
	agents := map[string]entity.Agent{}
	for _, key := range []string{"a", "b", "c"} {
		agent := createLifecycleAgent(t, f.ctx, f.service, f.owner, f.projectRef, "ssh-workflow-"+key, "SSH workflow "+key)
		if key == "c" {
			agent = *f.execute(command.ChangeAgentCapability, "ssh-artifact", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		}
		agents[key] = agent
		f.agents[key] = agent.Ref
	}
	github := *f.execute(command.CreateConnection, "ssh-github", nil, command.ConnectionInput{DefinitionKey: "github", Name: "SSH immutable source", PublicConfiguration: map[string]any{"owner": "example", "repository": "project"}}).Connection
	if _, err := f.pool.Exec(f.ctx, queryIntegrationGrantPolicyCredentialFixture, github.Ref); err != nil {
		t.Fatal("seed existing synthetic Git credential metadata")
	}
	var err error
	github, err = f.service.GetIntegrationConnection(f.ctx, f.owner, github.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ agent, capability, approval string }{{"a", "github.pull_request.create", "HUMAN_EACH_EFFECT"}, {"b", "github.pull_request.merge", "HUMAN_EACH_EFFECT"}, {"c", "github.commit.read", "NONE"}, {"c", "github.repository.content.list", "NONE"}, {"c", "github.repository.content.read", "NONE"}} {
		github = *f.execute(command.ChangeIntegrationGrant, "ssh-grant-"+grant.capability, &github.Version, command.IntegrationGrantInput{ConnectionRef: github.Ref, AgentRef: agents[grant.agent].Ref, CapabilityKey: grant.capability, ApprovalPolicy: grant.approval, Enabled: true}).Connection
	}
	secret := fixture.createSecret(t, "ssh-workflow-key", "SSH_COMPOSE", false)
	connection := fixture.connection(t, secret, "SSH workflow target")
	_, health := fixture.healthClaim(t, connection, "workflow")
	connected := fixture.execute(t, fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), command.CompleteConnectionTest, "workflow-connected", nil, command.IntegrationConnectionTestInput{TestRef: health.ConnectionTestRef, LeaseRef: health.LeaseRef, Fence: health.Fence, Generation: health.Generation, Success: true}).Connection
	if connected == nil || connected.State != "CONNECTED" {
		t.Fatal("native SSH readiness completion missing")
	}
	connection = *connected
	for _, capability := range []string{"ssh.compose.inspect", "ssh.compose.deploy", "ssh.compose.status"} {
		approval := "NONE"
		if capability == "ssh.compose.deploy" {
			approval = "HUMAN_EACH_EFFECT"
		}
		connection = *f.execute(command.ChangeIntegrationGrant, "ssh-grant-"+capability, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agents["c"].Ref, CapabilityKey: capability, ApprovalPolicy: approval, Enabled: true}).Connection
	}
	queueMode := mode == "rotated-secret-queue" || mode == "source-acl-queue" || mode == "target-acl-prefix"
	var nextConnection entity.IntegrationConnection
	if queueMode {
		nextConnection = fixture.queueTarget(t, agents["c"].Ref)
	}
	target, err := sshcompose.TargetFromConfiguration(fixture.configuration(secret))
	if err != nil {
		t.Fatal(err)
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Native SSH dependency proof", Purpose: "Exact merge to approved SSH receipt", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 360, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{
		{Key: "a", Position: 1, Name: "PR", AgentRef: agents["a"].Ref, Instructions: "Return exact PR receipt", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{"github.pull_request.create"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: target.Repository}}},
		{Key: "b", Position: 2, Name: "Merge", AgentRef: agents["b"].Ref, Instructions: "Merge only accepted PR", TimeoutSeconds: 120, DependsOn: []string{"a"}, RequiredCapabilityKeys: []string{"github.pull_request.merge"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "merge", Kind: "INTEGRATION_RECEIPT", Repository: target.Repository, SubjectStepKey: "a", Operation: "github.pull_request.merge"}}},
		{Key: "c", Position: 3, Name: "Deploy", AgentRef: agents["c"].Ref, Instructions: "Deploy exact accepted merge once and report its receipt", TimeoutSeconds: 120, DependsOn: []string{"b"}, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability, "github.commit.read", "github.repository.content.list", "github.repository.content.read", "ssh.compose.inspect", "ssh.compose.deploy", "ssh.compose.status"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "deployment", Kind: "DEPLOYMENT", Repository: target.Repository, SubjectStepKey: "b", Operation: "ssh.compose.deploy", TargetRef: target.Ref}, {Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}},
	}}
	workflow := f.execute(command.CreateWorkflow, "ssh-create", nil, command.WorkflowInput{ProjectRef: f.projectRef, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "ssh-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "ssh-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	if workflow.Published == nil {
		t.Fatal("SSH workflow publication missing")
	}
	launchActor := f.owner
	var sourceBinding entity.AccessBinding
	var targetBindings []entity.AccessBinding
	if mode == "source-acl-revoked" || mode == "source-acl-queue" || mode == "target-acl-prefix" {
		targetRefs := []string{connection.Ref}
		if queueMode {
			targetRefs = append(targetRefs, nextConnection.Ref)
		}
		launchActor, sourceBinding, targetBindings = fixture.sourceActor(t, github.Ref, targetRefs...)
	}
	launched := fixture.execute(t, launchActor, command.LaunchRun, "ssh-launch", nil, command.LaunchRunInput{ProjectRef: f.projectRef, Task: "Complete immutable SSH release", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}})
	f.runRef = launched.Run.Ref
	claim := func(key, step string) map[string]any {
		t.Helper()
		items := f.claims(key)
		if len(items) != 1 || stringMap(items[0], "agentRef") != agents[step].Ref {
			t.Fatalf("native SSH stage claim missing: step=%s", step)
		}
		return items[0]
	}
	resolve := func(lease map[string]any, connectionRef, capability, key string, input map[string]any) map[string]any {
		t.Helper()
		value, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connectionRef, "capability_key": capability, "idempotency_key": key}, input)
		if err != nil {
			t.Fatalf("native SSH integration prepare: key=%s class=%s", key, completionOutcomeClass(err))
		}
		return value
	}
	approve := func(invocation map[string]any, key string) entity.OwnerGate {
		t.Helper()
		gate, err := f.service.GetOwnerGate(f.ctx, f.owner, stringMap(invocation, "gateRef"))
		if err != nil || gate.State != "OPEN" {
			t.Fatal("exact owner Gate missing")
		}
		f.execute(command.ResolveOwnerGate, key, &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
		return gate
	}
	completeModel := func(key string, lease map[string]any, declaration string, artifact bool) {
		t.Helper()
		payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: declaration}
		if artifact {
			payload.Artifacts = []command.CompletedArtifact{workflowCaptureProgressArtifact("report.md", []byte("Exact native SSH owner receipt.\n"))}
		}
		f.execute(command.CompleteExecution, key, nil, payload)
	}
	author := claim("ssh-author", "a")
	pr := resolve(author, github.Ref, "github.pull_request.create", "ssh-pr", map[string]any{"title": "SSH release", "head": "qa-ssh", "base": "main", "draft": false})
	approve(pr, "ssh-pr-approve")
	gitWorker := fixture.principal(t, "integration-gateway", "platform.runtime.integrations.claim")
	gitClaim := func() map[string]any {
		t.Helper()
		items, err := f.service.ClaimIntegrationInvocations(f.ctx, gitWorker, "ssh-git-fixture", 1)
		if err != nil || len(items) != 1 {
			t.Fatal("native Git lease missing")
		}
		return items[0]
	}
	sha := strings.Repeat("a", 40)
	receipt := completeSSHFixtureInvocation(t, fixture, gitWorker, gitClaim(), `{"number":7,"state":"open","sha":"`+sha+`","draft":false}`, "github-pull:7", false)
	completeModel("ssh-author-complete", author, `{"schema":"kodex.workflow-result.v1","results":[{"key":"pr","receiptRef":"`+receipt+`"}]}`, false)
	merger := claim("ssh-merger", "b")
	merge := resolve(merger, github.Ref, "github.pull_request.merge", "ssh-merge", map[string]any{"pull_request_number": 7, "sha": sha, "merge_method": "merge"})
	approve(merge, "ssh-merge-approve")
	mergeReceipt := completeSSHFixtureInvocation(t, fixture, gitWorker, gitClaim(), `{"merged":true,"sha":"`+sha+`"}`, "github-pull:7", false)
	completeModel("ssh-merger-complete", merger, `{"schema":"kodex.workflow-result.v1","results":[{"key":"merge","receiptRef":"`+mergeReceipt+`"}]}`, false)
	sre := claim("ssh-sre", "c")
	inspector := resolve(sre, connection.Ref, "ssh.compose.inspect", "ssh-inspect", map[string]any{"merge_invocation_ref": stringMap(merge, "invocationRef")})
	sshWorker := fixture.principal(t, "integration-gateway", "platform.runtime.ssh-compose.claim")
	sshClaim := func() map[string]any {
		t.Helper()
		items, err := f.service.ClaimSSHComposeInvocations(f.ctx, sshWorker, "ssh-workflow-fixture", 1)
		if err != nil || len(items) != 1 {
			t.Fatalf("native SSH lease missing: %s", completionOutcomeClass(err))
		}
		return items[0]
	}
	inspectClaim := sshClaim()
	tree, bundle := strings.Repeat("b", 40), strings.Repeat("c", 64)
	completeSSHFixtureInvocation(t, fixture, gitWorker, inspectClaim, string(asJSON(map[string]any{"ready": true, "target_ref": target.Ref, "merged_sha": sha, "tree_sha": tree, "bundle_sha256": bundle})), "ssh-compose:"+target.Ref+":"+stringMap(inspectClaim, "invocationRef"), false)
	deployment := resolve(sre, connection.Ref, "ssh.compose.deploy", "ssh-deploy", map[string]any{"inspect_invocation_ref": stringMap(inspector, "invocationRef")})
	gate, err := f.service.GetOwnerGate(f.ctx, f.owner, stringMap(deployment, "gateRef"))
	if err != nil || gate.IntegrationIntent == nil {
		t.Fatal("safe deployment preview missing")
	}
	raw := string(asJSON(gate.IntegrationIntent))
	for _, coordinate := range []string{sha, tree, bundle, target.RuntimeImage} {
		if !strings.Contains(raw, coordinate) {
			t.Fatal("deployment Gate omitted immutable release coordinate")
		}
	}
	for _, private := range []string{"originRuntimeRevisionId", "originLeaseId", "sshSecret", "secretUid"} {
		if strings.Contains(raw, private) {
			t.Fatal("deployment Gate leaked private custody metadata")
		}
	}
	if items, err := f.service.ClaimSSHComposeInvocations(f.ctx, sshWorker, "ssh-before-gate", 1); err != nil || len(items) != 0 {
		t.Fatal("SSH WRITE claim bypassed Gate")
	}
	if mode == "origin-first" {
		completeModel("ssh-origin-before-gate", sre, "Waiting for the exact owner-approved SSH effect.", false)
		pending := f.read()
		if pending.state != "WAITING_HUMAN" || len(pending.journals["c"]) != 0 {
			t.Fatal("pending owner Gate accepted or closed business deployment")
		}
	}
	approve(deployment, "ssh-deploy-approve")
	deployed := sshClaim()
	var snapshot sshcompose.Snapshot
	bytes, ok := deployed["sshSnapshot"].([]byte)
	if !ok || sshcompose.Decode(bytes, &snapshot, 32768) != nil || snapshot.OriginRuntimeRevisionID == "" || snapshot.OriginLeaseID == "" || snapshot.OriginGeneration != runtimeRevisionMapInt64(sre, "generation") {
		t.Fatal("SSH snapshot lost exact immutable execution")
	}
	if queueMode {
		fixture.assertAdmissionQueue(t, f, mode, secret, connection, nextConnection, sourceBinding, targetBindings, sre, deployed)
		return
	}
	if mode == "source-acl-revoked" {
		fixture.assertSourceAccessRevocation(t, f, worker, sourceBinding, connection.Ref, sre, deployed, snapshot)
		return
	}
	if mode == "unknown-readonly" {
		completeSSHFixtureInvocation(t, fixture, gitWorker, deployed, "", "", true)
		completeModel("ssh-pending-model", sre, "Waiting for authoritative read-only SSH recovery.", false)
		pending := f.read()
		if pending.state != "RUNNING" || len(pending.journals["c"]) != 0 {
			t.Fatal("UNKNOWN accepted or terminalized deployment")
		}
		timer := time.NewTimer(31 * time.Second)
		defer timer.Stop()
		select {
		case <-f.ctx.Done():
			t.Fatal("SSH recovery backoff exceeded bounded context")
		case <-timer.C:
		}
		deployed = sshClaim()
		if stringMap(deployed, "workMode") != "RECOVER_READ_ONLY" {
			t.Fatal("UNKNOWN reissued SSH WRITE")
		}
		var permit sshcompose.SignedPermit
		bytes, ok := deployed["sshPermit"].([]byte)
		if !ok || sshcompose.Decode(bytes, &permit, 16384) != nil {
			t.Fatal("UNKNOWN recovery permit missing")
		}
		verified, err := sshcompose.Verify(permit, map[string]ed25519.PublicKey{sshcompose.Digest(public): public}, time.Now())
		if err != nil || verified.Purpose != "STATUS" {
			t.Fatal("UNKNOWN recovery did not receive STATUS-only permit")
		}
	}
	now := time.Now().Unix()
	generation := runtimeRevisionMapInt64(deployed, "generation")
	if mode == "unknown-readonly" {
		generation--
	}
	remote := sshcompose.Receipt{Version: 1, TargetRef: target.Ref, ProjectRef: target.ProjectRef, DeploymentRef: snapshot.DeploymentRef, EffectKey: snapshot.DeploymentEffectKey, InputDigest: snapshot.DeploymentInputDigest, MergedSHA: sha, TreeSHA: tree, BundleSHA256: bundle, Image: target.RuntimeImage, Generation: generation, State: "SUCCEEDED", HealthOK: true, StartedAt: now - 1, FinishedAt: now, ExpiresAt: now + 20, Deadline: now + 100}
	output := string(asJSON(map[string]any{"receipt": string(asJSON(remote)), "target_ref": target.Ref, "deployed_sha": sha, "bundle_sha256": bundle, "image_digest": target.RuntimeImage[len(target.RuntimeImage)-71:], "health_ok": true}))
	deploymentReceipt := completeSSHFixtureInvocation(t, fixture, gitWorker, deployed, output, "ssh-compose:"+target.Ref+":"+snapshot.DeploymentRef, false)
	declaration := `{"schema":"kodex.workflow-result.v1","results":[{"key":"deployment","receiptRef":"` + deploymentReceipt + `"},{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`
	if mode == "unknown-readonly" || mode == "origin-first" {
		sre = claim("ssh-readonly-continuation", "c")
		if !strings.Contains(stringMap(sre, "task"), deploymentReceipt) || !strings.Contains(stringMap(sre, "task"), "receipt_ref") {
			t.Fatal("native asynchronous continuation omitted owner receipt locator")
		}
	}
	completeModel("ssh-deployment-result", sre, declaration, mode != "known-success-rejoin")
	if mode == "known-success-rejoin" {
		rejected := f.read()
		if len(rejected.episodes) != 1 || rejected.episodes[0].Reason != workflowArtifactRequired {
			t.Fatal("deployment receipt waived missing business artifact")
		}
		managerItems := f.claims("ssh-manager-analysis")
		if len(managerItems) != 1 || stringMap(managerItems[0], "agentRef") != manager.Ref {
			t.Fatal("owner recovery manager missing")
		}
		f.complete("ssh-manager-complete", managerItems[0], "", false)
		recovery := f.recoveryGate(f.read())
		f.execute(command.ResolveOwnerGate, "ssh-retry-approve", &recovery.Version, command.GateResolutionInput{GateRef: recovery.Ref, Decision: "APPROVE"})
		retry := claim("ssh-retry", "c")
		if stringMap(retry, "runRef") == stringMap(sre, "runRef") || stringMap(retry, "runtimeRevisionRef") == stringMap(sre, "runtimeRevisionRef") {
			t.Fatal("business retry reused physical execution")
		}
		rejoined := resolve(retry, connection.Ref, "ssh.compose.deploy", "ssh-deploy-rejoin", map[string]any{"inspect_invocation_ref": stringMap(inspector, "invocationRef")})
		if stringMap(rejoined, "receiptRef") != deploymentReceipt || stringMap(rejoined, "invocationRef") != stringMap(deployed, "invocationRef") {
			t.Fatal("known SSH success did not rejoin exact native receipt")
		}
		if items, err := f.service.ClaimSSHComposeInvocations(f.ctx, sshWorker, "ssh-no-second-write", 1); err != nil || len(items) != 0 {
			t.Fatal("business retry reissued SSH WRITE")
		}
		completeModel("ssh-retry-result", retry, declaration, true)
	}
	final := f.read()
	history := final.journals["c"]
	if final.state != "SUCCEEDED" || len(history) == 0 || !history[len(history)-1].Accepted {
		t.Fatal("owner did not accept exact SSH dependency and deployment")
	}
	var accepted entity.WorkflowAcceptedResult
	for _, result := range history[len(history)-1].Results {
		if result.Kind == "DEPLOYMENT" {
			accepted = result
		}
	}
	if accepted.ReceiptRef != deploymentReceipt || accepted.HeadSHA != sha || accepted.TargetRef != target.Ref || accepted.BundleSHA256 != bundle || !accepted.HealthOK {
		t.Fatal("accepted deployment lost immutable native receipt pins")
	}
}

// completeSSHFixtureInvocation submits one synthetic provider result with real claim pins,
// then reads the owner's immutable receipt; UNKNOWN never fabricates a success receipt.
// completeSSHFixtureInvocation отправляет synthetic provider result с реальными claim pins
// и читает immutable receipt владельца; UNKNOWN не выдумывает success receipt.
func completeSSHFixtureInvocation(t *testing.T, fixture *sshComposeComponentFixture, principal value.Principal, claim map[string]any, summary, provider string, unknown bool) string {
	t.Helper()
	payload := command.IntegrationInvocationInput{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), Success: !unknown, UnknownOutcome: unknown, EffectKey: stringMap(claim, "effectKey"), InputDigest: stringMap(claim, "inputDigest"), ProviderEffectRef: provider, ResultSummary: summary}
	if unknown {
		payload.SafeErrorCode = "INTEGRATION_OUTCOME_UNKNOWN"
	} else {
		payload.ResponseDigest = workflowResultSHA([]byte(summary))
	}
	key := "ssh-workflow-complete-" + payload.InvocationRef + "-" + stringMap(claim, "leaseRef")
	if _, err := fixture.service.Execute(fixture.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: principal, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload}); err != nil {
		t.Fatalf("native SSH effect completion: %s", completionOutcomeClass(err))
	}
	state, err := fixture.service.GetIntegrationInvocation(fixture.ctx, fixture.principal(t, "runtime-controller", "platform.runtime.execution.claim"), payload.InvocationRef)
	if err != nil {
		t.Fatal("native invocation readback failed")
	}
	ref := stringMap(state, "effectReceiptRef")
	if unknown {
		if stringMap(state, "state") != "UNKNOWN_OUTCOME" || ref != "" {
			t.Fatal("UNKNOWN became a native success")
		}
	} else if ref == "" || stringMap(state, "state") != "SUCCEEDED" {
		t.Fatal("native successful receipt missing")
	}
	return ref
}

// TestSSHComposeWorkflowInvalidTarget rejects a mismatched accepted dependency before granting a new deployment slot.
// TestSSHComposeWorkflowInvalidTarget отклоняет несовпавшую accepted dependency до выдачи нового deployment slot.
func TestSSHComposeWorkflowInvalidTarget(t *testing.T) {
	requirement, _, proof, inputs, _ := workflowDeploymentFixture(t)
	step := entity.WorkflowStep{DependsOn: []string{"merge"}, ResultRequirements: []entity.WorkflowResultRequirement{requirement}}
	for _, scope := range [][]byte{proof.Scope, []byte(`{"target_ref":"ssh_foreign"}`)} {
		_, err := workflowEffectSlot(step, inputs, "ssh.compose.deploy", scope, []byte(`{"inspect_invocation_ref":"inv_fixture1838"}`))
		if string(scope) == string(proof.Scope) {
			if err != nil {
				t.Fatal("exact deployment slot rejected")
			}
		} else if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign deployment slot admitted")
		}
	}
}
