package platform

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// enableSSHComposeComponentDefinition requires the shipped consumer and configures only a deterministic disposable signer.
// enableSSHComposeComponentDefinition требует shipped consumer и настраивает только детерминированный disposable signer.
func (fixture *sshComposeComponentFixture) enableSSHComposeComponentDefinition(t *testing.T) ed25519.PublicKey {
	t.Helper()
	definition := fixture.repository.integrationDefinitions["ssh-compose"]
	if definition.Spec.Readiness != "READY" || definition.Metadata.Version != "1.0.0" {
		t.Fatal("shipped SSH consumer is not registered")
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	defer clear(key)
	if err := fixture.repository.ConfigureSSHComposeSigningKey(key); err != nil {
		t.Fatal(err)
	}
	return append(ed25519.PublicKey(nil), key[32:]...)
}

// principal resolves a real synthetic workload admission instead of constructing actor authority from payload fields.
// principal разрешает synthetic workload admission вместо создания actor authority из payload.
func (fixture *sshComposeComponentFixture) principal(t *testing.T, workload, operation string) value.Principal {
	t.Helper()
	return resolvedTestPrincipal(t, fixture.ctx, fixture.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: operation}, workload)
}

// execute preserves the normal command owner, OCC, audit and event transaction in the disposable database.
// execute сохраняет обычную owner/OCC/audit/event транзакцию команды в disposable БД.
func (fixture *sshComposeComponentFixture) execute(t *testing.T, principal value.Principal, kind command.Kind, key string, version *int64, payload any) command.Result {
	t.Helper()
	result, err := fixture.service.Execute(fixture.ctx, command.Command{Kind: kind, Principal: principal, Mutation: value.Mutation{IdempotencyKey: "ssh-lease-" + key, ExpectedVersion: version}, Payload: payload})
	if err != nil {
		t.Fatalf("SSH fixture command %s failed: %v", kind, err)
	}
	return result
}

// connection creates a project-scoped SSH target with only public metadata and a reserved secret locator.
// connection создаёт project-scoped SSH target только с public metadata и reserved secret locator.
func (fixture *sshComposeComponentFixture) connection(t *testing.T, secret entity.RuntimeSecret, name string) entity.IntegrationConnection {
	t.Helper()
	configuration := map[string]any{}
	for key, value := range fixture.configuration(secret) {
		configuration[key] = value
	}
	result := fixture.execute(t, fixture.owner, command.CreateConnection, name+"-connection", nil, command.ConnectionInput{DefinitionKey: "ssh-compose", Name: name, PublicConfiguration: configuration})
	if result.Connection == nil {
		t.Fatal("SSH connection missing")
	}
	return *result.Connection
}

// healthClaim obtains the ordinary bounded read-only connection-test lease without contacting SSH.
// healthClaim получает обычную bounded read-only connection-test lease без обращения к SSH.
func (fixture *sshComposeComponentFixture) healthClaim(t *testing.T, connection entity.IntegrationConnection, key string) (map[string]any, port.SSHComposeLeaseBinding) {
	t.Helper()
	fixture.execute(t, fixture.owner, command.TestConnection, key+"-test", &connection.Version, command.ConnectionInput{Ref: connection.Ref})
	claims, err := fixture.service.ClaimIntegrationConnectionTests(fixture.ctx, fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), "ssh-fixture", 32)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		if stringMap(claim, "connectionRef") == connection.Ref {
			return claim, port.SSHComposeLeaseBinding{ConnectionTestRef: stringMap(claim, "testRef"), WorkloadInstance: "ssh-fixture", LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64)}
		}
	}
	t.Fatal("SSH health lease was not claimed")
	return nil, port.SSHComposeLeaseBinding{}
}

