package project

import "fmt"

var (
	queryBranchRulesCreate                            string
	queryBranchRulesGetByID                           string
	queryBranchRulesList                              string
	queryBranchRulesUpdate                            string
	queryCommandResultCreate                          string
	queryCommandResultGet                             string
	queryDocumentationSourceCreate                    string
	queryDocumentationSourceGetByID                   string
	queryDocumentationSourceList                      string
	queryDocumentationSourceMarkPolicyManagedDisabled string
	queryDocumentationSourceUpdate                    string
	queryDocumentationSourceUpsertPolicy              string
	queryOnboardingSignalReconciliationUpsert         string
	queryOutboxEventClaim                             string
	queryOutboxEventCreate                            string
	queryOutboxEventMarkFailed                        string
	queryOutboxEventMarkPermanentlyFailed             string
	queryOutboxEventMarkPublished                     string
	queryPlacementPolicyCreate                        string
	queryPlacementPolicyGetByID                       string
	queryPlacementPolicyList                          string
	queryPlacementPolicyUpdate                        string
	queryPolicyEditProposalCreate                     string
	queryPolicyEditProposalGetByID                    string
	queryPolicyOverrideCreate                         string
	queryPolicyOverrideCancel                         string
	queryPolicyOverrideGetByID                        string
	queryPolicyOverrideList                           string
	queryProjectCreate                                string
	queryProjectGetByID                               string
	queryProjectList                                  string
	queryProjectUpdate                                string
	queryReleaseLineCreate                            string
	queryReleaseLineGetByID                           string
	queryReleaseLineList                              string
	queryReleaseLineUpdate                            string
	queryReleasePolicyCreate                          string
	queryReleasePolicyGetByID                         string
	queryReleasePolicyList                            string
	queryReleasePolicyUpdate                          string
	queryRepositoryCreate                             string
	queryRepositoryGetByID                            string
	queryRepositoryGetByProviderRef                   string
	queryRepositoryList                               string
	queryRepositoryUpdate                             string
	queryServiceDescriptorInsert                      string
	queryServiceDescriptorList                        string
	queryServiceDescriptorMarkProjectStale            string
	queryServicesPolicyGetActive                      string
	queryServicesPolicyGetByID                        string
	queryServicesPolicyGetBySource                    string
	queryServicesPolicyInsert                         string
	queryServicesPolicyNextVersion                    string
	queryWorkspaceCodeSourceList                      string
	queryWorkspaceDocumentationSourceList             string
	queryWorkspaceGuidanceRefList                     string
)

func init() {
	for target, name := range map[*string]string{
		&queryBranchRulesCreate:                            "branch_rules__create",
		&queryBranchRulesGetByID:                           "branch_rules__get_by_id",
		&queryBranchRulesList:                              "branch_rules__list",
		&queryBranchRulesUpdate:                            "branch_rules__update",
		&queryCommandResultCreate:                          "command_result__create",
		&queryCommandResultGet:                             "command_result__get",
		&queryDocumentationSourceCreate:                    "documentation_source__create",
		&queryDocumentationSourceGetByID:                   "documentation_source__get_by_id",
		&queryDocumentationSourceList:                      "documentation_source__list",
		&queryDocumentationSourceMarkPolicyManagedDisabled: "documentation_source__mark_policy_managed_disabled",
		&queryDocumentationSourceUpdate:                    "documentation_source__update",
		&queryDocumentationSourceUpsertPolicy:              "documentation_source__upsert_policy",
		&queryOnboardingSignalReconciliationUpsert:         "onboarding_signal_reconciliation__upsert",
		&queryOutboxEventClaim:                             "outbox_event__claim",
		&queryOutboxEventCreate:                            "outbox_event__create",
		&queryOutboxEventMarkFailed:                        "outbox_event__mark_failed",
		&queryOutboxEventMarkPermanentlyFailed:             "outbox_event__mark_permanently_failed",
		&queryOutboxEventMarkPublished:                     "outbox_event__mark_published",
		&queryPlacementPolicyCreate:                        "placement_policy__create",
		&queryPlacementPolicyGetByID:                       "placement_policy__get_by_id",
		&queryPlacementPolicyList:                          "placement_policy__list",
		&queryPlacementPolicyUpdate:                        "placement_policy__update",
		&queryPolicyEditProposalCreate:                     "policy_edit_proposal__create",
		&queryPolicyEditProposalGetByID:                    "policy_edit_proposal__get_by_id",
		&queryPolicyOverrideCreate:                         "policy_override__create",
		&queryPolicyOverrideCancel:                         "policy_override__cancel",
		&queryPolicyOverrideGetByID:                        "policy_override__get_by_id",
		&queryPolicyOverrideList:                           "policy_override__list",
		&queryProjectCreate:                                "project__create",
		&queryProjectGetByID:                               "project__get_by_id",
		&queryProjectList:                                  "project__list",
		&queryProjectUpdate:                                "project__update",
		&queryReleaseLineCreate:                            "release_line__create",
		&queryReleaseLineGetByID:                           "release_line__get_by_id",
		&queryReleaseLineList:                              "release_line__list",
		&queryReleaseLineUpdate:                            "release_line__update",
		&queryReleasePolicyCreate:                          "release_policy__create",
		&queryReleasePolicyGetByID:                         "release_policy__get_by_id",
		&queryReleasePolicyList:                            "release_policy__list",
		&queryReleasePolicyUpdate:                          "release_policy__update",
		&queryRepositoryCreate:                             "repository__create",
		&queryRepositoryGetByID:                            "repository__get_by_id",
		&queryRepositoryGetByProviderRef:                   "repository__get_by_provider_ref",
		&queryRepositoryList:                               "repository__list",
		&queryRepositoryUpdate:                             "repository__update",
		&queryServiceDescriptorInsert:                      "service_descriptor__insert",
		&queryServiceDescriptorList:                        "service_descriptor__list",
		&queryServiceDescriptorMarkProjectStale:            "service_descriptor__mark_project_stale",
		&queryServicesPolicyGetActive:                      "services_policy__get_active",
		&queryServicesPolicyGetByID:                        "services_policy__get_by_id",
		&queryServicesPolicyGetBySource:                    "services_policy__get_by_source",
		&queryServicesPolicyInsert:                         "services_policy__insert",
		&queryServicesPolicyNextVersion:                    "services_policy__next_version",
		&queryWorkspaceCodeSourceList:                      "workspace_code_source__list",
		&queryWorkspaceDocumentationSourceList:             "workspace_documentation_source__list",
		&queryWorkspaceGuidanceRefList:                     "workspace_guidance_ref__list",
	} {
		*target = mustLoadQuery(name)
	}
}

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err == nil {
		return query
	}
	panic(err)
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load project-catalog sql query %s: %w", name, err)
	}
	return string(data), nil
}
