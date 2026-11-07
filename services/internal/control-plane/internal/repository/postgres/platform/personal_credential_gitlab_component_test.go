package platform

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
)

// assertPersonalGitLabWritePermissions exercises the three project write profiles through native
// verification, launch, Human Gate, DB pin, worker claim and one-time Begin RPC. External READ
// proofs and object denials are synthetic; this proves no live GitLab permission or HTTP effect.
// assertPersonalGitLabWritePermissions проверяет три project write профиля через native
// verification, launch, Human Gate, DB pin, worker claim и одноразовый Begin RPC. Внешние READ
// proofs и object denials синтетические; живые GitLab права и HTTP effects не подтверждаются.
func assertPersonalGitLabWritePermissions(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-gitlab-write-")
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{
		DefinitionKey: "gitlab", Name: "Synthetic personal GitLab writes", PublicConfiguration: map[string]any{
			"base_url": "https://git.example.invalid", "project_path": "fixture/personal-writes"}}).Connection
	connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
	owner.Permission = "platform.integrations.credential-policy.set"
	connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
	owner.Permission = "platform.command.integrations.enable"
	connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
	owner.Permission = "platform.personal-integrations.create"
	binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
	owner.Permission = "platform.personal-integrations.configure"
	configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-gitlab-write-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-gitlab-personal-key"))
	if err != nil {
		t.Fatal("configure personal GitLab key", err)
	}
	owner.Permission = "platform.personal-integrations.test"
	execute(command.TestPersonalIntegrationCredential, owner, "verify", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
	verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	claims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-gitlab-write-verifier", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "connectionRef") != connection.Ref {
		t.Fatal("claim personal GitLab verification", err)
	}
	probe := personalTestCompletionFromClaim(claims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
		t.Fatal("begin personal GitLab verification", err)
	}
	probe.Outcome, probe.ConfirmedCapabilityKeys = entity.PersonalIntegrationCredentialAuthenticated, []string{"gitlab.project.metadata.read"}
	verifier.Permission = "platform.personal-integration-tests.complete"
	execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
	owner.Permission = "platform.personal-integrations.get"
	current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "READY" || len(current.VerifiedCapabilities) != 1 {
		t.Fatal("personal GitLab key lost exact READ-only verification", err)
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic personal GitLab writes", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-gitlab-write-writer", "Personal issue editor")
	operations := []string{"gitlab.issue.create", "gitlab.branch.create", "gitlab.merge_request.create"}
	for _, operation := range operations {
		connection = execute(command.ChangeIntegrationGrant, owner, "grant-"+operation, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
	}
	// A legacy health-only observation must block every configured write before run/session INSERT.
	// Legacy health-only observation блокирует все configured write до run/session INSERT.
	assertPersonalGitLabScopeLaunchBlocked(t, ctx, repository, service, owner, project.Ref, agent.Ref, "legacy", entity.IntegrationCredentialUnconfirmed)
	for _, phase := range []string{"read-only", "expired", "checked"} {
		owner.Permission = "platform.personal-integrations.test"
		execute(command.TestPersonalIntegrationCredential, owner, "scope-test-"+phase, &current.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
		verifier.Permission = "platform.personal-integration-tests.claim"
		observedClaims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-gitlab-scope-verifier", 1)
		if err != nil || len(observedClaims) != 1 {
			t.Fatal("claim scoped verification", err)
		}
		profiles, ok := observedClaims[0]["personalObservationProfiles"].([]string)
		writeKeys, keysOK := observedClaims[0]["personalDeniedWriteCapabilityKeys"].([]string)
		if !ok || !keysOK || !slices.Equal(profiles, []string{integrationpackage.CredentialObservationGitLabProjectWriteScopeV1}) || !slices.Equal(writeKeys, operations) {
			t.Fatal("owner did not derive exact immutable metadata scope")
		}
		completion := personalTestCompletionFromClaim(observedClaims[0])
		verifier.Permission = "platform.personal-integration-tests.begin"
		if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, verifier, completion); err != nil {
			t.Fatal("begin scoped verification", err)
		}
		completion.Outcome = entity.PersonalIntegrationCredentialAuthenticated
		completion.ConfirmedCapabilityKeys = []string{"gitlab.project.metadata.read"}
		completion.ObservedPermissionProfiles = profiles
		expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
		if phase == "read-only" {
			completion.DeniedCapabilityKeys = append([]string(nil), operations...)
		}
		if phase == "expired" {
			expiry = time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
		}
		completion.ExpiresAt = &expiry
		verifier.Permission = "platform.personal-integration-tests.complete"
		if phase == "checked" {
			fabricated := completion
			fabricated.DeniedCapabilityKeys = []string{"gitlab.project.delete"}
			_, deniedErr := service.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: verifier, Mutation: value.Mutation{IdempotencyKey: "gitlab-scope-unrequested-denial"}, Payload: fabricated})
			if deniedErr == nil {
				t.Fatal("owner accepted denial outside requested package")
			}
		}
		execute(command.CompletePersonalIntegrationCredentialTest, verifier, "scope-complete-"+phase, nil, completion)
		owner.Permission = "platform.personal-integrations.get"
		current, err = service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
		if err != nil || !slices.Equal(current.ObservedPermissionProfiles, profiles) || current.ExpiresAt == nil || !current.ExpiresAt.Equal(expiry) || slices.Contains(current.VerifiedCapabilities, "gitlab.issue.create") {
			t.Fatal("persisted scoped metadata changed current pins or fabricated WRITE", err)
		}
		if phase == "read-only" {
			assertPersonalGitLabScopeLaunchBlocked(t, ctx, repository, service, owner, project.Ref, agent.Ref, phase, entity.IntegrationCredentialInsufficient)
		}
		if phase == "expired" {
			assertPersonalGitLabScopeLaunchBlocked(t, ctx, repository, service, owner, project.Ref, agent.Ref, phase, entity.IntegrationCredentialExpired)
		}
	}
	run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Perform only explicitly approved synthetic issue actions"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "personal-gitlab-write-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("conditional personal GitLab launch was not claimed")
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	rpc := personalRuntimeRPCClient(t, workers)
	for _, operation := range operations {
		contract, exists := integrationpackage.CredentialResourcePermissionContractFor("gitlab", operation)
		if !exists || slices.Contains(current.VerifiedCapabilities, operation) {
			t.Fatal("GitLab profile missing or global WRITE fabricated")
		}
		input := map[string]any{"title": "Synthetic issue", "description": "Synthetic body"}
		if operation == "gitlab.branch.create" {
			input = map[string]any{"branch": "feature", "ref": "main"}
		} else if operation == "gitlab.merge_request.create" {
			input = map[string]any{"source_branch": "feature", "target_branch": "main", "title": "Draft: Synthetic change", "description": "Synthetic body"}
		}
		invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": "personal-gitlab-write-" + operation}, input)
		if err != nil || stringMap(invocation, "state") != "WAITING_APPROVAL" {
			t.Fatal("personal GitLab write bypassed Human Gate or failed exact pinning", err)
		}
		var storedProfile string
		if err := repository.pool.QueryRow(ctx, `SELECT pin.resource_permission_profile FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef")).Scan(&storedProfile); err != nil || storedProfile != contract.Profile {
			t.Fatal("personal GitLab DB pin lost its exact profile", err)
		}
		for _, wrong := range []string{"", integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1} {
			_, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_personal_invocation_credentials
(invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT pin.invocation_id,pin.organization_id,pin.connection_id,pin.root_run_id,pin.subject_id,pin.binding_id,pin.binding_generation,pin.credential_revision_id,pin.definition_digest,pin.configuration_digest,$2
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef"), wrong)
			var rejected *pgconn.PgError
			if !errors.As(err, &rejected) || rejected.Code != "23514" {
				t.Fatal("database accepted a missing or foreign personal GitLab profile", err)
			}
		}
		gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
		if err != nil {
			t.Fatal(err)
		}
		execute(command.ResolveOwnerGate, owner, "approve-"+operation, &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-gitlab-write-gateway", 1)
		if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(work[0], "resourcePermissionProfile") != contract.Profile || work[0]["credential"] != nil {
			t.Fatal("personal GitLab claim lost mandatory permission check", err)
		}
		request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-gitlab-write-gateway"}
		admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != current.Credential.Ref || admitted.GetResourcePermissionProfile().String() != "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile {
			t.Fatal("personal GitLab Begin lost root key or exact profile", err)
		}
		if operation != "gitlab.branch.create" {
			execute(command.CompleteIntegrationInvocation, gateway, "unknown-"+operation, nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, UnknownOutcome: true, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN"})
			originalEffect, originalInput := stringMap(work[0], "effectKey"), stringMap(work[0], "inputDigest")
			recoveryCodes := []string{"INTEGRATION_REQUEST_REJECTED", "INTEGRATION_AUTH_REJECTED"}
			if operation == "gitlab.issue.create" {
				recoveryCodes = []string{"INTEGRATION_REQUEST_REJECTED", "INTEGRATION_RESPONSE_INVALID", "INTEGRATION_RATE_LIMITED"}
			}
			for _, code := range recoveryCodes {
				recovery, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-gitlab-write-gateway", 1)
				if err != nil || len(recovery) != 1 || stringMap(recovery[0], "invocationRef") != request.InvocationRef || stringMap(recovery[0], "workMode") != "RECOVER_READ_ONLY" || stringMap(recovery[0], "effectKey") != originalEffect || stringMap(recovery[0], "inputDigest") != originalInput {
					t.Fatal("personal recovery lost immutable effect or became Execute", err)
				}
				next := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: request.InvocationRef, LeaseRef: stringMap(recovery[0], "leaseRef"), Fence: stringMap(recovery[0], "fence"), Generation: runtimeRevisionMapInt64(recovery[0], "generation"), WorkloadInstance: "personal-gitlab-write-gateway"}
				read, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), next)
				if err != nil || !read.GetAdmitted() || read.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY || read.GetCredentialRevision().GetRef() != current.Credential.Ref {
					t.Fatal("personal read-only recovery lost admission/current root key", err)
				}
				execute(command.CompleteIntegrationInvocation, gateway, "recovery-failed-"+operation+"-"+code, nil, command.IntegrationInvocationInput{InvocationRef: next.InvocationRef, LeaseRef: next.LeaseRef, Fence: next.Fence, Generation: next.Generation, SafeErrorCode: code})
				stored, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
				if err != nil || stringMap(stored, "state") != "UNKNOWN_OUTCOME" || stringMap(stored, "safeErrorCode") != code || stringMap(stored, "effectReceiptRef") != "" {
					t.Fatal("failed recovery falsely resolved the previous effect", err)
				}
			}
			var executions, recoveries int
			if err := repository.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE NOT admission.recovery_read_only),count(*) FILTER (WHERE admission.recovery_read_only)
FROM control_plane.integration_personal_invocation_admissions admission JOIN control_plane.integration_invocations invocation ON invocation.id=admission.invocation_id WHERE invocation.ref=$1`, request.InvocationRef).Scan(&executions, &recoveries); err != nil || executions != 1 || recoveries != len(recoveryCodes) {
				t.Fatal("recovery created another execute epoch or lost receipts", err)
			}
			if operation == "gitlab.issue.create" {
				more, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-gitlab-write-gateway", 1)
				if err != nil || len(more) != 0 {
					t.Fatal("exhausted read-only recovery budget was automatically reclaimed", err)
				}
			}
		} else {
			code := "INTEGRATION_REQUEST_REJECTED"
			execute(command.CompleteIntegrationInvocation, gateway, "rejected-"+operation, nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, SafeErrorCode: code})
			stored, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
			if err != nil || stringMap(stored, "state") != "FAILED" || stringMap(stored, "safeErrorCode") != code || stringMap(stored, "effectReceiptRef") != "" {
				t.Fatal("native pre-effect GitLab refusal became unknown or fabricated an effect", err)
			}
		}
	}
	retained, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || retained.State != "CHECK_REQUIRED" || retained.Version != current.Version+1 || len(retained.VerifiedCapabilities) != 0 || len(retained.ObservedPermissionProfiles) != 0 || retained.ExpiresAt != nil {
		t.Fatal("recovery auth rejection did not fence the current exact key proofs", err)
	}
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref})
}

