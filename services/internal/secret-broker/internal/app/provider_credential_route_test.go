package app

import (
	"context"
	"errors"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
)

func TestRouteProviderCredentialUnaryProtectsExactService(t *testing.T) {
	protectedCalls := 0
	handlerCalls := 0
	interceptor := routeProviderCredentialUnary(func(
		_ context.Context,
		_ any,
		_ *grpc.UnaryServerInfo,
		_ grpc.UnaryHandler,
	) (any, error) {
		protectedCalls++
		return "protected", nil
	})
	handler := func(context.Context, any) (any, error) {
		handlerCalls++
		return "handler", nil
	}
	providerMethods := []string{
		controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
		controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
		controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
		controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
		controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName,
		controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName,
	}
	for _, method := range providerMethods {
		result, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
		if err != nil || result != "protected" {
			t.Fatalf("provider method %q bypassed authorization verifier", method)
		}
	}
	result, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: secretbrokerv1.SecretBrokerService_CheckReadiness_FullMethodName,
	}, handler)
	if err != nil || result != "handler" || protectedCalls != len(providerMethods) || handlerCalls != 1 {
		t.Fatal("provider route protection expanded beyond the exact service")
	}
}

type verifierReadinessClient struct {
	ready bool
	err   error
}

func (client verifierReadinessClient) VerifyAuthorizationContext(
	context.Context,
	*internalrpcauthorityv1.VerifyAuthorizationContextRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.VerifyAuthorizationContextResponse, error) {
	return nil, errors.New("unexpected authorization verification")
}

func (client verifierReadinessClient) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{Ready: client.ready}, client.err
}

func TestProviderVerifierReadinessFailsClosed(t *testing.T) {
	t.Parallel()
	if err := (providerVerifierReadiness{client: verifierReadinessClient{ready: true}}).Check(context.Background()); err != nil {
		t.Fatalf("ready verifier was rejected: %v", err)
	}
	for _, client := range []verifierReadinessClient{
		{},
		{ready: true, err: errors.New("readiness failed")},
	} {
		if err := (providerVerifierReadiness{client: client}).Check(context.Background()); err == nil {
			t.Fatal("unready verifier was accepted")
		}
	}
}
