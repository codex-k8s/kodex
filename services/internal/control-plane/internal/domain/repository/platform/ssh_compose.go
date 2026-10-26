package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHComposeLeaseBinding identifies one exact fenced owner invocation or health test.
// SSHComposeLeaseBinding обозначает exact fenced owner invocation либо health test.
type SSHComposeLeaseBinding struct {
	WorkloadInstance, InvocationRef, ConnectionTestRef, LeaseRef, Fence string
	Generation                                                          int64
}

// SSHComposeRepository extends only the protected SSH adapter owner boundary.
// SSHComposeRepository расширяет только owner boundary защищённого SSH adapter.
type SSHComposeRepository interface {
	// ClaimSSHComposeInvocations claims the independent SSH execution lane.
	// ClaimSSHComposeInvocations захватывает отдельную SSH очередь исполнения.
	ClaimSSHComposeInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// RenewSSHComposeInvocation rechecks the complete owner graph before extending the same lease.
	// RenewSSHComposeInvocation повторно проверяет owner graph до продления того же lease.
	RenewSSHComposeInvocation(context.Context, value.Principal, SSHComposeLeaseBinding) (map[string]any, error)
	// ResolveSSHComposeCredentialProjection exposes one descriptor only to secret-broker.
	// ResolveSSHComposeCredentialProjection выдаёт один descriptor только secret-broker.
	ResolveSSHComposeCredentialProjection(context.Context, value.Principal, SSHComposeLeaseBinding) (map[string]any, error)
}
