package app

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// newSSHConfiguredConsumer keeps health independent from execution capacity, and exposes the real activity consumer only for an explicitly enabled complete source profile. The common protected delivery probe still runs before Listen or Claim.
// newSSHConfiguredConsumer сохраняет health независимо от execution capacity и выдаёт real activity consumer лишь явно включённому полному source profile. Общий protected delivery probe остаётся до Listen либо Claim.
func newSSHConfiguredConsumer(lifecycle context.Context, config Config, adapter *integration.Adapter, broker sb.ManagedSSHServiceClient) (brokerIdentityConsumer, error) {
	if lifecycle == nil || lifecycle.Err() != nil || adapter == nil || broker == nil || config.SSHInvocationCapacity > 4 {
		return nil, brokerIdentityWorkUnavailable
	}
	if config.SSHInvocationCapacity == 0 {
		return &sshIdentityConsumer{instance: config.InstanceID, adapter: adapter, broker: broker}, nil
	}
	registered, known := integrationpackage.Adapter(string(integrationpackage.AdapterSSHTarget))
	if !known || registered.Readiness != integrationpackage.ReadinessReady {
		return nil, brokerIdentityWorkUnavailable
	}
	return newSSHExecutionConsumer(lifecycle, config.InstanceID, int(config.SSHInvocationCapacity), adapter, broker)
}
