package controlplaneclient

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// TestManagedKubernetesBrokerConnectionRetainsProtectedBoundary checks method admission and exact input/options at the existing interceptor, without opening a network connection.
// TestManagedKubernetesBrokerConnectionRetainsProtectedBoundary проверяет method admission и exact input/options на existing interceptor без открытия network connection.
func TestManagedKubernetesBrokerConnectionRetainsProtectedBoundary(t *testing.T) {
	methods := []string{
		"/secretbroker.v1.ManagedKubernetesService/ExecuteManagedKubernetesOperation",
		"/secretbroker.v1.ManagedKubernetesService/ExchangeManagedKubernetesStream",
		"/secretbroker.v1.ManagedKubernetesService/CancelManagedKubernetesOperation",
		"/secretbroker.v1.ManagedKubernetesService/CheckManagedKubernetesReadiness",
	}
	calls, observed := 0, ""
	input, output := new(int), new(int)
	option := grpc.MaxCallRecvMsgSize(128)
	channel, err := grpc.NewClient("passthrough:///unused.example.invalid:1", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(
		func(_ context.Context, method string, request, response any, _ *grpc.ClientConn, _ grpc.UnaryInvoker, options ...grpc.CallOption) error {
			calls++
			observed = method
			if request != input || response != output || len(options) != 1 || options[0] != option {
				t.Fatal("protected invocation arguments changed")
			}
			return status.Error(codes.Unavailable, "synthetic protected boundary")
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	connection := (&Client{protected: channel}).ManagedKubernetesBrokerConnection()
	if len(ManagedKubernetesBrokerOperations()) != len(methods) {
		t.Fatal("broker method profile changed without adapter qualification")
	}
	for index, method := range methods {
		if status.Code(connection.Invoke(t.Context(), method, input, output, option)) != codes.Unavailable || calls != index+1 || observed != method {
			t.Fatal("registered call bypassed or did not reach protected interceptor")
		}
	}
	for _, method := range []string{"", "/secretbroker.v1.RuntimeCredentialProjectionService/ProjectSSHComposeCredential", methods[0] + "/extra"} {
		if status.Code(connection.Invoke(t.Context(), method, input, output)) != codes.PermissionDenied || calls != len(methods) {
			t.Fatal("unregistered call reached protected channel")
		}
	}
	if stream, err := connection.NewStream(t.Context(), &grpc.StreamDesc{}, methods[1]); stream != nil || status.Code(err) != codes.PermissionDenied || calls != len(methods) {
		t.Fatal("generic stream bypassed bounded exchange")
	}
	for _, client := range []*Client{nil, {}} {
		if status.Code(client.ManagedKubernetesBrokerConnection().Invoke(t.Context(), methods[0], input, output)) != codes.PermissionDenied {
			t.Fatal("missing protected channel did not fail closed")
		}
	}
}
