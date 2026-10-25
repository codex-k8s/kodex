package platform

import _ "embed"

var (
	// queryMVPListProviderDefinitions embeds global ordered provider-definition metadata with optional
	// key/name search and key cursor, including enabled status without hiding disabled definitions.
	// Caller read admission and page bounds are separate; this is not an executable provider registry
	// grant.
	// queryMVPListProviderDefinitions встраивает global ordered provider-definition metadata с
	// optional key/name search и key cursor, включая enabled status без скрытия disabled definitions.
	// Caller read admission и page bounds отдельны; это не executable provider registry grant.
	//go:embed sql/mvp_list_provider_definitions.sql
	queryMVPListProviderDefinitions string
	// queryMVPListProviderAccounts embeds tenant account pagination by descending update time/ref with
	// search/state/definition filters and latest non-reserved authorization metadata. Financial
	// observation is shown only for the enabled authorized current credential; caller resource
	// visibility and sensitive-field handling remain separate.
	// queryMVPListProviderAccounts встраивает tenant account pagination по убыванию update time/ref с
	// search/state/definition filters и latest non-reserved authorization metadata. Financial
	// observation показывается только для enabled authorized current credential; caller resource
	// visibility и sensitive-field handling отдельны.
	//go:embed sql/mvp_list_provider_accounts.sql
	queryMVPListProviderAccounts string
	// queryMVPListModelCapabilities embeds up to 4097 tenant account/provider catalog rows, preserving
	// last successful models while separately classifying current version/credential/failure/expiry
	// readiness. Provider/account filters do not authorize the actor, and stale displayed models must
	// not be treated as current execution admission.
	// queryMVPListModelCapabilities встраивает до 4097 tenant account/provider catalog rows, сохраняя
	// last successful models с отдельной классификацией current version/credential/failure/expiry
	// readiness. Provider/account filters не авторизуют actor; stale displayed models не являются
	// current execution admission.
	//go:embed sql/mvp_list_model_capabilities.sql
	queryMVPListModelCapabilities string
	// queryMVPGetProviderAccount embeds one exact tenant/account projection with latest non-reserved
	// authorization metadata and current-credential-gated financial observation. It does not filter by
	// actor permissions or automatically suppress revoked/deleted states; consumers retain access and
	// lifecycle policy.
	// queryMVPGetProviderAccount встраивает одну exact tenant/account projection с latest non-reserved
	// authorization metadata и current-credential-gated financial observation. Не фильтрует actor
	// permissions и автоматически не скрывает revoked/deleted states; access и lifecycle policy
	// остаются у consumers.
	//go:embed sql/mvp_get_provider_account.sql
	queryMVPGetProviderAccount string
	// queryMVPListRoleImageRevisions embeds descending tenant/recipe revision history with exact
	// build/source/provenance/manifest/promotion receipt metadata and an optional earlier-revision
	// boundary. Reading these pins neither verifies signatures nor grants build, promotion or pull
	// authority.
	// queryMVPListRoleImageRevisions встраивает descending tenant/recipe revision history с exact
	// build/source/provenance/manifest/promotion receipt metadata и optional earlier-revision
	// boundary. Чтение pins не проверяет signatures и не выдаёт build, promotion либо pull authority.
	//go:embed sql/mvp_list_role_image_revisions.sql
	queryMVPListRoleImageRevisions string
	// queryMVPListScheduleRevisions embeds descending tenant/schedule immutable revision metadata
	// including target pins, timing/DST/misfire/overlap and prompt/input policies. It performs no
	// actor access check and creates no occurrence or launch grant.
	// queryMVPListScheduleRevisions встраивает descending tenant/schedule immutable revision metadata,
	// включая target pins, timing/DST/misfire/overlap и prompt/input policies. Не проверяет actor
	// access, не создаёт occurrence либо launch grant.
	//go:embed sql/mvp_list_schedule_revisions.sql
	queryMVPListScheduleRevisions string
	// queryMVPListScheduleRuns embeds tenant/schedule occurrence-run pagination with
	// root/parent/retry/session/target, usage and root artifact/gate locators. It reads current
	// projection fields alongside immutable schedule revision pins; caller authorization and any
	// per-result eligibility are not supplied by these joins.
	// queryMVPListScheduleRuns встраивает tenant/schedule occurrence-run pagination с
	// root/parent/retry/session/target, usage и root artifact/gate locators. Читает current projection
	// fields вместе с immutable schedule revision pins; caller authorization и per-result eligibility
	// этими joins не задаются.
	//go:embed sql/mvp_list_schedule_runs.sql
	queryMVPListScheduleRuns string
	// queryMVPRuntimeEnvironmentAgents embeds ordered non-archived agent refs currently bound to a
	// tenant environment, with optional name/ref search and ref cursor. It does not check agent.view
	// or authorize rebinding; callers own visibility and mutations.
	// queryMVPRuntimeEnvironmentAgents встраивает ordered non-archived agent refs с current binding
	// tenant environment, optional name/ref search и ref cursor. Не проверяет agent.view и не
	// разрешает rebinding; visibility и mutations принадлежат callers.
	//go:embed sql/mvp_runtime_environment_agents.sql
	queryMVPRuntimeEnvironmentAgents string
	// queryPromptPreviewSnapshot embeds the latest tenant runtime safe promptSnapshot for a run or
	// session target, ordered by revision time/ref. It checks snapshot presence but not viewer
	// run/session access or full snapshot validity; caller must resolve that trusted target before
	// returning preview.
	// queryPromptPreviewSnapshot встраивает latest tenant runtime safe promptSnapshot для run либо
	// session target по revision time/ref. Проверяет наличие snapshot, а не viewer run/session access
	// либо full snapshot validity; caller заранее разрешает trusted target до возврата preview.
	//go:embed sql/prompt_preview_snapshot.sql
	queryPromptPreviewSnapshot string
)
