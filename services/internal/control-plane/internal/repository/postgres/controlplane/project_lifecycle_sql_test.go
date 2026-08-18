package controlplane

import (
	"strings"
	"testing"
)

func TestProjectDeleteIgnoresOnlyTerminalWorkspaceMapping(t *testing.T) {
	required := []string{
		"kind = 'WORKSPACE_MATTERMOST_MAPPING'",
		"state = 'ARCHIVED'",
		"spec ->> 'mappingState' = 'UNLINKED'",
	}
	for _, fragment := range required {
		if !strings.Contains(sqlProjectHasLiveResources, fragment) {
			t.Fatalf("terminal Workspace Mattermost mapping exclusion is incomplete: %s", fragment)
		}
	}
	if !strings.Contains(sqlProjectHasLiveResources, "AND NOT (") ||
		!strings.Contains(sqlProjectHasLiveResources, "AND state <> 'DELETED'") {
		t.Fatal("project deletion no longer rejects other live child resources")
	}
}
