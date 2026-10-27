package platform

import (
	"strings"
	"testing"
)

// TestProjectMembershipSearchIncludesVisibleProjectName checks the membership SQL contains visible
// project-name search; it does not execute or authorize the query.
// TestProjectMembershipSearchIncludesVisibleProjectName проверяет наличие поиска по отображаемому
// имени проекта в SQL участников; не исполняет запрос и не проверяет его авторизацию.
func TestProjectMembershipSearchIncludesVisibleProjectName(t *testing.T) {
	if !strings.Contains(queryProjectMembershipList, "project.name ILIKE") {
		t.Fatal("project membership search must include the visible project name")
	}
}

// TestOrganizationCatalogSearchIncludesVisibleProjectName checks agent, workflow, schedule,
// environment and secret SQL references the visible project name, without running PostgreSQL
// search.
// TestOrganizationCatalogSearchIncludesVisibleProjectName проверяет наличие отображаемого имени
// проекта в SQL агентов, workflow, расписаний, окружений и секретов без выполнения поиска в
// PostgreSQL.
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

// TestRunCatalogSearchIncludesVisibleColumns checks both page and total SQL retain activity/result
// summaries, initiator and target-name search expressions.
// TestRunCatalogSearchIncludesVisibleColumns проверяет сохранение выражений поиска по сводкам
// активности и результата, инициатору и имени цели в SQL страницы и общего количества.
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
