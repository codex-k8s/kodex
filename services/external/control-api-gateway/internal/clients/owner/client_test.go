package owner

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type ownerProofFailure struct{ err error }

func (failure ownerProofFailure) AuthorityProof(
	context.Context,
	string,
	string,
) (string, string, error) {
	return "", "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d", failure.err
}

func TestWaitForReadyObservesTransportStateWithoutBusinessRPC(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForReady(ctx, connection); err != nil {
		t.Fatalf("ready transport was rejected: %v", err)
	}
}

func TestBotOperationRegistryUsesAuthorityPolicyIdentifiers(t *testing.T) {
	operations := interactionOperations()
	tests := map[string]string{
		interactiongatewayv1.AgentMattermostBotIdentityService_ListAgentMattermostBotIdentities_FullMethodName:              "interaction.agent-bot.catalog.read",
		interactiongatewayv1.AgentMattermostBotIdentityService_CreateAndBindAgentMattermostBotIdentity_FullMethodName:       "interaction.agent-bot.create-and-bind",
		interactiongatewayv1.AgentMattermostBotIdentityService_BindAgentMattermostBotIdentity_FullMethodName:                "interaction.agent-bot.bind",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentity_FullMethodName:                 "interaction.agent-bot.get",
		interactiongatewayv1.AgentMattermostBotIdentityService_RebindAgentMattermostBotIdentity_FullMethodName:              "interaction.agent-bot.rebind",
		interactiongatewayv1.AgentMattermostBotIdentityService_RevokeAgentMattermostBotIdentity_FullMethodName:              "interaction.agent-bot.revoke",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityOperation_FullMethodName:        "interaction.agent-bot.operation.get",
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityProviderReadback_FullMethodName: "interaction.agent-bot.provider.readback",
	}
	for method, expected := range tests {
		if operation, ok := operations.OperationID(method); !ok || operation != expected {
			t.Fatalf("operation mapping %s = %q, ok=%v", method, operation, ok)
		}
	}
}

func TestOwnerProofOperationsKeepOnlyHealthGlobal(t *testing.T) {
	t.Parallel()

	proofs := ProofOperations()
	projectRequired := ProjectRequiredOperations()
	if len(proofs) != 42 || len(projectRequired) != 40 {
		t.Fatalf("owner proof profile is incomplete: proofs=%d project=%d", len(proofs), len(projectRequired))
	}
	for operationID := range proofs {
		_, globalHealth := map[string]struct{}{
			"interaction.team.readiness":             {},
			"integration.management.diagnostics.get": {},
		}[operationID]
		_, projectScoped := projectRequired[operationID]
		if projectScoped == globalHealth {
			t.Fatalf("owner operation is not project-scoped: %s", operationID)
		}
	}
}

func TestBareOwnerErrorsAreNormalizedOnlyForExactLegacyProfiles(t *testing.T) {
	integrationMethod := integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName
	detail := normalizeBareError(RPCSourceIntegration, integrationMethod, status.Error(codes.NotFound, "private provider payload"))
	if detail == nil || detail.GetReason() != controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND || detail.GetCode() != "NOT_FOUND" || detail.GetRetryable() {
		t.Fatalf("integration error detail was not normalized: %#v", detail)
	}
	teamMethod := interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName
	detail = normalizeBareError(RPCSourceInteraction, teamMethod, status.Error(codes.FailedPrecondition, "private team payload"))
	if detail == nil || detail.GetReason() != controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT || detail.GetCode() != "STATE_CONFLICT" || detail.GetRetryable() {
		t.Fatalf("interaction Team error detail was not normalized: %#v", detail)
	}
	botMethod := interactiongatewayv1.AgentMattermostBotIdentityService_RebindAgentMattermostBotIdentity_FullMethodName
	if detail := normalizeBareError(RPCSourceInteraction, botMethod, status.Error(codes.Unavailable, "private bot payload")); detail != nil {
		t.Fatalf("typed bot profile accepted a bare status: %#v", detail)
	}
	if (&DownstreamError{Source: RPCSourceIntegration, Method: "/unknown.Service/Get", Err: status.Error(codes.NotFound, "private")}).Valid() {
		t.Fatal("unknown downstream method was accepted")
	}
}

func TestOwnerSourceInterceptorNormalizesLocalAuthorityFailure(t *testing.T) {
	t.Parallel()

	method := integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName
	issuer := authorityclient.IssuerUnaryClientInterceptor(nil, integrationOperations(), ownerProofFailure{
		err: status.Error(codes.Unavailable, "private resolver failure"),
	})
	err := sourceErrorInterceptor(RPCSourceIntegration)(context.Background(), method, nil, nil, nil,
		func(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, options ...grpc.CallOption) error {
			return issuer(ctx, method, request, reply, connection,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
					t.Fatal("downstream RPC was called")
					return nil
				}, options...)
		},
	)
	var downstream *DownstreamError
	if !errors.As(err, &downstream) || downstream.NormalizedDetail == nil ||
		downstream.NormalizedDetail.GetReason() != controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE ||
		downstream.NormalizedDetail.GetCode() != "UNAVAILABLE" || !downstream.NormalizedDetail.GetRetryable() {
		t.Fatalf("local authority failure was not normalized: %#v", err)
	}
}
