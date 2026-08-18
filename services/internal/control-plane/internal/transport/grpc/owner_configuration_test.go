package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestWorkspaceMattermostMappingActionIsClosedAndNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action controlplanev1.WorkspaceMattermostMappingAction
		want   string
	}{
		{name: "bind", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND, want: "bind"},
		{name: "relink", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK, want: "relink"},
		{name: "unlink", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK, want: "unlink"},
		{name: "unspecified", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNSPECIFIED},
		{name: "unknown", action: controlplanev1.WorkspaceMattermostMappingAction(99)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := workspaceMattermostMappingAction(test.action); got != test.want {
				t.Fatalf("unexpected normalized action: got %q want %q", got, test.want)
			}
		})
	}
}
