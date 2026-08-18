package httptransport

import (
	"testing"
	"time"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMattermostMutationAcceptsCompleteTerminalReadback(t *testing.T) {
	t.Parallel()

	now := timestamppb.New(time.Date(2026, 8, 18, 4, 0, 0, 0, time.UTC))
	team := &interactiongatewayv1.MattermostTeamView{
		Selector: "11111111-1111-4111-8111-111111111111", DisplayName: "Owner Workspace",
		Slug: "owner-workspace", Status: interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_ACTIVE,
		ProviderSnapshotSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreatedAt:              now, UpdatedAt: now, ObservedAt: now,
	}
	binding := &interactiongatewayv1.WorkspaceMattermostTeamBindingView{
		MappingRef: "22222222-2222-4222-8222-222222222222", MappingVersion: 1,
		MappingGeneration: 1, State: interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_BOUND,
		Team: team, ProviderEffectVersion: 1, ProviderEffectGeneration: 1,
		ProviderObservedAt: now, UpdatedAt: now,
	}
	operation := &interactiongatewayv1.WorkspaceMattermostMappingOperationView{
		OperationId: "33333333-3333-4333-8333-333333333333",
		Action:      interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND,
		State:       interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_BOUND,
		CreatedAt:   now, UpdatedAt: now, Result: binding,
	}

	result, err := mattermostMutation(binding, operation)
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding == nil || result.Operation.Result == nil ||
		result.Operation.Result.Team.Selector != team.GetSelector() {
		t.Fatalf("terminal Mattermost readback is incomplete: %#v", result)
	}
}
