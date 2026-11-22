package controlplaneclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ManagedKubernetesBrokerConnection exposes the registered bounded broker methods through the existing protected connection.
// It creates no channel or authority and never exposes credential-projection methods to the generated Kubernetes client.
// ManagedKubernetesBrokerConnection открывает registered bounded broker methods через существующий protected connection.
// Не создаёт channel либо authority и не открывает credential-projection methods generated Kubernetes client.
func (client *Client) ManagedKubernetesBrokerConnection() grpc.ClientConnInterface {
	return managedKubernetesBrokerConnection{client: client}
}

// managedKubernetesBrokerConnection limits a generated adapter to the fixed purpose registry.
// managedKubernetesBrokerConnection ограничивает generated adapter fixed purpose registry.
type managedKubernetesBrokerConnection struct{ client *Client }

// Invoke delegates admitted unary methods unchanged, retaining all existing authorization interceptors.
// Invoke передаёт admitted unary methods без изменений, сохраняя все существующие authorization interceptors.
func (connection managedKubernetesBrokerConnection) Invoke(ctx context.Context, method string, input, output any, options ...grpc.CallOption) error {
	if connection.client != nil && connection.client.protected != nil {
		for _, registered := range ManagedKubernetesBrokerOperations() {
			if method == registered {
				return connection.client.protected.Invoke(ctx, method, input, output, options...)
			}
		}
	}
	return status.Error(codes.PermissionDenied, "managed Kubernetes broker method rejected")
}

// NewStream rejects generic gRPC streams; managed Kubernetes frames use the bounded registered exchange RPC.
// NewStream отклоняет generic gRPC streams; managed Kubernetes frames используют bounded registered exchange RPC.
func (connection managedKubernetesBrokerConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.PermissionDenied, "managed Kubernetes broker stream rejected")
}
