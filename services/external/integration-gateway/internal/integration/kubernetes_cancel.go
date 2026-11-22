package integration

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// CancelKubernetesInvocation asks the authoritative owner to close exactly the original claim without a caller-selected outcome, target or credential. It performs one bounded RPC; a lost acknowledgement needs separate metadata readback and never proves a joined broker transport.
// CancelKubernetesInvocation просит authoritative owner закрыть ровно original claim без caller-selected outcome, target либо credential. Выполняет один bounded RPC; lost acknowledgement требует отдельного metadata readback и не доказывает joined broker transport.
func CancelKubernetesInvocation(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient) (*cp.ManagedKubernetesInvocationReceipt, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	call, cancel := context.WithTimeout(ctx, kubernetesOwnerTimeout)
	defer cancel()
	if call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := owner.CancelManagedKubernetesInvocation(call, &cp.CancelManagedKubernetesInvocationRequest{Binding: wire})
	if err != nil || call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInvocationReceipt(response.Receipt, binding) || response.Receipt.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return proto.Clone(response.Receipt).(*cp.ManagedKubernetesInvocationReceipt), nil
}
