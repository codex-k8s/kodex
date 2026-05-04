package access

import "fmt"

var (
	queryAccessActionGetByKey                 = mustLoadQuery("access_action__get_by_key")
	queryAccessActionUpsert                   = mustLoadQuery("access_action__upsert")
	queryAccessDecisionAuditCreate            = mustLoadQuery("access_decision_audit__create")
	queryAccessDecisionAuditGetByID           = mustLoadQuery("access_decision_audit__get_by_id")
	queryAccessRuleFindByIdentity             = mustLoadQuery("access_rule__find_by_identity")
	queryAccessRuleListForCheck               = mustLoadQuery("access_rule__list_for_check")
	queryAccessRuleUpsert                     = mustLoadQuery("access_rule__upsert")
	queryAllowlistEntryFind                   = mustLoadQuery("allowlist_entry__find")
	queryAllowlistEntryGetByID                = mustLoadQuery("allowlist_entry__get_by_id")
	queryAllowlistEntryUpdate                 = mustLoadQuery("allowlist_entry__update")
	queryAllowlistEntryUpsert                 = mustLoadQuery("allowlist_entry__upsert")
	queryCommandResultCreate                  = mustLoadQuery("command_result__create")
	queryCommandResultGet                     = mustLoadQuery("command_result__get")
	queryExternalAccountBindingFindByIdentity = mustLoadQuery("external_account_binding__find_by_identity")
	queryExternalAccountBindingFindForUsage   = mustLoadQuery("external_account_binding__find_for_usage")
	queryExternalAccountBindingGetByID        = mustLoadQuery("external_account_binding__get_by_id")
	queryExternalAccountBindingUpdate         = mustLoadQuery("external_account_binding__update")
	queryExternalAccountBindingUpsert         = mustLoadQuery("external_account_binding__upsert")
	queryExternalAccountCreate                = mustLoadQuery("external_account__create")
	queryExternalAccountGetByID               = mustLoadQuery("external_account__get_by_id")
	queryExternalAccountUpdate                = mustLoadQuery("external_account__update")
	queryExternalProviderGetBySlug            = mustLoadQuery("external_provider__get_by_slug")
	queryExternalProviderGetByID              = mustLoadQuery("external_provider__get_by_id")
	queryExternalProviderUpdate               = mustLoadQuery("external_provider__update")
	queryExternalProviderUpsert               = mustLoadQuery("external_provider__upsert")
	queryGroupCreate                          = mustLoadQuery("group__create")
	queryGroupGetByID                         = mustLoadQuery("group__get_by_id")
	queryMembershipFindByIdentity             = mustLoadQuery("membership__find_by_identity")
	queryMembershipListBySubject              = mustLoadQuery("membership__list_by_subject")
	queryMembershipUpsert                     = mustLoadQuery("membership__upsert")
	queryOrganizationCountActiveOwner         = mustLoadQuery("organization__count_active_owner")
	queryOrganizationCreate                   = mustLoadQuery("organization__create")
	queryOrganizationGetByID                  = mustLoadQuery("organization__get_by_id")
	queryOutboxEventCreate                    = mustLoadQuery("outbox_event__create")
	queryPendingAccessList                    = mustLoadQuery("pending_access__list")
	querySecretBindingRefGetByID              = mustLoadQuery("secret_binding_ref__get_by_id")
	querySecretBindingRefUpsert               = mustLoadQuery("secret_binding_ref__upsert")
	queryUserCreate                           = mustLoadQuery("user__create")
	queryUserGetByEmail                       = mustLoadQuery("user__get_by_email")
	queryUserGetByID                          = mustLoadQuery("user__get_by_id")
	queryUserGetByIdentity                    = mustLoadQuery("user__get_by_identity")
	queryUserListAccessScopes                 = mustLoadQuery("user__list_access_scopes")
	queryUserUpdate                           = mustLoadQuery("user__update")
	queryUserIdentityCreate                   = mustLoadQuery("user_identity__create")
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
