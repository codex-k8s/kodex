package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// verifyPersonalAssistantKey executes the real self verification queue and gateway worker ports.
// AUTHENTICATED is a synthetic health observation, not an external provider or JWT proof.
// verifyPersonalAssistantKey исполняет настоящую self verification queue и gateway worker ports.
// AUTHENTICATED — synthetic health observation, не проверка внешнего provider либо JWT.
func verifyPersonalAssistantKey(t *testing.T, ctx context.Context, repository *Repository, service, workers *platformservice.Service, actor value.Principal, ref, key string) entity.PersonalIntegrationCredential {
	t.Helper()
	actor.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, actor, ref)
	if err != nil {
		t.Fatal(err)
	}
	actor.Permission = "platform.personal-integrations.test"
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key + "-test", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: ref, ExpectedConnectionVersion: binding.ConnectionVersion}}); err != nil {
		t.Fatal(err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	claims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, worker, "personal-assistant-verifier", 1)
	if err != nil || len(claims) != 1 {
		t.Fatal("claim personal assistant verification", err)
	}
	probe := personalTestCompletionFromClaim(claims[0])
	worker.Permission = "platform.personal-integration-tests.begin"
	if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, worker, probe); err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.personal-integration-tests.complete"
	probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := workers.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: worker, Mutation: value.Mutation{IdempotencyKey: key + "-verified"}, Payload: probe}); err != nil {
		t.Fatal(err)
	}
	actor.Permission = "platform.personal-integrations.get"
	binding, err = service.GetPersonalIntegrationCredential(ctx, actor, ref)
	if err != nil || binding.State != "READY" {
		t.Fatal("personal assistant key was not verified", err)
	}
	return binding
}

