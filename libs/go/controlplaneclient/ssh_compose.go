package controlplaneclient

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SSHComposeCredentialConnection exposes only the registered projection method for its generated broker adapter.
// SSHComposeCredentialConnection предоставляет только registered projection method generated broker adapter.
func (client *Client) SSHComposeCredentialConnection() grpc.ClientConnInterface {
	return sshComposeCredentialConnection{client: client}
}

// sshComposeCredentialConnection rejects every unrelated method before reaching the protected channel.
// sshComposeCredentialConnection отклоняет посторонние methods до protected channel.
type sshComposeCredentialConnection struct{ client *Client }

// Invoke permits one fixed unary method; the protected interceptor still verifies application authority.
// Invoke допускает один fixed unary method; protected interceptor проверяет application authority.
func (connection sshComposeCredentialConnection) Invoke(ctx context.Context, method string, input, output any, options ...grpc.CallOption) error {
	if connection.client == nil || connection.client.protected == nil || method != "/secretbroker.v1.RuntimeCredentialProjectionService/ProjectSSHComposeCredential" {
		return status.Error(codes.PermissionDenied, "SSH credential method rejected")
	}
	return connection.client.protected.Invoke(ctx, method, input, output, options...)
}

// NewStream rejects streaming because the closed SSH projection is one bounded unary response.
// NewStream отклоняет streaming: SSH projection является bounded unary response.
func (connection sshComposeCredentialConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.PermissionDenied, "SSH credential stream rejected")
}
