package owner

import (
	"context"
	"testing"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"google.golang.org/grpc"
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

func (client readinessIntegrationClient) GetManagementDiagnostics(context.Context, *integrationgatewayv1.GetManagementDiagnosticsRequest, ...grpc.CallOption) (*integrationgatewayv1.GetManagementDiagnosticsResponse, error) {
	return client.response, nil
}

func TestCheckRejectsNonReadyOwnerPath(t *testing.T) {
	t.Parallel()
	client := &Client{
		Interaction: readinessInteractionClient{response: &interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse{Ready: true, AuthorityReady: true}},
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