// assertPersonalSystemAssistantRuntime proves ordinary SYSTEM turn admission and effect lineage
// through the real owner runtime and Begin RPC. A refused INTERRUPT_ACTIVE must leave the current
// admitted run intact. Credentials/provider observations and transport-peer admission are synthetic.
// assertPersonalSystemAssistantRuntime проверяет обычный SYSTEM turn и effect lineage через
// настоящий owner runtime и Begin RPC. Отказ INTERRUPT_ACTIVE сохраняет текущий допущенный run.
// Credentials/provider observations и transport-peer admission синтетические.
func assertPersonalSystemAssistantRuntime(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connectionRef string) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-system-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("personal system command %s failed: %v", kind, err)
		}
		return result
	}
	prepareObservedWarmFixture(t, ctx, repository)
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	candidates, err := service.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connectionRef, "gitlab.project.metadata.read", query.Page{Size: 10})
	if err != nil || len(candidates.Items) != 1 {
		t.Fatal("system personal configuration was filtered as a missing shared key", err)
	}
	grant := execute(command.ChangeSystemAssistantIntegrationGrant, "grant", &candidates.ConnectionVersion, command.SystemAssistantIntegrationGrantInput{ConnectionRef: connectionRef, CapabilityKey: "gitlab.project.metadata.read", Enabled: true, ApprovalPolicy: "NONE"}).Connection
	if grant == nil || grant.CredentialRevision != nil || grant.State == "CONNECTED" {
		t.Fatal("system grant fabricated shared credential readiness")
	}
	conversation := execute(command.CreateAssistantConversation, "conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	if conversation == nil {
		t.Fatal("system personal conversation is absent")
	}
	counts := func() [3]int64 {
		t.Helper()
		var counts [3]int64
		if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.runs),(SELECT count(*) FROM control_plane.session_turns),(SELECT version FROM control_plane.assistant_conversations WHERE ref=$1)", conversation.Ref).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
			t.Fatal(err)
		}
		return counts
	}
	refused := func(key string) {
		t.Helper()
		before := counts()
		_, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic refused assistant interruption", DeliveryMode: "INTERRUPT_ACTIVE"}})
		var failure *platformservice.IntegrationCredentialPreflightError
		if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].ConnectionRef != connectionRef || failure.Issues[0].Problem != entity.IntegrationCredentialRevoked || before != counts() {
			t.Fatal("system turn lost personal preflight or committed execution state on refusal", err)
		}
	}
	refused("personal-system-before-key")
	// A SYSTEM conversation's project context is not a different assistant identity.
	// Контекст проекта SYSTEM conversation не подменяет identity самого помощника.
	project := execute(command.CreateProject, "context-project", nil, command.ProjectInput{Name: "System personal context", Language: "ru"}).Project
	other := execute(command.CreateAssistantConversation, "context-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	moved := execute(command.MoveAssistantConversationToProject, "context-move", &other.Version, command.AssistantConversationProjectInput{ConversationRef: other.Ref, ProjectRef: project.Ref}).Conversation
	if moved == nil || moved.AssistantScope != "SYSTEM" || moved.ProjectRef != project.Ref {
		t.Fatal("system assistant project context changed its identity")
	}
	original := conversation
	conversation = moved
	refused("personal-system-moved-context-before-key")
	conversation = original
	self := owner
	self.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, self, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	self.Permission = "platform.personal-integrations.configure"
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-system-key", ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-system-personal-key")); err != nil {
		t.Fatal(err)
	}
	binding = verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connectionRef, "personal-system-key")
	turn := execute(command.AddAssistantTurn, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic system personal read", DeliveryMode: "QUEUE"}).Conversation
	if turn == nil || len(turn.Turns) != 1 {
		t.Fatal("system personal turn is absent")
	}
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	claimed, err := workers.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "personal-system-runtime"}, Payload: command.LeaseInput{WorkloadInstance: "personal-system-worker", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != turn.Turns[0].RunRef {
		t.Fatal("system personal runtime was not claimed", err)
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": turn.Turns[0].RunRef, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-system-read"}, map[string]any{})
	if err != nil {
		t.Fatal("resolve system personal invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-system-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
		t.Fatal("system claim exposed a shared credential or lost its intent", err)
	}
	request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-system-gateway"}
	response, err := personalRuntimeRPCClient(t, workers).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
	if err != nil || !response.GetAdmitted() || response.GetCredentialRevision().GetRef() != binding.Credential.Ref {
		t.Fatal("system Begin did not preserve the root user's exact key", err)
	}
	self.Permission = "platform.personal-integrations.revoke"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: self, Mutation: value.Mutation{IdempotencyKey: "personal-system-revoke", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef}}); err != nil {
		t.Fatal(err)
	}
	before, err := service.GetRun(ctx, owner, turn.Turns[0].RunRef)
	if err != nil {
		t.Fatal(err)
	}
	refused("personal-system-after-revoke")
	after, err := service.GetRun(ctx, owner, before.Ref)
	if err != nil || after.Version != before.Version || after.State != before.State {
		t.Fatal("refused personal interruption cancelled the admitted root", err)
	}
	summary := string(asJSON(map[string]any{"id": 3, "path_with_namespace": "fixture/system-project", "default_branch": "main", "visibility": "private", "archived": false}))
	if _, err := workers.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-system-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:3", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
		t.Fatal("complete admitted system effect after revoke", err)
	}
	execute(command.CancelRun, "cancel", &after.Version, command.RunCommandInput{RunRef: after.Ref})
	cancelled, err := service.GetRun(ctx, owner, after.Ref)
	if err != nil {
		t.Fatal(err)
	}
	priorCounts := counts()
	_, err = service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-system-retry-after-revoke", ExpectedVersion: &cancelled.Version},
		Payload:  command.RunCommandInput{RunRef: cancelled.Ref}})
	var retryFailure *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &retryFailure) || len(retryFailure.Issues) != 1 || retryFailure.Issues[0].Problem != entity.IntegrationCredentialRevoked || counts() != priorCounts {
		t.Fatal("assistant retry bypassed personal preflight or committed a new turn", err)
	}
	assertPersonalArchiveProcessResume(t, ctx, repository, service, workers, owner, worker, connectionRef)
	current, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ChangeSystemAssistantIntegrationGrant, "disable-grant", &current.Version, command.SystemAssistantIntegrationGrantInput{ConnectionRef: connectionRef, CapabilityKey: "gitlab.project.metadata.read", Enabled: false, ApprovalPolicy: "NONE"})
}
