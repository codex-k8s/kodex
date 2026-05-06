package project

import "fmt"

var (
	queryBranchRulesCreate                 = mustLoadQuery("branch_rules__create")
	queryBranchRulesGetByID                = mustLoadQuery("branch_rules__get_by_id")
	queryBranchRulesList                   = mustLoadQuery("branch_rules__list")
	queryBranchRulesUpdate                 = mustLoadQuery("branch_rules__update")
	queryCommandResultCreate               = mustLoadQuery("command_result__create")
	queryCommandResultGet                  = mustLoadQuery("command_result__get")
	queryDocumentationSourceCreate         = mustLoadQuery("documentation_source__create")
	queryDocumentationSourceGetByID        = mustLoadQuery("documentation_source__get_by_id")
	queryDocumentationSourceList           = mustLoadQuery("documentation_source__list")
	queryDocumentationSourceUpdate         = mustLoadQuery("documentation_source__update")
	queryOutboxEventClaim                  = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate                 = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed             = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanentlyFailed  = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished          = mustLoadQuery("outbox_event__mark_published")
	queryPlacementPolicyCreate             = mustLoadQuery("placement_policy__create")
	queryPlacementPolicyGetByID            = mustLoadQuery("placement_policy__get_by_id")
	queryPlacementPolicyList               = mustLoadQuery("placement_policy__list")
	queryPlacementPolicyUpdate             = mustLoadQuery("placement_policy__update")
	queryPolicyEditProposalCreate          = mustLoadQuery("policy_edit_proposal__create")
	queryPolicyEditProposalGetByID         = mustLoadQuery("policy_edit_proposal__get_by_id")
	queryPolicyOverrideCreate              = mustLoadQuery("policy_override__create")
	queryPolicyOverrideGetByID             = mustLoadQuery("policy_override__get_by_id")
	queryPolicyOverrideList                = mustLoadQuery("policy_override__list")
	queryProjectCreate                     = mustLoadQuery("project__create")
	queryProjectGetByID                    = mustLoadQuery("project__get_by_id")
	queryProjectList                       = mustLoadQuery("project__list")
	queryProjectUpdate                     = mustLoadQuery("project__update")
	queryReleaseLineCreate                 = mustLoadQuery("release_line__create")
	queryReleaseLineGetByID                = mustLoadQuery("release_line__get_by_id")
	queryReleaseLineList                   = mustLoadQuery("release_line__list")
	queryReleaseLineUpdate                 = mustLoadQuery("release_line__update")
	queryReleasePolicyCreate               = mustLoadQuery("release_policy__create")
	queryReleasePolicyGetByID              = mustLoadQuery("release_policy__get_by_id")
	queryReleasePolicyList                 = mustLoadQuery("release_policy__list")
	queryReleasePolicyUpdate               = mustLoadQuery("release_policy__update")
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
	queryServicesPolicyNextVersion         = mustLoadQuery("services_policy__next_version")
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
