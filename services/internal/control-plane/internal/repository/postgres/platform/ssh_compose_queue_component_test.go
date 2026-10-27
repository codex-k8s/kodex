package platform

import (
	"bytes"
	_ "embed"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// querySSHAdmissionQueueProof reads exact invocation pins and bounded event cardinality only.
// It never creates business state, changes time or reads credential values.
// querySSHAdmissionQueueProof читает exact invocation pins и bounded event cardinality;
// business state, время и значения credentials не изменяются и не читаются.
//
//go:embed testdata/sql/ssh_admission_queue_proof.sql
var querySSHAdmissionQueueProof string

// sshAdmissionQueueProof keeps immutable execution evidence separate from admission metadata.
// sshAdmissionQueueProof отделяет immutable execution evidence от admission metadata.
type sshAdmissionQueueProof struct {
	state, safeError, receipt, reason string
	snapshot                          []byte
	generation, audits, events        int64
	blocked                           bool
}

// admissionQueueProof returns one authoritative read of an invocation without logging its snapshot.
// admissionQueueProof возвращает authoritative read invocation без вывода snapshot в лог.
func (fixture *sshComposeComponentFixture) admissionQueueProof(t *testing.T, ref string) sshAdmissionQueueProof {
	t.Helper()
	var proof sshAdmissionQueueProof
	if err := fixture.pool.QueryRow(fixture.ctx, querySSHAdmissionQueueProof, ref).Scan(&proof.state, &proof.safeError, &proof.receipt, &proof.snapshot, &proof.generation, &proof.blocked, &proof.reason, &proof.audits, &proof.events); err != nil {
		t.Fatal("read SSH admission queue proof")
	}
	return proof
}

// TestSSHComposeAdmissionQueueComponent proves permanent-denial progress using native commands.
// No SSH, provider request, business SQL mutation or artificial clock is used.
// TestSSHComposeAdmissionQueueComponent проверяет progress permanent denial штатными командами,
// без SSH, provider request, business SQL mutations либо искусственного времени.
func TestSSHComposeAdmissionQueueComponent(t *testing.T) {
	for _, mode := range []string{"rotated-secret-queue", "source-acl-queue", "target-acl-prefix"} {
		t.Run(mode, func(t *testing.T) { testSSHComposeWorkflow(t, mode) })
	}
}

// queueTarget publishes an independent healthy target and READ grant before the model revision.
// A distinct public address prevents the target reservation from aliasing the original target.
// queueTarget публикует независимый healthy target и READ grant до model revision;
// другое public address исключает совпадение reservation с исходным target.
func (fixture *sshComposeComponentFixture) queueTarget(t *testing.T, agentRef string) entity.IntegrationConnection {
	t.Helper()
	secret := fixture.createSecret(t, "queue-independent", "SSH_COMPOSE", false)
	configuration := map[string]any{}
	for key, val := range fixture.configuration(secret) {
		configuration[key] = val
	}
	configuration["address"] = "8.8.4.4"
	created := fixture.execute(t, fixture.owner, command.CreateConnection, "queue-independent", nil, command.ConnectionInput{DefinitionKey: "ssh-compose", Name: "Independent queue target", PublicConfiguration: configuration}).Connection
	if created == nil {
		t.Fatal("independent SSH target missing")
	}
	_, binding := fixture.healthClaim(t, *created, "queue-independent")
	connected := fixture.execute(t, fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), command.CompleteConnectionTest, "queue-independent-connected", nil, command.IntegrationConnectionTestInput{TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}).Connection
	if connected == nil || connected.State != "CONNECTED" {
		t.Fatal("independent SSH target not healthy")
	}
	granted := fixture.execute(t, fixture.owner, command.ChangeIntegrationGrant, "queue-independent-grant", &connected.Version, command.IntegrationGrantInput{ConnectionRef: connected.Ref, AgentRef: agentRef, CapabilityKey: "ssh.compose.inspect", ApprovalPolicy: "NONE", Enabled: true}).Connection
	if granted == nil {
		t.Fatal("independent SSH grant missing")
	}
	return *granted
}

