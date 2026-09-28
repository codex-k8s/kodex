package platform

import (
	"strings"
	"testing"
)

func TestProjectMembershipSearchIncludesVisibleProjectName(t *testing.T) {
	if !strings.Contains(queryProjectMembershipList, "project.name ILIKE") {
		t.Fatal("project membership search must include the visible project name")
	}
}

func TestOrganizationCatalogSearchIncludesVisibleProjectName(t *testing.T) {
	queries := map[string]string{
		"agents":       queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId,
		"workflows":    queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId,
		"automations":  queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId,
		"environments": queryRuntimeConfigurationListEnvironments,
		"secrets":      queryRuntimeSecretsList,
	}
	for catalog, statement := range queries {
		t.Run(catalog, func(t *testing.T) {
			if !strings.Contains(statement, "project.name") && !strings.Contains(statement, "p.name") {
				t.Fatal("catalog search must include the visible project name")
			}
		})
	}
}
