package app

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"sync/atomic"
)

// sshIdentityConsumer forwards health work and optionally owns finite invocation activities through the fixed broker-purpose path; the default Run remains health-only until qualified mixed composition.
// sshIdentityConsumer передаёт health work и при наличии owns finite invocation activities через fixed broker-purpose path; default Run остаётся health-only до qualified mixed composition.
type sshIdentityConsumer struct {
	closed     atomic.Bool
	activities *sshInvocationActivities
	adapter    *integration.Adapter
	broker     sb.ManagedSSHServiceClient
	instance   string
}

// ProbePersonal completes only the original personal owner test and never invokes generic TOKEN verification.
// ProbePersonal завершает лишь original personal owner test без generic TOKEN verification.
func (consumer *sshIdentityConsumer) ProbePersonal(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	return consumer.probe(ctx, claim)
}

// ProbeShared completes only the original shared owner test without selecting credentials or a different service subject.
// ProbeShared завершает лишь original shared owner test без выбора credentials либо другого service subject.
func (consumer *sshIdentityConsumer) ProbeShared(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	return consumer.probe(ctx, claim)
}

// probe maps a validated purpose receipt without interpreting authentication as effect authority or retrying an uncertain RPC.
// probe преобразует validated purpose receipt без вывода effect authority из authentication и без повторного uncertain RPC.
func (consumer *sshIdentityConsumer) probe(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	if consumer == nil || consumer.closed.Load() || consumer.adapter == nil || consumer.broker == nil {
		return brokerIdentityProbeReceipt{}, brokerIdentityWorkUnavailable
	}
	receipt, err := consumer.adapter.ProbeSSH(ctx, claim, consumer.instance, consumer.broker)
	return brokerIdentityProbeReceipt{TestRef: receipt.TestRef, State: receipt.State, Applied: receipt.Applied, Attempted: receipt.Attempted, Authenticated: receipt.Authenticated}, err
}

// ReserveClaim supplies invocation credit only from the actual factory-owned ActivityGate; health metadata alone never makes a runnable business lane.
// ReserveClaim выдаёт invocation credit лишь из actual factory-owned ActivityGate; одни health metadata не создают runnable business lane.
func (consumer *sshIdentityConsumer) ReserveClaim() (brokerIdentityClaimSlot, bool) {
	if consumer.activities == nil {
		return nil, false
	}
	return consumer.activities.reserve()
}

// Close closes new synchronous health admission; its actual request join remains the common worker's responsibility.
// Close закрывает новые synchronous health admissions; actual request join остаётся ответственностью common worker.
func (consumer *sshIdentityConsumer) Close() error {
	consumer.closed.Store(true)
	if consumer.activities != nil {
		return consumer.activities.close()
	}
	return nil
}

// Wait joins every configured asynchronous invocation while the common worker independently joins synchronous health.
// Wait ожидает каждую configured asynchronous invocation; common worker отдельно ожидает synchronous health.
func (consumer *sshIdentityConsumer) Wait(ctx context.Context) error {
	if !consumer.closed.Load() {
		return brokerIdentityWorkUnavailable
	}
	if consumer.activities != nil {
		return consumer.activities.wait(ctx)
	}
	return nil
}

// Joined reports closed admission and positive local invocation join, never remote process quiescence.
// Joined сообщает closed admission и positive local invocation join, не remote process quiescence.
func (consumer *sshIdentityConsumer) Joined() bool {
	return consumer.closed.Load() && (consumer.activities == nil || consumer.activities.gate.Joined())
}

// Failure signals unresolved asynchronous lifecycle failure; health-only construction has no producer and synchronous health belongs to the common worker.
// Failure сообщает unresolved asynchronous lifecycle failure; health-only construction не имеет producer, synchronous health принадлежит common worker.
func (consumer *sshIdentityConsumer) Failure() <-chan struct{} {
	if consumer.activities == nil {
		return nil
	}
	return consumer.activities.failure
}

// integrationBrokerOperations combines the registered purpose methods with the unchanged Compose projection; execution and reconciliation stay closed at capacity zero.
// integrationBrokerOperations объединяет registered purpose methods с прежней Compose projection; execution и reconciliation остаются закрытыми при capacity zero.
func integrationBrokerOperations(sshCapacity uint32) map[string]string {
	operations := integrationGatewayHealthBrokerOperations()
	operations["ssh.health.probe"] = controlplaneclient.SSHIdentityBrokerOperations()["ssh.health.probe"]
	if sshCapacity != 0 {
		registered := controlplaneclient.SSHIdentityBrokerOperations()
		operations["ssh.execute"] = registered["ssh.execute"]
		operations["ssh.reconcile"] = registered["ssh.reconcile"]
	}
	return operations
}
