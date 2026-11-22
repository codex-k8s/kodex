package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// authenticateSSHExecutionFixture uses real setup, the existing personal/shared test claim and purpose health completion; its synthetic observation qualifies only owner persistence, not a live host.
// authenticateSSHExecutionFixture использует real setup, existing personal/shared test claim и purpose health completion; synthetic observation проверяет лишь owner persistence, не live host.
func authenticateSSHExecutionFixture(t *testing.T, f *sshSetupNativeFixture) entity.PersonalIntegrationCredential {
	t.Helper()
	expectedVersion := int64(0)
	if f.shared {
		expectedVersion = f.connection.Version
	}
	setup := f.begin(t, "ssh-execution-first-setup", expectedVersion)
	admission, _ := f.claim(t, setup)
	locator := f.locator(setup)
	if _, err := f.complete(admission, command.SSHIdentityUploadComplete{Identity: &locator}); err != nil {
		t.Fatal("attach original SSH execution identity", err)
	}
	return testSSHExecutionFixtureHealth(t, f, "ssh-execution-health")
}

// testSSHExecutionFixtureHealth queues and completes a fresh public health test for the existing identity at the current Connection version; it never allocates replacement material or rewrites the earlier setup receipt.
// testSSHExecutionFixtureHealth ставит и завершает fresh public health test существующей identity при current Connection version; replacement material не выделяется, прежний setup receipt не переписывается.
func testSSHExecutionFixtureHealth(t *testing.T, f *sshSetupNativeFixture, key string) entity.PersonalIntegrationCredential {
	t.Helper()
	owner := f.transport
	var binding entity.PersonalIntegrationCredential
	var claims []map[string]any
	var err error
	if f.shared {
		owner.Permission = "platform.query.integrations.get"
		f.connection, err = f.service.GetIntegrationConnection(f.ctx, owner, f.connection.Ref)
		if err != nil {
			t.Fatal("read shared SSH identity attachment", err)
		}
		owner.Permission = "platform.command.integrations.test"
		queued, err := f.service.Execute(f.ctx, command.Command{Kind: command.TestConnection, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &f.connection.Version}, Payload: command.ConnectionInput{Ref: f.connection.Ref}})
		if err != nil || queued.Connection == nil {
			t.Fatal("queue shared SSH identity health", err)
		}
		f.connection = *queued.Connection
		gateway := f.owner
		gateway.CallerWorkload, gateway.Permission = "integration-gateway", "platform.runtime.integration-tests.claim"
		claims, err = f.repository.ClaimIntegrationConnectionTests(f.ctx, gateway, "ssh-owner-health-gateway", 16)
		if err != nil {
			t.Fatal("claim shared SSH identity health", err)
		}
		matching := claims[:0]
		for _, claim := range claims {
			if stringMap(claim, "connectionRef") == f.connection.Ref {
				matching = append(matching, claim)
			}
		}
		claims = matching
		if len(claims) != 1 {
			t.Fatal("shared SSH health claim was not exact")
		}
	} else {
		owner.Permission = "platform.personal-integrations.get"
		binding, err = f.service.GetPersonalIntegrationCredential(f.ctx, owner, f.connection.Ref)
		if err != nil || binding.BrokerIdentity == nil {
			t.Fatal("read original execution identity", err)
		}
		owner.Permission = "platform.personal-integrations.test"
		if _, err := f.service.Execute(f.ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &binding.Version},
			Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref, ExpectedConnectionVersion: f.connection.Version}}); err != nil {
			t.Fatal("queue original SSH identity health", err)
		}
		gateway := f.owner
		gateway.CallerWorkload, gateway.Permission = "integration-gateway", "platform.personal-integration-tests.claim"
		claims, err = f.repository.ClaimPersonalIntegrationCredentialTests(f.ctx, gateway, "ssh-owner-health-gateway", 1)
		if err != nil || len(claims) != 1 {
			t.Fatal("claim original SSH identity health", err)
		}
	}
	claim := claims[0]
	health := managedssh.Binding{WorkloadInstance: "ssh-owner-health-gateway", ConnectionTestRef: stringMap(claim, "testRef"),
		LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.health.begin"
	probe, err := f.repository.BeginSSHIdentityHealthProbe(f.ctx, broker, health)
	if err != nil {
		t.Fatal("begin original SSH identity health", err)
	}
	broker.Permission = "platform.runtime.ssh.health.complete"
	receipt, err := f.repository.CompleteSSHIdentityHealthProbe(f.ctx, broker, command.SSHIdentityHealthComplete{Binding: health,
		AttemptRef: probe.AttemptRef, SnapshotDigest: probe.SnapshotDigest, Outcome: entity.PersonalIntegrationCredentialAuthenticated,
		ObservedSubjectDigest: strings.Repeat("d", 64), QualifiedUntil: time.Now().UTC().Add(3 * time.Minute).Truncate(time.Microsecond)})
	if err != nil || !receipt.Applied {
		t.Fatal("complete original SSH identity health", err)
	}
	if f.shared {
		owner.Permission = "platform.query.integrations.get"
		f.connection, err = f.service.GetIntegrationConnection(f.ctx, owner, f.connection.Ref)
		if err != nil || f.connection.State != "CONNECTED" {
			t.Fatal("read authenticated shared SSH identity", err)
		}
		return entity.PersonalIntegrationCredential{}
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err = f.service.GetPersonalIntegrationCredential(f.ctx, owner, f.connection.Ref)
	if err != nil || binding.State != "READY" {
		t.Fatal("read authenticated original SSH identity", err)
	}
	return binding
}

// TestSSHExecutionOwnerComponent exercises actual public source admission through one canonical capacity claim and purpose Begin/Complete; no readiness override or live SSH is used.
// TestSSHExecutionOwnerComponent выполняет actual public source admission через одну canonical capacity claim и purpose Begin/Complete; readiness override и live SSH отсутствуют.
func TestSSHExecutionOwnerComponent(t *testing.T) {
	f := newSSHSetupNativeFixture(t)
	authenticateSSHExecutionFixture(t, f)
	seedObservedCatalogFixture(t, f.ctx, f.repository)
	execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-execution-native-")
	project := execute(command.CreateProject, f.transport, "project", nil, command.ProjectInput{Name: "Disposable SSH owner execution", Language: "ru"}).Project
	agent := createLifecycleAgent(t, f.ctx, f.service, f.transport, project.Ref, "ssh-execution-agent", "SSH bounded reader")
	f.connection = *execute(command.ChangeIntegrationGrant, f.transport, "grant", &f.connection.Version,
		command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: agent.Ref, CapabilityKey: "ssh.system.read", ApprovalPolicy: "NONE", Enabled: true}).Connection
	run := execute(command.LaunchRun, f.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
		Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read the selected synthetic SSH system"}).Run
	worker := resolvedTestPrincipal(t, f.ctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-owner-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("claim original SSH runtime revision")
	}
	invocation, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"),
		"connection_ref": f.connection.Ref, "capability_key": "ssh.system.read", "idempotency_key": "ssh-owner-read"}, map[string]any{})
	if err != nil || stringMap(invocation, "state") != "READY" {
		t.Fatal("resolve original SSH owner invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, f.ctx, f.repository)
	claims, err := f.service.ClaimIntegrationInvocationsWithCapacity(f.ctx, gateway, "ssh-owner-gateway", 1, value.IntegrationInvocationDeliveryCapacity{SSH: 1})
	if err != nil || len(claims) != 1 {
		t.Fatal("claim original SSH invocation through common facade", err)
	}
	claim := claims[0]
	if stringMap(claim, "invocationRef") != stringMap(invocation, "invocationRef") || claim["credentialDeliveryKind"] != "BROKER_IDENTITY" || claim["brokerIdentityPurpose"] != managedssh.IdentityPurpose || claim["credential"] != nil {
		t.Fatal("common facade changed original SSH claim or credential kind")
	}
	assertSSHOwnerGatewayClaim(t, claim, "ssh-owner-gateway")
	binding := managedssh.Binding{WorkloadInstance: "ssh-owner-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
		Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.execution.begin"
	admission, err := f.repository.BeginSSHExecution(f.ctx, broker, binding)
	if err != nil || admission.InvocationRef != binding.InvocationRef || admission.Validate(time.Now()) != nil {
		t.Fatal("begin original SSH execution", err)
	}
	if _, err := f.repository.BeginSSHExecution(f.ctx, broker, binding); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("original SSH Begin repeated dispatch admission", err)
	}
	result := []byte(`{"system":"synthetic"}`)
	completion := command.SSHExecutionComplete{Binding: binding, AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest,
		Observation: managedssh.ExecutionObservation{Outcome: managedssh.ExecutionSucceeded, Result: result, ResultDigest: sshHealthHash(result)}}
	broker.Permission = "platform.runtime.ssh.execution.complete"
	completed, err := f.repository.CompleteSSHExecution(f.ctx, broker, completion)
	if err != nil || !completed.Recorded || !completed.Applied || completed.OwnerState != "SUCCEEDED" {
		t.Fatal("complete original SSH execution", err)
	}
	runtimeReader := worker
	runtimeReader.Permission = "platform.runtime.ssh.invocation.read"
	proof := query.SSHRuntimeRead{InvocationRef: binding.InvocationRef, LeaseRef: stringMap(leases[0], "leaseRef"), Fence: stringMap(leases[0], "fence"),
		Generation: runtimeRevisionMapInt64(leases[0], "generation"), RuntimeInputDigest: stringMap(leases[0], "inputDigest")}
	view, err := f.service.ReadSSHRuntimeInvocation(f.ctx, runtimeReader, proof)
	if err != nil || view.State != "SUCCEEDED" || view.ResultSummary != string(result) || !view.Valid() {
		t.Fatal("read SSH result under original runtime lease", err)
	}
	for _, change := range []func(*query.SSHRuntimeRead){
		func(q *query.SSHRuntimeRead) { q.Fence += "wrong" },
		func(q *query.SSHRuntimeRead) { q.Generation++ },
		func(q *query.SSHRuntimeRead) { q.RuntimeInputDigest = strings.Repeat("f", 64) },
		func(q *query.SSHRuntimeRead) { q.InvocationRef = "inv_foreign_runtime" },
	} {
		bad := proof
		change(&bad)
		if v, err := f.service.ReadSSHRuntimeInvocation(f.ctx, runtimeReader, bad); !errors.Is(err, errs.ErrNotFound) || v != (entity.SSHRuntimeResult{}) {
			t.Fatal("changed runtime lease disclosed SSH output", err)
		}
	}
	replayed, err := f.repository.CompleteSSHExecution(f.ctx, broker, completion)
	if err != nil || !replayed.Applied || len(replayed.Observation.Result) != 0 {
		t.Fatal("historical SSH completion replay republished private output", err)
	}
	completion.Observation.Result = []byte(`{"system":"tampered"}`)
	completion.Observation.ResultDigest = sshHealthHash(completion.Observation.Result)
	if _, err := f.repository.CompleteSSHExecution(f.ctx, broker, completion); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("changed SSH receipt replaced original observation", err)
	}
	testSSHRuntimeACLExpiryDuringConnectionLock(t, f, proof, runtimeReader)

}
