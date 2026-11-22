package controlplaneclient

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// kubernetesOwnerMethodRejected is the fixed refusal for unrelated owner RPCs.
	// kubernetesOwnerMethodRejected задаёт fixed refusal посторонних owner RPC.
	kubernetesOwnerMethodRejected = "Kubernetes invocation owner method rejected"
	// kubernetesOwnerStreamRejected keeps this gateway port unary without exposing a raw channel.
	// kubernetesOwnerStreamRejected сохраняет unary gateway port без raw channel.
	kubernetesOwnerStreamRejected = "Kubernetes invocation owner stream rejected"
)

// ManagedKubernetesOwnerConnection exposes only gateway renewal, finalization, cancellation and metadata probes.
// It reuses the existing protected connection and never weakens its method/issuer authority checks.
// ManagedKubernetesOwnerConnection открывает лишь gateway renew/finalization/cancel/metadata probes.
// Использует existing protected connection, сохраняя method/issuer authority checks.
func (client *Client) ManagedKubernetesOwnerConnection() grpc.ClientConnInterface {
	return kubernetesInvocationOwnerConnection{client: client}
}

// kubernetesInvocationOwnerConnection excludes broker effect/credential methods before dispatch.
// kubernetesInvocationOwnerConnection исключает broker effect/credential methods до dispatch.
type kubernetesInvocationOwnerConnection struct{ client *Client }

// Invoke forwards one of eight fixed unary methods without rewriting the original binding or request.
// The protected interceptor still enforces the configured workload operation map and signed proof.
// Invoke передаёт один из восьми fixed unary methods без изменения original binding/request.
// Protected interceptor сохраняет workload operation map и signed proof checks.
func (connection kubernetesInvocationOwnerConnection) Invoke(ctx context.Context, method string, input, output any, options ...grpc.CallOption) error {
	if connection.client == nil || connection.client.protected == nil {
		return status.Error(codes.PermissionDenied, kubernetesOwnerMethodRejected)
	}
	switch method {
	case cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName,
		cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName,
		cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName,
		cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName,
		cp.ManagedKubernetesOwnerService_AcceptManagedKubernetesOutputFrame_FullMethodName,
		cp.ManagedKubernetesOwnerService_GetManagedKubernetesOutputFrame_FullMethodName,
		cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName,
		cp.ManagedKubernetesOwnerService_CheckManagedKubernetesOwnerReadiness_FullMethodName:
		return connection.client.protected.Invoke(ctx, method, input, output, options...)
	default:
		return status.Error(codes.PermissionDenied, kubernetesOwnerMethodRejected)
	}
}

// NewStream refuses every streaming call because the owner port carries only bounded unary metadata.
// NewStream отклоняет streaming: owner port передаёт лишь bounded unary metadata.
func (kubernetesInvocationOwnerConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.PermissionDenied, kubernetesOwnerStreamRejected)
}
