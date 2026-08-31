package platform

import _ "embed"

var (
	//go:embed sql/provider_credential_cleanup_expire_terminal_claims.sql
	queryProviderCredentialCleanupExpireTerminalClaims string
	//go:embed sql/provider_credential_cleanup_lock_claimable_accounts.sql
	queryProviderCredentialCleanupLockClaimableAccounts string
	//go:embed sql/provider_credential_cleanup_select_claimable_tasks.sql
	queryProviderCredentialCleanupSelectClaimableTasks string
	//go:embed sql/provider_credential_cleanup_claim_task.sql
	queryProviderCredentialCleanupClaimTask string
	//go:embed sql/provider_credential_cleanup_lock_task.sql
	queryProviderCredentialCleanupLockTask string
	//go:embed sql/provider_credential_cleanup_complete_task.sql
	queryProviderCredentialCleanupCompleteTask string
	//go:embed sql/provider_credential_cleanup_fail_task.sql
	queryProviderCredentialCleanupFailTask string
)
