package controlplaneclient

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SSHIdentityBrokerConnection exposes only the exact registered SSH health, original-claim execution and reconciliation operations; generic streams and credential projection stay closed.
// SSHIdentityBrokerConnection открывает лишь exact registered SSH health, original-claim execution и reconciliation operations без generic streams либо credential projection.
func (client *Client) SSHIdentityBrokerConnection() grpc.ClientConnInterface {
	return sshIdentityBrokerConnection{client: client}
}

// sshIdentityBrokerConnection is the closed generated-client adapter over the existing authenticated connection.
// sshIdentityBrokerConnection является closed generated-client adapter существующего authenticated connection.
type sshIdentityBrokerConnection struct{ client *Client }

// Invoke preserves existing interceptors/options for the exact registered operation; nil and unregistered routes fail before I/O.
// Invoke сохраняет existing interceptors/options для exact registered operation; nil и unregistered routes отклоняются до I/O.
func (connection sshIdentityBrokerConnection) Invoke(ctx context.Context, method string, input, output any, options ...grpc.CallOption) error {
	if connection.client != nil && connection.client.protected != nil {
		for _, registered := range SSHIdentityBrokerOperations() {
			if method == registered {
				return connection.client.protected.Invoke(ctx, method, input, output, options...)
			}
		}
	}
	return status.Error(codes.PermissionDenied, "SSH broker method rejected")
}

// NewStream rejects arbitrary streaming because the finite SSH purpose protocol uses bounded unary operations.
// NewStream отклоняет arbitrary streaming: finite SSH purpose protocol использует bounded unary operations.
func (sshIdentityBrokerConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.PermissionDenied, "SSH broker stream rejected")
}
