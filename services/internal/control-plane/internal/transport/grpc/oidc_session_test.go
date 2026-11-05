package grpc

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestOIDCSessionMethodsAuthenticateBeforePayload rejects missing transport identity before malformed input or any nil service can be used.
// TestOIDCSessionMethodsAuthenticateBeforePayload отклоняет отсутствующую transport identity до malformed input и обращения к nil service.
func TestOIDCSessionMethodsAuthenticateBeforePayload(t *testing.T) {
	var server *Server
	for _, invoke := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := server.ResolveOIDCSession(ctx, nil); return err },
		func(ctx context.Context) error { _, err := server.GetOIDCSessionClosure(ctx, nil); return err },
		func(ctx context.Context) error { _, err := server.RevokeOIDCSession(ctx, nil); return err },
	} {
		if status.Code(invoke(t.Context())) != codes.Unauthenticated {
			t.Fatal("self-session method inspected payload or owner before authentication")
		}
	}
}

// TestOIDCSessionContractHasNoAuthoritySelectors checks empty requests and exact generated method/operation pins across the domain and protected client registry.
// TestOIDCSessionContractHasNoAuthoritySelectors проверяет пустые requests и exact generated method/operation pins domain и protected client registry.
func TestOIDCSessionContractHasNoAuthoritySelectors(t *testing.T) {
	methods := cp.File_controlplane_v1_oidc_session_proto.Services().ByName("OIDCSessionService").Methods()
	if methods.Len() != 3 {
		t.Fatal("self-session service expanded outside its closed contract")
	}
	for index := range methods.Len() {
		if methods.Get(index).Input().Fields().Len() != 0 {
			t.Fatal("self-session request can select actor, tenant or revision")
		}
	}
	operations := controlplaneclient.ControlAPIGatewayOperations()
	for operation, method := range map[string]string{platformrepo.StandardOIDCResolveOperation: cp.OIDCSessionService_ResolveOIDCSession_FullMethodName,
		platformrepo.StandardOIDCClosureOperation: cp.OIDCSessionService_GetOIDCSessionClosure_FullMethodName, platformrepo.StandardOIDCRevokeOperation: cp.OIDCSessionService_RevokeOIDCSession_FullMethodName} {
		if operations[operation] != method {
			t.Fatal("self-session operation drifted from its exact generated method")
		}
	}
}