// assertAdmissionQueue proves durable prefix progress, immutable UNKNOWN and explicit cancellation.
// The independent inspect has no Git source, so source ACL revocation cannot accidentally deny it.
// Model leases are renewed natively while preparing a bounded prefix; no expired lease is reused.
// assertAdmissionQueue проверяет durable prefix progress, immutable UNKNOWN и явную отмену.
// Независимый inspect не читает Git source и не зависит от отозванного source ACL.
// Model lease продлевается штатно при подготовке bounded prefix; expired lease не переиспользуется.
func (fixture *sshComposeComponentFixture) assertAdmissionQueue(t *testing.T, f *workflowRecoveryRegression, mode string, secret entity.RuntimeSecret,
	connection, next entity.IntegrationConnection, source entity.AccessBinding, targets []entity.AccessBinding, execution, deployed map[string]any) {
	t.Helper()
	runtime := fixture.principal(t, "runtime-controller", "platform.runtime.execution.claim")
	gateway := fixture.principal(t, "integration-gateway", "platform.runtime.integrations.claim")
	sshWorker := fixture.principal(t, "integration-gateway", "platform.runtime.ssh-compose.claim")
	renew := func(key string) {
		fixture.execute(t, runtime, command.RenewExecution, key, nil, command.LeaseInput{LeaseRef: stringMap(execution, "leaseRef"), Fence: stringMap(execution, "fence"), Generation: runtimeRevisionMapInt64(execution, "generation")})
	}
	prepare := func(target, key string) string {
		value, err := fixture.service.ResolveIntegrationInvocation(fixture.ctx, runtime, map[string]string{"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": target, "capability_key": "ssh.compose.inspect", "idempotency_key": key}, map[string]any{})
		if err != nil || stringMap(value, "invocationRef") == "" {
			t.Fatalf("queue inspect admission failed: %s", completionOutcomeClass(err))
		}
		return stringMap(value, "invocationRef")
	}
	oldRef := stringMap(deployed, "invocationRef")
	poisoned := []string{oldRef}
	if mode == "rotated-secret-queue" {
		completeRuntimeSecretRotate(t, fixture.ctx, fixture.service, runtimeSecretOwnerPrincipal(fixture.owner, "secret.rotate"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.consume"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.complete"), secret, runtimeSecretHashB, "queue-rotate")
		fresh, err := fixture.service.GetIntegrationConnection(fixture.ctx, fixture.owner, connection.Ref)
		if err != nil {
			t.Fatal("read target after secret rotation")
		}
		_, health := fixture.healthClaim(t, fresh, "queue-reconnect")
		ready := fixture.execute(t, fixture.principal(t, "integration-gateway", "platform.runtime.integration-tests.claim"), command.CompleteConnectionTest, "queue-reconnected", nil, command.IntegrationConnectionTestInput{TestRef: health.ConnectionTestRef, LeaseRef: health.LeaseRef, Fence: health.Fence, Generation: health.Generation, Success: true}).Connection
		if ready == nil || ready.State != "CONNECTED" {
			t.Fatal("rotated target reconnection missing")
		}
	} else {
		completeSSHFixtureInvocation(t, fixture, gateway, deployed, "", "", true)
	}
	renew("queue-origin-renew")
	if mode == "target-acl-prefix" {
		for index := 1; index < sshComposeAdmissionScanLimit; index++ {
			if index%8 == 0 {
				renew("queue-origin-renew-" + strconv.Itoa(index))
			}
			poisoned = append(poisoned, prepare(connection.Ref, "queue-denied-"+strconv.Itoa(index)))
		}
	}
	tail := prepare(next.Ref, "queue-eligible-tail")
	var revoked entity.AccessBinding
	if mode == "source-acl-queue" {
		revoked = source
	} else if mode == "target-acl-prefix" {
		if len(targets) != 2 {
			t.Fatal("exact target actor bindings missing")
		}
		revoked = targets[0]
	}
	if revoked.Ref != "" {
		fixture.execute(t, fixture.owner, command.RevokeAccessBinding, "queue-revoke", &revoked.Version, command.AccessBindingInput{BindingRef: revoked.Ref})
	}
	before := make(map[string]sshAdmissionQueueProof, len(poisoned))
	for _, ref := range poisoned {
		before[ref] = fixture.admissionQueueProof(t, ref)
	}
	if before[oldRef].state != "UNKNOWN_OUTCOME" || before[oldRef].receipt != "" || before[oldRef].blocked {
		t.Fatal("queue baseline lost unresolved effect")
	}
	claims, err := fixture.service.ClaimSSHComposeInvocations(fixture.ctx, sshWorker, "ssh-workflow-fixture", 1)
	if err != nil {
		t.Fatalf("permanent denial rolled back queue progress: %s", completionOutcomeClass(err))
	}
	if mode == "target-acl-prefix" {
		if len(claims) != 0 {
			t.Fatal("bounded all-denied batch unexpectedly claimed tail")
		}
	} else if len(claims) != 1 || stringMap(claims[0], "invocationRef") != tail {
		t.Fatal("old unresolved admission blocked independent target")
	}
	for _, ref := range poisoned {
		old, after := before[ref], fixture.admissionQueueProof(t, ref)
		if !after.blocked || after.reason != entity.IntegrationAdmissionCurrentDenied || after.state != old.state || after.safeError != old.safeError || after.receipt != old.receipt || after.generation != old.generation || !bytes.Equal(after.snapshot, old.snapshot) || after.audits != 1 || after.events != 1 {
			t.Fatal("durable denial changed execution outcome or omitted exact audit/event")
		}
		read, readErr := fixture.service.GetIntegrationInvocation(fixture.ctx, gateway, ref)
		if readErr != nil || read["admissionBlocked"] != true || stringMap(read, "admissionBlockReason") != entity.IntegrationAdmissionCurrentDenied || stringMap(read, "state") != old.state || stringMap(read, "safeErrorCode") != old.safeError || stringMap(read, "effectReceiptRef") != old.receipt {
			t.Fatal("blocked owner read misrepresented unresolved effect")
		}
	}
	if revoked.Ref != "" {
		receiptAccessBinding(t, fixture.ctx, fixture.service, fixture.owner, revoked.Subject.Ref, "queue-explicit-access-restored", []string{"integration.manage"}, revoked.Scope)
	}
	if mode == "target-acl-prefix" {
		claims, err = fixture.service.ClaimSSHComposeInvocations(fixture.ctx, sshWorker, "ssh-workflow-fixture", 1)
		if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != tail {
			t.Fatal("committed denied prefix did not release eligible tail")
		}
	}
	claims, err = fixture.service.ClaimSSHComposeInvocations(fixture.ctx, sshWorker, "ssh-workflow-fixture", 1)
	if err != nil || len(claims) != 0 {
		t.Fatal("restored access automatically unblocked pinned invocation")
	}
	root, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil {
		t.Fatal("read root before blocked-admission cancellation")
	}
	f.execute(command.CancelRun, "queue-owner-cancel", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Cancel blocked SSH execution"})
	after := fixture.admissionQueueProof(t, oldRef)
	if after.state != "UNKNOWN_OUTCOME" || after.receipt != "" || !after.blocked || after.audits != 1 || after.events != 1 {
		t.Fatal("owner cancellation resolved or erased unknown provider outcome")
	}
}
