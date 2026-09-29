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

func TestRunCatalogSearchIncludesVisibleColumns(t *testing.T) {
	statements := map[string]string{
		"page":  queryQueriesListrunsSelectRunsOrganizationIdRefProjectId,
		"total": queryCatalogRunsCount,
	}
	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			for _, visibleValue := range []string{
				"activitySummary",
				"r.result_summary",
				"sub.display_name",
				"COALESCE(a.name,w.name,sa.name,r.target_ref)",
			} {
				if !strings.Contains(statement, visibleValue) {
					t.Fatalf("run catalog search must include visible value %q", visibleValue)
				}
			}
		})
	}
}
