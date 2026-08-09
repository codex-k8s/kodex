package owner

import (
	"context"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type readinessInteractionClient struct {
	interactiongatewayv1.MattermostTeamServiceClient
	response *interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse
}

func (client readinessInteractionClient) CheckReadiness(context.Context, *interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest, ...grpc.CallOption) (*interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse, error) {
	return client.response, nil
}

type readinessIntegrationClient struct {
	integrationgatewayv1.IntegrationManagementServiceClient
	response *integrationgatewayv1.GetManagementDiagnosticsResponse
}

type readinessBotClient struct {
	interactiongatewayv1.AgentMattermostBotIdentityServiceClient
	response *interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse
}

func (client readinessBotClient) ListAgentMattermostBotIdentities(context.Context, *interactiongatewayv1.ListAgentMattermostBotIdentitiesRequest, ...grpc.CallOption) (*interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse, error) {
	return client.response, nil
}

func (client readinessIntegrationClient) GetManagementDiagnostics(context.Context, *integrationgatewayv1.GetManagementDiagnosticsRequest, ...grpc.CallOption) (*integrationgatewayv1.GetManagementDiagnosticsResponse, error) {
	return client.response, nil
}

func TestCheckRejectsNonReadyOwnerPath(t *testing.T) {
	t.Parallel()
	client := &Client{
		Interaction: readinessInteractionClient{response: &interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse{Ready: true, AuthorityReady: true}},
		Bot:         readinessBotClient{response: &interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse{}},
		Integration: readinessIntegrationClient{response: &integrationgatewayv1.GetManagementDiagnosticsResponse{Status: "UNAVAILABLE"}},
	}
	if err := client.Check(context.Background()); err == nil {
		t.Fatal("non-ready integration owner path was accepted")
	}
	client.Integration = readinessIntegrationClient{response: &integrationgatewayv1.GetManagementDiagnosticsResponse{Status: "READY"}}
	if err := client.Check(context.Background()); err != nil {
		t.Fatalf("ready owner path was rejected: %v", err)
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