// assertPersonalGitLabScopeLaunchBlocked checks the actual launch transaction and every required
// capability. Rejection must create neither a session nor a run, and no shared key may be selected.
// assertPersonalGitLabScopeLaunchBlocked проверяет настоящую launch-транзакцию и все required
// capability. Отказ не создаёт session/run и не выбирает shared key.
func assertPersonalGitLabScopeLaunchBlocked(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, agentRef, phase string, expected entity.IntegrationCredentialProblem) {
	t.Helper()
	var beforeSessions, beforeRuns, afterSessions, afterRuns int
	query := "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)"
	if err := repository.pool.QueryRow(ctx, query).Scan(&beforeSessions, &beforeRuns); err != nil {
		t.Fatal(err)
	}
	_, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-gitlab-scope-block-" + phase}, Payload: command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}, Task: "Synthetic scope preflight"}})
	var failure *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &failure) || len(failure.Issues) != 3 {
		t.Fatal("scope preflight did not report all three writes", err)
	}
	for _, issue := range failure.Issues {
		if issue.Problem != expected {
			t.Fatal("scope preflight misclassified known/unknown/expired metadata")
		}
	}
	if err := repository.pool.QueryRow(ctx, query).Scan(&afterSessions, &afterRuns); err != nil || beforeSessions != afterSessions || beforeRuns != afterRuns {
		t.Fatal("blocked scope preflight created execution state", err)
	}
}
