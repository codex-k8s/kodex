package controlplaneclient

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// kubernetesCaptureMethodRejected is the fixed refusal for unrelated owner methods.
	// kubernetesCaptureMethodRejected — fixed refusal посторонних owner methods.
	kubernetesCaptureMethodRejected = "Kubernetes artifact capture method rejected"
	// kubernetesCaptureStreamRejected keeps the task port unary and payload-free.
	// kubernetesCaptureStreamRejected сохраняет task port unary и без payload.
	kubernetesCaptureStreamRejected = "Kubernetes artifact capture stream rejected"
)

// ArtifactRetentionCaptureOperations registers only the artifact-retention task producer methods.
// It grants no invocation, effect, credential, frame or user-controlled artifact upload method.
// ArtifactRetentionCaptureOperations регистрирует только artifact-retention task producer methods.
// Не выдаёт invocation/effect/credential/frame либо user-controlled artifact upload method.
func ArtifactRetentionCaptureOperations() map[string]string {
	return map[string]string{
		"platform.artifacts.kubernetes-capture.finalize":  cp.ManagedKubernetesOwnerService_FinalizeManagedKubernetesCapture_FullMethodName,
		"platform.artifacts.kubernetes-capture.get":       cp.ManagedKubernetesOwnerService_GetManagedKubernetesCapture_FullMethodName,
		"platform.artifacts.kubernetes-capture.renew":     cp.ManagedKubernetesOwnerService_RenewManagedKubernetesCapture_FullMethodName,
		"platform.artifacts.kubernetes-capture.readiness": cp.ManagedKubernetesOwnerService_CheckManagedKubernetesCaptureReadiness_FullMethodName,
	}
}

// ManagedKubernetesCaptureConnection restricts the existing protected connection to fixed task RPCs.
// The protected interceptor still owns mTLS, application identity and exact issued method proof.
// ManagedKubernetesCaptureConnection ограничивает existing protected connection fixed task RPCs.
// Protected interceptor сохраняет mTLS/application identity/exact issued method proof.
func (client *Client) ManagedKubernetesCaptureConnection() grpc.ClientConnInterface {
	return kubernetesCaptureConnection{client: client}
}

// kubernetesCaptureConnection prevents a worker from opening unrelated methods or a raw stream.
// kubernetesCaptureConnection запрещает worker посторонние methods либо raw stream.
type kubernetesCaptureConnection struct{ client *Client }

// Invoke dispatches only a known unary artifact task method without rewriting its binding.
// Invoke передаёт только known unary artifact task method без изменения binding.
func (connection kubernetesCaptureConnection) Invoke(ctx context.Context, method string, input, output any, options ...grpc.CallOption) error {
	if connection.client == nil || connection.client.protected == nil {
		return status.Error(codes.PermissionDenied, kubernetesCaptureMethodRejected)
	}
	for _, registered := range ArtifactRetentionCaptureOperations() {
		if method == registered {
			return connection.client.protected.Invoke(ctx, method, input, output, options...)
		}
	}
	return status.Error(codes.PermissionDenied, kubernetesCaptureMethodRejected)
}

// NewStream refuses every streaming method because artifact task replies carry metadata only.
// NewStream отклоняет streaming methods: artifact task replies содержат только metadata.
func (kubernetesCaptureConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.PermissionDenied, kubernetesCaptureStreamRejected)
}