// TestSSHComposeLeaseComponent checks real owner permit/projection fencing and dependency cancellation without remote effects.
// TestSSHComposeLeaseComponent проверяет реальные owner permit/projection fences и dependency cancel без remote effects.
func TestSSHComposeLeaseComponent(t *testing.T) {
	fixture := newSSHComposeComponentFixture(t)
	public := fixture.enableSSHComposeComponentDefinition(t)
	broker := fixture.principal(t, "secret-broker", "platform.credential-projections.ssh-compose.resolve")
	t.Run("health pins and rotation close old credential authority", func(t *testing.T) {
		secret := fixture.createSecret(t, "lease-health", "SSH_COMPOSE", false)
		connection := fixture.connection(t, secret, "health target")
		claim, binding := fixture.healthClaim(t, connection, "health")
		resolved, err := fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, binding)
		if err != nil {
			t.Fatalf("resolve health projection: %v", err)
		}
		var signed sshcompose.SignedPermit
		if err = sshcompose.Decode(resolved["permit"].([]byte), &signed, 16384); err != nil {
			t.Fatal(err)
		}
		permit, err := sshcompose.Verify(signed, map[string]ed25519.PublicKey{sshcompose.Digest(public): public}, time.Now())
		if err != nil || permit.Purpose != "INSPECT" || permit.InvocationRef != binding.ConnectionTestRef || permit.Generation != binding.Generation {
			t.Fatal("owner health permit lost exact binding")
		}
		for _, mutate := range []func(*port.SSHComposeLeaseBinding){func(b *port.SSHComposeLeaseBinding) { b.Fence = "foreign" }, func(b *port.SSHComposeLeaseBinding) { b.Generation++ }, func(b *port.SSHComposeLeaseBinding) { b.WorkloadInstance = "foreign" }, func(b *port.SSHComposeLeaseBinding) { b.LeaseRef = "lea_foreign_fixture" }, func(b *port.SSHComposeLeaseBinding) { b.ConnectionTestRef = "tst_foreign_fixture" }} {
			bad := binding
			mutate(&bad)
			if _, err = fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, bad); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("foreign health binding admitted: %v", err)
			}
		}
		completeRuntimeSecretRotate(t, fixture.ctx, fixture.service, runtimeSecretOwnerPrincipal(fixture.owner, "secret.rotate"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.consume"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.complete"), secret, runtimeSecretHashB, "ssh-health-rotate")
		if _, err = fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, binding); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("old health revision projected after rotation: %v", err)
		}
		var state string
		var lease *string
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT state,lease_ref FROM control_plane.integration_connection_tests WHERE ref=$1`, binding.ConnectionTestRef).Scan(&state, &lease); err != nil {
			t.Fatal(err)
		}
		if state != "CANCELLED" || lease != nil {
			t.Fatal("rotation left health lease active")
		}
		_, err = fixture.service.Execute(fixture.ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), Mutation: value.Mutation{IdempotencyKey: "ssh-health-stale-complete"}, Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(claim, "testRef"), LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}})
		if err == nil {
			t.Fatal("old health test marked new credential connected")
		}
		fresh, err := fixture.service.GetIntegrationConnection(fixture.ctx, fixture.owner, connection.Ref)
		if err != nil || fresh.State != "NOT_CONNECTED" || fresh.Version != connection.Version+2 || !contains(fresh.NextActions, "TEST") {
			t.Fatalf("rotated health aggregate was not reset exactly: %v", err)
		}
		var eventCount int
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM control_plane.outbox_events WHERE convert_from(payload,'UTF8')::jsonb->>'eventName'='INTEGRATION_CONNECTION_CHANGED' AND convert_from(payload,'UTF8')::jsonb->>'aggregateRef'=$1 AND (convert_from(payload,'UTF8')::jsonb->>'aggregateVersion')::bigint=$2 AND convert_from(payload,'UTF8')::jsonb->'data'->>'state'='NOT_CONNECTED'`, fresh.Ref, fresh.Version).Scan(&eventCount); err != nil || eventCount != 1 {
			t.Fatalf("rotated health event cardinality/version: %v", err)
		}
		_, replacement := fixture.healthClaim(t, fresh, "health-replacement")
		if replacement.ConnectionTestRef == binding.ConnectionTestRef || replacement.LeaseRef == binding.LeaseRef {
			t.Fatal("replacement health reused the old lease identity")
		}
		newProjection, err := fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, replacement)
		if err != nil || newProjection["descriptor"].(entity.RuntimeSecretRevisionDescriptor).Revision != secret.CurrentRevision+1 {
			t.Fatalf("replacement health did not pin new secret revision: %v", err)
		}
		_, err = fixture.service.Execute(fixture.ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), Mutation: value.Mutation{IdempotencyKey: "ssh-health-stale-after-replacement"}, Payload: command.IntegrationConnectionTestInput{TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}})
		if err == nil {
			t.Fatal("old health completion superseded a new claimed test")
		}
		var replacementGeneration int64
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT state,lease_ref,generation FROM control_plane.integration_connection_tests WHERE ref=$1`, replacement.ConnectionTestRef).Scan(&state, &lease, &replacementGeneration); err != nil || state != "CLAIMED" || lease == nil || *lease != replacement.LeaseRef || replacementGeneration != replacement.Generation {
			t.Fatalf("old completion changed replacement lease: %v", err)
		}
		currentSecret, err := fixture.service.GetRuntimeSecret(fixture.ctx, runtimeSecretOwnerPrincipal(fixture.owner, "secret.view"), secret.Ref)
		if err != nil {
			t.Fatal(err)
		}
		revoke, err := fixture.service.PrepareRuntimeSecretOperation(fixture.ctx, runtimeSecretOwnerPrincipal(fixture.owner, "secret.revoke"), port.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: secret.Ref, Mutation: value.Mutation{IdempotencyKey: "ssh-health-revoke", ExpectedVersion: &currentSecret.Version}})
		if err != nil {
			t.Fatal(err)
		}
		revokeClaim, err := fixture.service.ConsumeRuntimeSecretOperation(fixture.ctx, runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.consume"), port.RuntimeSecretConsumeInput{OperationGrant: revoke.OperationGrant, ClaimantID: "ssh-revoke-fixture"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fixture.service.CompleteRuntimeSecretOperation(fixture.ctx, runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.complete"), port.RuntimeSecretCompleteInput{OperationRef: revoke.OperationRef, ClaimantID: "ssh-revoke-fixture", ClaimGeneration: revokeClaim.ClaimGeneration}); err != nil {
			t.Fatal(err)
		}
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT state,lease_ref FROM control_plane.integration_connection_tests WHERE ref=$1`, replacement.ConnectionTestRef).Scan(&state, &lease); err != nil || state != "CANCELLED" || lease != nil {
			t.Fatalf("revoke retained health lease: %v", err)
		}
		if _, err = fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, replacement); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("revoked credential retained projection: %v", err)
		}
		fresh, err = fixture.service.GetIntegrationConnection(fixture.ctx, fixture.owner, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		disabled := fixture.execute(t, fixture.owner, command.SetConnectionEnabled, "health-disable", &fresh.Version, command.ConnectionInput{Ref: fresh.Ref, Enabled: false}).Connection
		if disabled == nil {
			t.Fatal("disabled SSH connection missing")
		}
		fixture.execute(t, fixture.owner, command.DeleteConnection, "health-delete", &disabled.Version, command.ConnectionInput{Ref: disabled.Ref})
		_, err = fixture.service.Execute(fixture.ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), Mutation: value.Mutation{IdempotencyKey: "ssh-health-stale-after-delete"}, Payload: command.IntegrationConnectionTestInput{TestRef: replacement.ConnectionTestRef, LeaseRef: replacement.LeaseRef, Fence: replacement.Fence, Generation: replacement.Generation, Success: true}})
		if err == nil {
			t.Fatal("deleted connection admitted old health completion")
		}
	})
	t.Run("ordinary lane cannot claim SSH and target disable closes lease", func(t *testing.T) {
		secret := fixture.createSecret(t, "lease-inspect", "SSH_COMPOSE", false)
		connection := fixture.connection(t, secret, "inspect target")
		_, health := fixture.healthClaim(t, connection, "inspect")
		if _, err := fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, health); err != nil {
			t.Fatal(err)
		}
		connected := fixture.execute(t, fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), command.CompleteConnectionTest, "inspect-connected", nil, command.IntegrationConnectionTestInput{TestRef: health.ConnectionTestRef, LeaseRef: health.LeaseRef, Fence: health.Fence, Generation: health.Generation, Success: true}).Connection
		if connected == nil || connected.State != "CONNECTED" {
			t.Fatal("SSH fixture health completion missing")
		}
		agent := createLifecycleAgent(t, fixture.ctx, fixture.service, fixture.owner, fixture.projectRef, "ssh-inspect-agent", "SSH fixture operator")
		granted := fixture.execute(t, fixture.owner, command.ChangeIntegrationGrant, "inspect-grant", &connected.Version, command.IntegrationGrantInput{ConnectionRef: connected.Ref, CapabilityKey: "ssh.compose.inspect", AgentRef: agent.Ref, Enabled: true, ApprovalPolicy: "NONE"}).Connection
		run := fixture.execute(t, fixture.owner, command.LaunchRun, "inspect-run", nil, command.LaunchRunInput{ProjectRef: fixture.projectRef, Title: "SSH fixture inspect", Task: "No remote effect", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
		runtimeWorker := fixture.principal(t, "runtime-controller", "platform.runtime.execution.claim")
		runtime := fixture.execute(t, runtimeWorker, command.ClaimExecution, "inspect-runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-runtime-fixture", Limit: 1}).RuntimeItems
		if run == nil || len(runtime) != 1 {
			t.Fatal("SSH runtime fixture missing")
		}
		invocation, err := fixture.service.ResolveIntegrationInvocation(fixture.ctx, runtimeWorker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connected.Ref, "capability_key": "ssh.compose.inspect", "idempotency_key": "ssh-inspect-effect"}, map[string]any{})
		if err != nil {
			t.Fatalf("resolve SSH inspect: %v", err)
		}
		ordinary, err := fixture.service.ClaimIntegrationInvocations(fixture.ctx, fixture.principal(t, "integration-gateway", "platform.runtime.integrations.claim"), "ordinary-fixture", 1)
		if err != nil || len(ordinary) != 0 {
			t.Fatal("ordinary lane claimed protected SSH work")
		}
		claims, err := fixture.service.ClaimSSHComposeInvocations(fixture.ctx, fixture.principal(t, "integration-gateway", "platform.runtime.ssh-compose.claim"), "ssh-fixture", 1)
		if err != nil || len(claims) != 1 {
			t.Fatalf("SSH lane claim failed: %v", err)
		}
		claim := claims[0]
		binding := port.SSHComposeLeaseBinding{InvocationRef: stringMap(invocation, "ref"), WorkloadInstance: "ssh-fixture", LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64)}
		binding.InvocationRef = stringMap(claim, "invocationRef")
		if _, err = fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, binding); err != nil {
			t.Fatalf("inspect projection: %v", err)
		}
		if _, err = fixture.service.RenewSSHComposeInvocation(fixture.ctx, fixture.principal(t, "integration-gateway", "platform.runtime.ssh-compose.renew"), binding); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("read-only inspect received deployment renewal: %v", err)
		}
		fixture.execute(t, fixture.owner, command.SetConnectionEnabled, "inspect-disable", &granted.Version, command.ConnectionInput{Ref: connected.Ref, Enabled: false})
		var state string
		var lease *string
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT state,lease_ref FROM control_plane.integration_invocations WHERE ref=$1`, binding.InvocationRef).Scan(&state, &lease); err != nil {
			t.Fatal(err)
		}
		if state != "FAILED" || lease != nil {
			t.Fatal("target disable left an inspect lease active")
		}
		if _, err = fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, broker, binding); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("disabled target retained projection: %v", err)
		}
	})
}
