package platform

import _ "embed"

var (
	//go:embed sql/mvp_list_provider_definitions.sql
	queryMVPListProviderDefinitions string
	//go:embed sql/mvp_list_provider_accounts.sql
	queryMVPListProviderAccounts string
	//go:embed sql/mvp_list_model_capabilities.sql
	queryMVPListModelCapabilities string
	//go:embed sql/mvp_get_provider_account.sql
	queryMVPGetProviderAccount string
	//go:embed sql/mvp_list_role_image_revisions.sql
	queryMVPListRoleImageRevisions string
	//go:embed sql/mvp_list_schedule_revisions.sql
	queryMVPListScheduleRevisions string
	//go:embed sql/mvp_list_schedule_runs.sql
	queryMVPListScheduleRuns string
	//go:embed sql/mvp_runtime_environment_agents.sql
	queryMVPRuntimeEnvironmentAgents string
	//go:embed sql/prompt_preview_snapshot.sql
	queryPromptPreviewSnapshot string
)
