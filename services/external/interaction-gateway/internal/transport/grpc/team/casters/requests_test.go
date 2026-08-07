package casters

import (
	"strings"
	"testing"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
)

func TestCreateRequestRejectsUnboundedOrInvalidIdentity(t *testing.T) {
	for _, request := range []*interactiongatewayv1.CreateMattermostTeamRequest{
		nil,
		{DisplayName: strings.Repeat("x", 257), IdempotencyKey: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{DisplayName: "Owner Workspace", IdempotencyKey: "caller-controlled-text"},
		{DisplayName: "Owner Workspace", IdempotencyKey: "00000000-0000-0000-0000-000000000000"},
	} {
		if _, _, _, err := CreateRequest(request); err == nil {
			t.Fatalf("invalid request was accepted: %#v", request)
		}
	}
}

func TestListAndReadbackRequireBoundedOpaqueReferences(t *testing.T) {
	if _, _, err := ListRequest(&interactiongatewayv1.ListMattermostTeamsRequest{PageSize: 101}); err == nil {
		t.Fatal("unbounded catalog page was accepted")
	}
	if _, err := ProviderReadbackRequest(&interactiongatewayv1.GetMattermostTeamProviderReadbackRequest{
		Selector: "raw-provider-team-id",
	}); err == nil {
		t.Fatal("raw provider reference was accepted")
	}
}
