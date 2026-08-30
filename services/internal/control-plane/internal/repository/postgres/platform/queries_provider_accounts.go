package platform

import _ "embed"

var (
	//go:embed sql/provider_accounts_create.sql
	queryProviderAccountsCreate string
	//go:embed sql/provider_accounts_lock.sql
	queryProviderAccountsLock string
	//go:embed sql/provider_accounts_insert_authorization.sql
	queryProviderAccountsInsertAuthorization string
	//go:embed sql/provider_accounts_complete_authorization.sql
	queryProviderAccountsCompleteAuthorization string
	//go:embed sql/provider_accounts_insert_credential_revision.sql
	queryProviderAccountsInsertCredentialRevision string
	//go:embed sql/provider_accounts_activate_credential.sql
	queryProviderAccountsActivateCredential string
	//go:embed sql/provider_accounts_update_lifecycle.sql
	queryProviderAccountsUpdateLifecycle string
	//go:embed sql/provider_accounts_fail_pending_authorizations.sql
	queryProviderAccountsFailPendingAuthorizations string
	//go:embed sql/provider_accounts_materialization_referenced.sql
	queryProviderAccountsMaterializationReferenced string
	//go:embed sql/provider_accounts_materialization_guard.sql
	queryProviderAccountsMaterializationGuard string
)
