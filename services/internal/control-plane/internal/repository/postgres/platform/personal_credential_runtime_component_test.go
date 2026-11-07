package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalIntegrationRuntime follows the actual owner service from credential verification
// through launch, runtime/RR, invocation, gateway claim, one-time Begin and completion. Provider
// health/output are synthetic observations; this is neither live JWT nor an external HTTP test.
// assertPersonalIntegrationRuntime проходит настоящий owner service от проверки credential через
// launch, runtime/RR, invocation, gateway claim, одноразовый Begin и completion. Наблюдения health/
// output синтетические; это не live JWT и не тест внешнего HTTP.
func assertPersonalIntegrationRuntime(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, second value.Principal, connection entity.IntegrationConnection) {
	t.Helper()
	workerService, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	self := owner
	self.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	self.Permission = "platform.personal-integrations.configure"
	binding, err = service.ConfigurePersonalIntegrationCredential(ctx, self,
		value.Mutation{IdempotencyKey: "personal-runtime-key-replace", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-runtime-owner-key"))
	if err != nil {
		t.Fatal("configure personal runtime key", err)
	}
	self.Permission = "platform.personal-integrations.test"
	queued, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: self,
		Mutation: value.Mutation{IdempotencyKey: "personal-runtime-key-check", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}})
	if err != nil || queued.PersonalIntegrationCredential == nil {
		t.Fatal("queue personal runtime verification", err)
	}
	verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	probeClaims, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-runtime-verifier", 1)
	if err != nil || len(probeClaims) != 1 {
		t.Fatal("claim personal runtime verification", err)
	}
	probe := personalTestCompletionFromClaim(probeClaims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := workerService.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
		t.Fatal("begin personal runtime verification", err)
	}
	verifier.Permission = "platform.personal-integration-tests.complete"
	probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: verifier,
		Mutation: value.Mutation{IdempotencyKey: "personal-runtime-key-verified"}, Payload: probe}); err != nil {
		t.Fatal("complete personal runtime verification", err)
	}
	self.Permission = "platform.personal-integrations.get"
	binding, err = service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil || binding.State != "READY" || binding.Credential == nil {
		t.Fatal("personal runtime key not verified", err)
	}
	execute := func(kind command.Kind, principal value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: principal, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("personal runtime command %s failed: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Personal runtime project", Language: "ru"}).Project
	if project == nil {
		t.Fatal("missing personal runtime project")
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-runtime-agent", "Personal runtime reader")
	granted := execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true})
	if granted.Connection == nil || granted.Connection.State == "CONNECTED" || granted.Connection.CredentialRevision != nil {
		t.Fatal("personal grant fabricated shared readiness")
	}
	assertPersonalRuntimeSecondUserPreflight(t, ctx, repository, service, owner, second, connection.Ref, project.Ref, agent.Ref)
	assertPersonalNoninteractiveConfiguration(t, ctx, repository, service, owner, project.Ref, agent.Ref, connection.Ref)
	run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read synthetic project metadata"}).Run
	if run == nil {
		t.Fatal("missing personal runtime root")
	}
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	claimed := execute(command.ClaimExecution, worker, "runtime-claim", nil, command.LeaseInput{WorkloadInstance: "personal-runtime-worker", Limit: 1}).RuntimeItems
	if len(claimed) != 1 || stringMap(claimed[0], "runRef") != run.Ref {
		t.Fatalf("personal runtime was not claimed: count=%d", len(claimed))
	}
	input := map[string]string{"run_ref": run.Ref, "node_ref": stringMap(claimed[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-runtime-read"}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, input, map[string]any{})
	if err != nil || stringMap(invocation, "state") != "READY" {
		t.Fatal("personal runtime failed real invocation admission", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-runtime-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") {
		t.Fatal("personal runtime invocation not claimed", err)
	}
	if required, _ := work[0]["credentialAdmissionRequired"].(bool); !required || work[0]["credential"] != nil {
		t.Fatal("personal claim exposed shared credential or skipped Begin")
	}
	admission := command.PersonalIntegrationInvocationAdmission{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-runtime-gateway"}
	gateway.Permission = "platform.personal-integration-calls.begin"
	rpc := personalRuntimeRPCClient(t, workerService)
	rpcInput := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: admission.InvocationRef, LeaseRef: admission.LeaseRef, Fence: admission.Fence, Generation: admission.Generation, WorkloadInstance: admission.WorkloadInstance}
	assertPersonalRuntimeRPCRefusals(t, ctx, rpc, rpcInput)
	admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), rpcInput)
	if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != binding.Credential.Ref || admitted.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE {
		t.Fatal("positive personal runtime Begin failed exact owner selection", err)
	}
	rpcRepeated, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), rpcInput)
	if err != nil || rpcRepeated.GetAdmitted() || rpcRepeated.GetCredentialRevision() != nil {
		t.Fatal("RPC Begin replay issued a second execution permit", err)
	}
	repeated, err := workerService.BeginPersonalIntegrationInvocation(ctx, gateway, admission)
	if !errors.Is(err, errs.ErrConflict) || repeated.Credential != nil {
		t.Fatal("personal Begin replay authorized another execution", err)
	}
	summary := string(asJSON(map[string]any{"id": 1, "path_with_namespace": "fixture/project", "default_branch": "main", "visibility": "private", "archived": false}))
	completion := command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-complete"}, Payload: command.IntegrationInvocationInput{
		InvocationRef: admission.InvocationRef, LeaseRef: admission.LeaseRef, Fence: admission.Fence, Generation: admission.Generation, Success: true,
		EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:1", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}
	if _, err := service.Execute(ctx, completion); err != nil {
		t.Fatal("complete admitted personal invocation", err)
	}
	stored, err := service.GetIntegrationInvocation(ctx, gateway, admission.InvocationRef)
	if err != nil || stringMap(stored, "state") != "SUCCEEDED" || stringMap(stored, "effectReceiptRef") == "" {
		t.Fatal("personal runtime completion lost receipt", err)
	}
	if _, err := service.Execute(ctx, completion); err != nil {
		t.Fatal("replay personal completion receipt", err)
	}
	verifyAgain := func(key string) entity.PersonalIntegrationCredential {
		t.Helper()
		p := owner
		p.Permission = "platform.personal-integrations.get"
		current, err := service.GetPersonalIntegrationCredential(ctx, p, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		p.Permission = "platform.personal-integrations.test"
		if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: p, Mutation: value.Mutation{IdempotencyKey: key + "-test", ExpectedVersion: &current.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: current.ConnectionVersion}}); err != nil {
			t.Fatal("queue runtime re-verification", err)
		}
		v := verifier
		v.Permission = "platform.personal-integration-tests.claim"
		claims, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, v, "personal-runtime-verifier", 1)
		if err != nil || len(claims) != 1 {
			t.Fatal("claim runtime re-verification", err)
		}
		probe := personalTestCompletionFromClaim(claims[0])
		v.Permission = "platform.personal-integration-tests.begin"
		if err := workerService.BeginPersonalIntegrationCredentialProbe(ctx, v, probe); err != nil {
			t.Fatal(err)
		}
		probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
		v.Permission = "platform.personal-integration-tests.complete"
		if _, err := workerService.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: v, Mutation: value.Mutation{IdempotencyKey: key + "-verified"}, Payload: probe}); err != nil {
			t.Fatal(err)
		}
		p.Permission = "platform.personal-integrations.get"
		current, err = service.GetPersonalIntegrationCredential(ctx, p, connection.Ref)
		if err != nil || current.State != "READY" {
			t.Fatal("runtime re-verification did not restore exact current key", err)
		}
		return current
	}
	newWork := func(key string) map[string]any {
		t.Helper()
		next := make(map[string]string, len(input))
		for k, v := range input {
			next[k] = v
		}
		next["idempotency_key"] = key
		inv, err := service.ResolveIntegrationInvocation(ctx, worker, next, map[string]any{})
		if err != nil {
			t.Fatal("resolve subsequent personal invocation", err)
		}
		claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-runtime-gateway", 1)
		if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(inv, "invocationRef") {
			t.Fatal("claim subsequent personal invocation", err)
		}
		return claims[0]
	}
	beginInput := func(work map[string]any) *cp.BeginPersonalIntegrationInvocationRequest {
		return &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), WorkloadInstance: "personal-runtime-gateway"}
	}
	completeInput := func(work map[string]any, key, code string) command.Command {
		payload := command.IntegrationInvocationInput{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), SafeErrorCode: code}
		if code == "" {
			payload.Success = true
			payload.EffectKey = stringMap(work, "effectKey")
			payload.InputDigest = stringMap(work, "inputDigest")
			payload.ProviderEffectRef = "gitlab-project:1"
			payload.ResultSummary = summary
			payload.ResponseDigest = workflowResultSHA([]byte(summary))
		}
		return command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload}
	}
	unstarted := newWork("personal-runtime-auth-failure")
	authCompletion := completeInput(unstarted, "personal-runtime-auth-completed", "INTEGRATION_AUTH_REJECTED")
	if _, err := service.Execute(ctx, authCompletion); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("personal completion fabricated an effect without Begin", err)
	}
	allowed, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), beginInput(unstarted))
	if err != nil || !allowed.GetAdmitted() {
		t.Fatal("admit synthetic authentication failure", err)
	}
	if _, err := service.Execute(ctx, authCompletion); err != nil {
		t.Fatal("complete actual admitted authentication rejection", err)
	}
	self.Permission = "platform.personal-integrations.get"
	recheck, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil || recheck.State != "CHECK_REQUIRED" || recheck.Credential.Ref != binding.Credential.Ref || len(recheck.VerifiedCapabilities) != 0 {
		t.Fatal("authentication rejection retained stale personal readiness", err)
	}
	if _, err := service.Execute(ctx, authCompletion); err != nil {
		t.Fatal("replay authentication failure receipt", err)
	}
	afterReplay, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil || afterReplay.Version != recheck.Version {
		t.Fatal("failure replay reset the credential twice", err)
	}
	binding = verifyAgain("personal-runtime-old-key-recheck")
	oldWork := newWork("personal-runtime-old-key-late-error")
	allowed, err = rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), beginInput(oldWork))
	if err != nil || !allowed.GetAdmitted() || allowed.GetCredentialRevision().GetRef() != binding.Credential.Ref {
		t.Fatal("admit old revision before rotation", err)
	}
	self.Permission = "platform.personal-integrations.configure"
	rotated, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-runtime-rotated-key", ExpectedVersion: &binding.Version}, connection.Ref, binding.ConnectionVersion, []byte("synthetic-runtime-rotated-key"))
	if err != nil || rotated.Credential.Ref == binding.Credential.Ref {
		t.Fatal("rotate admitted personal key", err)
	}
	rotated = verifyAgain("personal-runtime-new-key-recheck")
	if _, err := service.Execute(ctx, completeInput(oldWork, "personal-runtime-old-key-error-complete", "INTEGRATION_AUTH_REJECTED")); err != nil {
		t.Fatal("record old revision's late authentication rejection", err)
	}
	self.Permission = "platform.personal-integrations.get"
	retained, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil || retained.State != "READY" || retained.Version != rotated.Version || retained.Credential.Ref != rotated.Credential.Ref || retained.Generation != rotated.Generation {
		t.Fatal("old authentication error poisoned a rotated personal key", err)
	}
	currentWork := newWork("personal-runtime-new-key-call")
	allowed, err = rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), beginInput(currentWork))
	if err != nil || !allowed.GetAdmitted() || allowed.GetCredentialRevision().GetRef() != rotated.Credential.Ref {
		t.Fatal("next invocation did not select the verified rotated revision", err)
	}
	if _, err := service.Execute(ctx, completeInput(currentWork, "personal-runtime-new-key-complete", "")); err != nil {
		t.Fatal("complete new revision's call", err)
	}
	if history, err := service.GetIntegrationInvocation(ctx, gateway, admission.InvocationRef); err != nil || stringMap(history, "state") != "SUCCEEDED" || stringMap(history, "effectReceiptRef") != stringMap(stored, "effectReceiptRef") {
		t.Fatal("rotation rewrote an earlier effect receipt", err)
	}
	for _, ordering := range []string{"begin-first", "revoke-first", "concurrent"} {
		self.Permission = "platform.personal-integrations.get"
		before, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if before.State != "READY" {
			self.Permission = "platform.personal-integrations.configure"
			if _, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-runtime-race-restore-" + ordering, ExpectedVersion: &before.Version}, connection.Ref, before.ConnectionVersion, []byte("synthetic-race-key-"+ordering)); err != nil {
				t.Fatal("restore isolated key between revoke orderings", err)
			}
			before = verifyAgain("personal-runtime-race-verify-" + ordering)
		}
		work := newWork("personal-runtime-race-" + ordering)
		request := beginInput(work)
		revoker := owner
		revoker.Permission = "platform.personal-integrations.revoke"
		revoke := func() error {
			_, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: revoker,
				Mutation: value.Mutation{IdempotencyKey: "personal-runtime-race-revoke-" + ordering, ExpectedVersion: &before.Version},
				Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}})
			return err
		}
		var response *cp.BeginPersonalIntegrationInvocationResponse
		if ordering == "concurrent" {
			start := make(chan struct{})
			begun := make(chan struct {
				response *cp.BeginPersonalIntegrationInvocationResponse
				err      error
			}, 1)
			revoked := make(chan error, 1)
			go func() {
				<-start
				r, e := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
				begun <- struct {
					response *cp.BeginPersonalIntegrationInvocationResponse
					err      error
				}{r, e}
			}()
			go func() { <-start; revoked <- revoke() }()
			close(start)
			result := <-begun
			response, err = result.response, result.err
			if revokeErr := <-revoked; revokeErr != nil {
				t.Fatal("concurrent personal revoke did not commit", revokeErr)
			}
		} else if ordering == "revoke-first" {
			if err := revoke(); err != nil {
				t.Fatal("serial personal revoke did not commit", err)
			}
			response, err = rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		} else {
			response, err = rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
			if revokeErr := revoke(); revokeErr != nil {
				t.Fatal("post-admission personal revoke did not commit", revokeErr)
			}
		}
		if err != nil || response == nil || ordering == "begin-first" && !response.GetAdmitted() || ordering == "revoke-first" && response.GetAdmitted() {
			t.Fatal("personal revoke/Begin ordering violated admission", ordering, err)
		}
		self.Permission = "platform.personal-integrations.get"
		after, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
		if err != nil || after.State != "REVOKED" || after.Generation != before.Generation+1 {
			t.Fatal("committed revoke did not fence future personal calls", ordering, err)
		}
		if response.GetAdmitted() {
			if response.GetCredentialRevision().GetRef() != before.Credential.Ref {
				t.Fatal("admitted call borrowed another credential", ordering)
			}
			if _, err := service.Execute(ctx, completeInput(work, "personal-runtime-race-complete-"+ordering, "")); err != nil {
				t.Fatal("revoke erased an already admitted external result", ordering, err)
			}
		} else {
			if response.GetCredentialRevision() != nil {
				t.Fatal("denied personal call exposed a credential descriptor", ordering)
			}
			history, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
			if err != nil || stringMap(history, "state") != "FAILED" || stringMap(history, "effectReceiptRef") != "" {
				t.Fatal("unstarted revoked call retained a runnable or fabricated effect", ordering, err)
			}
			if _, err := service.Execute(ctx, completeInput(work, "personal-runtime-race-fake-complete-"+ordering, "")); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("revoked unstarted call accepted a fabricated completion", ordering, err)
			}
		}
		if repeated, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request); err != nil || repeated.GetAdmitted() || repeated.GetCredentialRevision() != nil {
			t.Fatal("revoke allowed a new or repeated external start", ordering, err)
		}
	}
	freshRun, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel-runtime", &freshRun.Version, command.RunCommandInput{RunRef: run.Ref})
	assertPersonalRuntimeSecondUserCall(t, ctx, repository, service, workerService, owner, second, worker, gateway, verifier, connection.Ref, rotated.Credential.Ref, rpc)
}
