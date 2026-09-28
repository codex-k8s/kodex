package platform

import (
	"strings"
	"testing"
)

func TestOwnerGateSearchIncludesVisibleColumns(t *testing.T) {
	for _, visibleValue := range []string{
		"p.name",
		"root.title",
		"COALESCE(requester_agent.name,initiator.display_name)",
	} {
		if !strings.Contains(queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState, visibleValue) {
			t.Fatalf("owner gate search must include visible value %q", visibleValue)
		}
	}
}
