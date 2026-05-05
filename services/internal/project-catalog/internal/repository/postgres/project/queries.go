package project

import "fmt"

var (
	queryBranchRulesGetByID                = mustLoadQuery("branch_rules__get_by_id")
	queryBranchRulesList                   = mustLoadQuery("branch_rules__list")
	queryBranchRulesUpsert                 = mustLoadQuery("branch_rules__upsert")
	queryCommandResultCreate               = mustLoadQuery("command_result__create")
	queryCommandResultGet                  = mustLoadQuery("command_result__get")
	queryDocumentationSourceGetByID        = mustLoadQuery("documentation_source__get_by_id")
	queryDocumentationSourceList           = mustLoadQuery("documentation_source__list")
	queryDocumentationSourceUpsert         = mustLoadQuery("documentation_source__upsert")
	queryOutboxEventClaim                  = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate                 = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed             = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanentlyFailed  = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished          = mustLoadQuery("outbox_event__mark_published")
	queryPlacementPolicyGetByID            = mustLoadQuery("placement_policy__get_by_id")
	queryPlacementPolicyList               = mustLoadQuery("placement_policy__list")
	queryPlacementPolicyUpsert             = mustLoadQuery("placement_policy__upsert")
	queryPolicyEditProposalCreate          = mustLoadQuery("policy_edit_proposal__create")
	queryPolicyOverrideUpsert              = mustLoadQuery("policy_override__upsert")
	queryProjectCreate                     = mustLoadQuery("project__create")
	queryProjectGetByID                    = mustLoadQuery("project__get_by_id")
	queryProjectList                       = mustLoadQuery("project__list")
	queryProjectUpdate                     = mustLoadQuery("project__update")
	queryReleaseLineGetByID                = mustLoadQuery("release_line__get_by_id")
	queryReleaseLineList                   = mustLoadQuery("release_line__list")
	queryReleaseLineUpsert                 = mustLoadQuery("release_line__upsert")
	queryReleasePolicyGetByID              = mustLoadQuery("release_policy__get_by_id")
	queryReleasePolicyList                 = mustLoadQuery("release_policy__list")
	queryReleasePolicyUpsert               = mustLoadQuery("release_policy__upsert")
	queryRepositoryCreate                  = mustLoadQuery("repository__create")
	queryRepositoryGetByID                 = mustLoadQuery("repository__get_by_id")
	queryRepositoryList                    = mustLoadQuery("repository__list")
	queryRepositoryUpdate                  = mustLoadQuery("repository__update")
	queryServiceDescriptorInsert           = mustLoadQuery("service_descriptor__insert")
	queryServiceDescriptorList             = mustLoadQuery("service_descriptor__list")
	queryServiceDescriptorMarkProjectStale = mustLoadQuery("service_descriptor__mark_project_stale")
	queryServicesPolicyGetActive           = mustLoadQuery("services_policy__get_active")
	queryServicesPolicyGetByID             = mustLoadQuery("services_policy__get_by_id")
	queryServicesPolicyInsert              = mustLoadQuery("services_policy__insert")
	queryWorkspaceCodeSourceList           = mustLoadQuery("workspace_code_source__list")
	queryWorkspaceDocumentationSourceList  = mustLoadQuery("workspace_documentation_source__list")
	queryWorkspaceGuidanceRefList          = mustLoadQuery("workspace_guidance_ref__list")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
