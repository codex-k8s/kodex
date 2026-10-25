package platform

import (
	"strings"
	"testing"
)

// TestOwnerGateSearchIncludesVisibleColumns checks gate-search SQL references project name, root
// title and requester/initiator display identity without executing the search.
// TestOwnerGateSearchIncludesVisibleColumns проверяет ссылки на имя проекта, заголовок корня и
// отображаемую идентичность запросившего либо инициатора в SQL поиска подтверждений без исполнения
// поиска.
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
