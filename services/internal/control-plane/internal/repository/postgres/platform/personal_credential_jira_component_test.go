package platform

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
)

// assertPersonalJiraResourcePermissions exercises all three Server write profiles through native
// verification, launch, Human Gate, DB pin, worker claim and one-time Begin RPC. External READ
// proofs and object denials are synthetic; this proves no live Jira permission or HTTP effect.
// assertPersonalJiraResourcePermissions проверяет три Server write профиля через native
// verification, launch, Human Gate, DB pin, worker claim и одноразовый Begin RPC. Внешние READ
// proofs и object denials синтетические; живые Jira права и HTTP effects не подтверждаются.
func assertPersonalJiraResourcePermissions(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-jira-")
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{
		DefinitionKey: "jira-server-dc", Name: "Synthetic personal issues", PublicConfiguration: map[string]any{
			"base_url": "https://issues.example.invalid", "context_path": "/tracker", "project_key": "DEMO", "issue_type": "Task", "auth_scheme": "BEARER",
			"issue_create_fields": `{"hours":{"id":"customfield_11002","type":"NUMBER"},"epic":{"id":"customfield_11003","type":"ISSUE_KEY"}}`}}).Connection
	connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
	owner.Permission = "platform.integrations.credential-policy.set"
	connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
	owner.Permission = "platform.command.integrations.enable"
	connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
	owner.Permission = "platform.personal-integrations.create"
	binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
	owner.Permission = "platform.personal-integrations.configure"
	configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-jira-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-jira-personal-key"))
	if err != nil {
		t.Fatal("configure personal Jira key", err)
	}
	owner.Permission = "platform.personal-integrations.test"
	execute(command.TestPersonalIntegrationCredential, owner, "verify", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
	verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	claims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-jira-verifier", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "connectionRef") != connection.Ref {
		t.Fatal("claim personal Jira verification", err)
	}
	probe := personalTestCompletionFromClaim(claims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
		t.Fatal("begin personal Jira verification", err)
	}
	probe.Outcome, probe.ConfirmedCapabilityKeys = entity.PersonalIntegrationCredentialAuthenticated, []string{"jira.project.read", "jira.identity.read"}
	verifier.Permission = "platform.personal-integration-tests.complete"
	execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
	owner.Permission = "platform.personal-integrations.get"
	current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "READY" || len(current.VerifiedCapabilities) != 2 {
		t.Fatal("personal Jira key lost exact READ-only verification", err)
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic personal Jira writes", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-jira-writer", "Personal issue editor")
	operations := []string{"jira.issue.create", "jira.issue.comment.write", "jira.issue.transition.apply"}
	for _, operation := range operations {
		connection = execute(command.ChangeIntegrationGrant, owner, "grant-"+operation, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
	}
	run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Perform only explicitly approved synthetic issue actions"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "personal-jira-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("conditional personal Jira launch was not claimed")
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	rpc := personalRuntimeRPCClient(t, workers)
	for _, operation := range operations {
		contract, exists := integrationpackage.CredentialResourcePermissionContractFor("jira-server-dc", operation)
		if !exists || slices.Contains(current.VerifiedCapabilities, operation) {
			t.Fatal("Jira profile missing or global WRITE fabricated")
		}
		input := map[string]any{"summary": "Synthetic issue", "description": "Synthetic body"}
		if operation == "jira.issue.create" {
			input["fields_json"], input["reporter_self"] = `{"hours":0,"epic":"DEMO-42"}`, true
		}
		if operation == "jira.issue.comment.write" {
			input = map[string]any{"issue_key": "DEMO-1", "body": "Synthetic comment"}
		} else if operation == "jira.issue.transition.apply" {
			input = map[string]any{"issue_key": "DEMO-1", "transition_id": "11"}
		}
		invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": "personal-jira-" + operation}, input)
		if err != nil || stringMap(invocation, "state") != "WAITING_APPROVAL" {
			t.Fatal("personal Jira write bypassed Human Gate or failed exact pinning", err)
		}
		var storedProfile string
		if err := repository.pool.QueryRow(ctx, `SELECT pin.resource_permission_profile FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef")).Scan(&storedProfile); err != nil || storedProfile != contract.Profile {
			t.Fatal("personal Jira DB pin lost its exact profile", err)
		}
		for _, wrong := range []string{"", integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1} {
			_, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_personal_invocation_credentials
(invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT pin.invocation_id,pin.organization_id,pin.connection_id,pin.root_run_id,pin.subject_id,pin.binding_id,pin.binding_generation,pin.credential_revision_id,pin.definition_digest,pin.configuration_digest,$2
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef"), wrong)
			var rejected *pgconn.PgError
			if !errors.As(err, &rejected) || rejected.Code != "23514" {
				t.Fatal("database accepted a missing or foreign personal Jira profile", err)
			}
		}
		gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
		if err != nil {
			t.Fatal(err)
		}
		if operation == "jira.issue.create" {
			if gate.IntegrationIntent == nil {
				t.Fatal("Jira create gate omitted canonical intent")
			}
			preview, _ := json.Marshal(gate.IntegrationIntent.EffectPreview)
			var observed map[string]any
			if json.Unmarshal(preview, &observed) != nil || observed["contentComplete"] != true {
				t.Fatal("Jira custom input preview is incomplete")
			}
			seen := map[string]bool{}
			for _, item := range observed["fields"].([]any) {
				field := item.(map[string]any)
				key := field["key"].(string)
				if key == "fields_json" || key == "reporter_self" {
					if field["value"] != input[key] || field["opaque"] != false {
						t.Fatal("native gate changed Jira create intent")
					}
					seen[key] = true
				}
			}
			if len(seen) != 2 {
				t.Fatal("native gate hid Jira custom fields or self reporter")
			}
		}
		execute(command.ResolveOwnerGate, owner, "approve-"+operation, &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-jira-gateway", 1)
		if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(work[0], "resourcePermissionProfile") != contract.Profile || work[0]["credential"] != nil {
			t.Fatal("personal Jira claim lost mandatory permission check", err)
		}
		if operation == "jira.issue.create" {
			bounded, ok := work[0]["boundedInput"].(map[string]any)
			configuration, configured := work[0]["configuration"].(map[string]any)
			if !ok || !configured || bounded["fields_json"] != input["fields_json"] || bounded["reporter_self"] != true || configuration["issue_create_fields"] != connection.PublicConfiguration["issue_create_fields"] {
				t.Fatal("worker claim changed approved Jira input or configuration snapshot")
			}
		}
		request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-jira-gateway"}
		admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != current.Credential.Ref || admitted.GetResourcePermissionProfile().String() != "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile {
			t.Fatal("personal Jira Begin lost root key or exact profile", err)
		}
		if operation == "jira.issue.create" || operation == "jira.issue.comment.write" {
			output := map[string]any{"id": "1", "key": "DEMO-1"}
			providerRef := "jira-issue:DEMO-1"
			if operation == "jira.issue.comment.write" {
				output = map[string]any{"comment_id": "801", "issue_key": "DEMO-1"}
				providerRef = "jira-comment:801"
			}
			assertPersonalServerReadRecovery(t, ctx, repository, service, workers, gateway, work[0], request, current.Credential.Ref, providerRef, output)
			continue
		}
		code := "INTEGRATION_REQUEST_REJECTED"
		if operation == "jira.issue.create" {
			code = "INTEGRATION_RESPONSE_INVALID"
		} else if operation == "jira.issue.comment.write" {
			code = "INTEGRATION_UNAVAILABLE"
		}
		execute(command.CompleteIntegrationInvocation, gateway, "rejected-"+operation, nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, SafeErrorCode: code})
		stored, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
		if err != nil || stringMap(stored, "state") != "FAILED" || stringMap(stored, "safeErrorCode") != code || stringMap(stored, "effectReceiptRef") != "" {
			t.Fatal("native pre-effect Jira refusal became unknown or fabricated an effect", err)
		}
	}
	retained, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || retained.State != "READY" || retained.Version != current.Version || !slices.Equal(retained.VerifiedCapabilities, current.VerifiedCapabilities) {
		t.Fatal("Jira object denial changed key readiness or global permission proofs", err)
	}
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref})
}
