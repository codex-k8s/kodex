package platform

import _ "embed"

// queryRoleImagesExpireBuilds expires up to 32 leased builds in active build stages with SKIP LOCKED, advancing fence/version and choosing EXPIRED or DEAD_LETTER by attempt limit.
// queryRoleImagesExpireBuilds завершает до 32 арендованных сборок в активных стадиях через SKIP LOCKED, увеличивает ограждение и версию и выбирает EXPIRED либо DEAD_LETTER по пределу попыток.
//
//go:embed sql/role_images_expire_builds.sql
var queryRoleImagesExpireBuilds string

// queryRoleImagesClaimOutcomeReceipt locks an unexpired idempotency receipt by tenant, actor, operation and key; it reads prior intent and response without creating a new receipt.
// queryRoleImagesClaimOutcomeReceipt блокирует неистёкшую квитанцию идемпотентности по организации, субъекту, операции и ключу; читает прежние ввод и ответ без создания новой квитанции.
//
//go:embed sql/role_images_claim_outcome_receipt.sql
var queryRoleImagesClaimOutcomeReceipt string

var (
	// queryRoleImagesReadFailureClaim reads an artifact's recorded admission failure generation and code by tenant and internal ID without locking or validating a live claim.
	// queryRoleImagesReadFailureClaim читает поколение и код сохранённого отказа в допуске артефакта по организации и внутреннему ID без блокировки или проверки живого захвата.
	//go:embed sql/role_images_read_failure_claim.sql
	queryRoleImagesReadFailureClaim string
	// queryRoleImagesAdmissionClaimReplayLive checks replay of one unexpired image-admission claim against artifact version, fence, generation, workload and token digest.
	// queryRoleImagesAdmissionClaimReplayLive проверяет повтор одного неистёкшего захвата image-admission по версии артефакта, ограждению, поколению, исполнителю и дайджесту токена.
	//go:embed sql/role_images_admission_claim_replay_live.sql
	queryRoleImagesAdmissionClaimReplayLive string
	// queryRoleImagesExpireAdmissionClaim fails an expired CLAIMED admission for the expected version and workload, preserves the failure generation and clears claim authority.
	// queryRoleImagesExpireAdmissionClaim переводит просроченный CLAIMED допуск ожидаемой версии и исполнителя в FAILED, сохраняет поколение отказа и очищает полномочия захвата.
	//go:embed sql/role_images_expire_admission_claim.sql
	queryRoleImagesExpireAdmissionClaim string
	// queryRoleImagesAdmissionCurrent tests whether artifact, active recipe and latest completed build still share exact recipe, policy and runtime-contract pins; it does not itself admit the artifact.
	// queryRoleImagesAdmissionCurrent проверяет совпадение точных привязок артефакта, активного рецепта и последней завершённой сборки к рецепту, политике и runtime-контракту; сам артефакт не допускает.
	//go:embed sql/role_images_admission_current.sql
	queryRoleImagesAdmissionCurrent string
	// queryRoleImagesFailAdmission fails a still-live CLAIMED admission for the expected version and workload, records the failure generation and revokes its claim.
	// queryRoleImagesFailAdmission завершает отказом ещё действующий CLAIMED допуск ожидаемой версии и исполнителя, записывает поколение отказа и отзывает захват.
	//go:embed sql/role_images_fail_admission.sql
	queryRoleImagesFailAdmission string
	// queryRoleImagesExpireAdmissions locks at most 32 expired admission claims with SKIP LOCKED, marks them FAILED and returns recipe/build/project references for follow-up.
	// queryRoleImagesExpireAdmissions блокирует не более 32 просроченных захватов допуска через SKIP LOCKED, переводит их в FAILED и возвращает ссылки рецепта, сборки и проекта для продолжения.
	//go:embed sql/role_images_expire_admissions.sql
	queryRoleImagesExpireAdmissions string
	// queryRoleImagesGetAdmissionFailure reads a nonempty FAILED admission of the latest build only while artifact and active recipe pins still match; it does not retry that admission.
	// queryRoleImagesGetAdmissionFailure читает непустой отказ FAILED допуска последней сборки лишь при совпадении привязок артефакта и активного рецепта; повтор допуска не запускает.
	//go:embed sql/role_images_get_admission_failure.sql
	queryRoleImagesGetAdmissionFailure string
	// queryRoleImagesListRecipes lists tenant-scoped recipe views with scope, actor eligibility, filters and updated-at cursor pagination; project and organization eligibility use distinct predicates.
	// queryRoleImagesListRecipes выдаёт рецепты организации с фильтрами, проверкой доступа по области и субъекту и пагинацией по времени обновления; для проекта и организации действуют разные предикаты.
	//go:embed sql/role_images_list_recipes.sql
	queryRoleImagesListRecipes string
	// queryRoleImagesGetRecipe reads one tenant recipe and its role/project/active-artifact descriptors by ref; caller must establish actor eligibility separately.
	// queryRoleImagesGetRecipe читает один рецепт организации и описатели роли, проекта и активного артефакта по ссылке; доступ субъекта вызывающий код проверяет отдельно.
	//go:embed sql/role_images_get_recipe.sql
	queryRoleImagesGetRecipe string
	// queryRoleImagesListBuilds returns up to 100 recent builds for a resolved tenant recipe, including lease and provenance metadata; it does not choose a claimable build.
	// queryRoleImagesListBuilds возвращает до 100 последних сборок разрешённого рецепта организации с метаданными аренды и происхождения; кандидата на захват не выбирает.
	//go:embed sql/role_images_list_builds.sql
	queryRoleImagesListBuilds string
	// queryRoleImagesGetActiveArtifact reads one tenant artifact's build, evidence, signature, admission and promotion descriptors; the projection does not prove current eligibility.
	// queryRoleImagesGetActiveArtifact читает описатели сборки, доказательств, подписи, допуска и продвижения одного артефакта организации; проекция не доказывает текущую допустимость.
	//go:embed sql/role_images_get_active_artifact.sql
	queryRoleImagesGetActiveArtifact string
	// queryRoleImagesGetPromotionCandidate selects the latest recipe-pinned artifact that is accepted with required evidence, rejected, or has a pending risk decision; only the returned promotable flag marks the initial promotion path.
	// queryRoleImagesGetPromotionCandidate выбирает последний артефакт с актуальными привязками рецепта: принятый с доказательствами, отклонённый либо с ожидающим риск-решением; только возвращаемый признак promotable отмечает начальный путь продвижения.
	//go:embed sql/role_images_get_promotion_candidate.sql
	queryRoleImagesGetPromotionCandidate string
	// queryRoleImagesResolveProjectRole resolves an ACTIVE project and ACTIVE role in the same tenant by refs, returning their IDs without granting image access on its own.
	// queryRoleImagesResolveProjectRole разрешает ACTIVE проект и ACTIVE роль одной организации по ссылкам и возвращает ID без самостоятельной выдачи доступа к образу.
	//go:embed sql/role_images_resolve_project_role.sql
	queryRoleImagesResolveProjectRole string
	// queryRoleImagesLockRecipe locks a tenant recipe by ref and returns its version, generation, policy and runtime-contract pins; authorization is outside this row lock.
	// queryRoleImagesLockRecipe блокирует рецепт организации по ссылке и возвращает версию, поколение и привязки политики и runtime-контракта; авторизация находится вне этой блокировки строки.
	//go:embed sql/role_images_lock_recipe.sql
	queryRoleImagesLockRecipe string
	// queryRoleImagesResolveAccessTarget resolves a recipe access target by tenant, ref and scope, requiring an ACTIVE project for project-scoped recipes; it does not evaluate actor permissions.
	// queryRoleImagesResolveAccessTarget разрешает цель доступа рецепта по организации, ссылке и области, требует ACTIVE проект для проектного рецепта; права субъекта не проверяет.
	//go:embed sql/role_images_resolve_access_target.sql
	queryRoleImagesResolveAccessTarget string
	// queryRoleImagesInsertRecipe inserts an ACTIVE recipe with generation one and supplied specification, policy, contract and owner pins; caller supplies validated IDs.
	// queryRoleImagesInsertRecipe создаёт ACTIVE рецепт первого поколения с заданными привязками спецификации, политики, контракта и владельца; проверенные ID передаёт вызывающий код.
	//go:embed sql/role_images_insert_recipe.sql
	queryRoleImagesInsertRecipe string
	// queryRoleImagesUpdateRecipe replaces a tenant recipe's name/specification and policy pins, increments generation/version and clears its active artifact; the statement has no expected-version predicate.
	// queryRoleImagesUpdateRecipe заменяет имя, спецификацию и привязки политики рецепта организации, увеличивает поколение и версию и очищает активный артефакт; условие ожидаемой версии отсутствует.
	//go:embed sql/role_images_update_recipe.sql
	queryRoleImagesUpdateRecipe string
	// queryRoleImagesRefreshRecipePolicy refreshes policy and runtime-contract pins on a tenant recipe and invalidates its active artifact, advancing version without a SQL expected-version guard.
	// queryRoleImagesRefreshRecipePolicy обновляет привязки политики и runtime-контракта рецепта организации и сбрасывает активный артефакт, увеличивая версию без SQL-условия ожидаемой версии.
	//go:embed sql/role_images_refresh_recipe_policy.sql
	queryRoleImagesRefreshRecipePolicy string
	// queryRoleImagesChangeRecipeState changes recipe state for a resolved tenant ID, incrementing generation and clearing active artifact when reactivated; caller controls allowed transitions.
	// queryRoleImagesChangeRecipeState меняет состояние рецепта по разрешённому ID организации, при повторной активации увеличивает поколение и очищает активный артефакт; допустимые переходы определяет вызывающий код.
	//go:embed sql/role_images_change_recipe_state.sql
	queryRoleImagesChangeRecipeState string
	// queryRoleImagesCancelOpenBuilds cancels nonterminal builds of a tenant recipe as superseded, revoking claimant, lease and generation/fence authority.
	// queryRoleImagesCancelOpenBuilds отменяет незавершённые сборки рецепта организации как вытесненные и отзывает исполнителя, аренду и полномочия поколения и ограждения.
	//go:embed sql/role_images_cancel_open_builds.sql
	queryRoleImagesCancelOpenBuilds string
	// queryRoleImagesCancelExactBuild cancels one nonterminal build by tenant, recipe and ref, clears claim authority and returns the resulting build view.
	// queryRoleImagesCancelExactBuild отменяет одну незавершённую сборку по организации, рецепту и ссылке, очищает полномочия захвата и возвращает полученное представление сборки.
	//go:embed sql/role_images_cancel_exact_build.sql
	queryRoleImagesCancelExactBuild string
	// queryRoleImagesCancelOpenPromotions fails queued/promoting requests of a tenant recipe and rejects their pending/claimed/authorized artifacts while clearing promotion claims and authorizations.
	// queryRoleImagesCancelOpenPromotions завершает отказом запросы продвижения рецепта организации в QUEUED/PROMOTING и отклоняет их ожидающие, захваченные или авторизованные артефакты с очисткой захватов и разрешений.
	//go:embed sql/role_images_cancel_open_promotions.sql
	queryRoleImagesCancelOpenPromotions string
	// queryRoleImagesFindPromotedArtifact finds the most recently promoted accepted artifact for the tenant recipe and exact specification digest; it does not revalidate current recipe pins.
	// queryRoleImagesFindPromotedArtifact находит последний продвинутый и допущенный артефакт рецепта организации с точным дайджестом спецификации; текущие привязки рецепта заново не проверяет.
	//go:embed sql/role_images_find_promoted_artifact.sql
	queryRoleImagesFindPromotedArtifact string
	// queryRoleImagesInsertBuild creates a QUEUED build attempt one from an ACTIVE tenant recipe snapshot, pinning recipe version, generation, specification and immutable build digest.
	// queryRoleImagesInsertBuild создаёт QUEUED первую попытку сборки по снимку ACTIVE рецепта организации, закрепляя версию, поколение, спецификацию и неизменяемый дайджест сборки.
	//go:embed sql/role_images_insert_build.sql
	queryRoleImagesInsertBuild string
	// queryRoleImagesClaimBuildCandidate locks one available QUEUED or retryable FAILED/EXPIRED build with SKIP LOCKED, ordered by availability; it does not assign a lease.
	// queryRoleImagesClaimBuildCandidate блокирует одну доступную QUEUED либо повторяемую FAILED/EXPIRED сборку через SKIP LOCKED в порядке готовности; аренду не назначает.
	//go:embed sql/role_images_claim_build_candidate.sql
	queryRoleImagesClaimBuildCandidate string
	// queryRoleImagesClaimBuild claims a tenant build at an expected version, pins attempt, workload, authority generation, fence and lease, and starts MATERIALIZATION; stage eligibility was selected earlier.
	// queryRoleImagesClaimBuild захватывает сборку организации при ожидаемой версии, закрепляет попытку, исполнителя, поколение полномочий, ограждение и аренду и начинает MATERIALIZATION; допустимая стадия выбрана ранее.
	//go:embed sql/role_images_claim_build.sql
	queryRoleImagesClaimBuild string
	// queryRoleImagesGetBuildInput reads immutable build input only while an ACTIVE recipe still matches build version, generation and specification digest; it returns current policy/contract metadata.
	// queryRoleImagesGetBuildInput читает неизменяемый ввод сборки лишь при совпадении ACTIVE рецепта с версией, поколением и дайджестом спецификации сборки; возвращает текущие метаданные политики и контракта.
	//go:embed sql/role_images_get_build_input.sql
	queryRoleImagesGetBuildInput string
	// queryRoleImagesLockBuild locks a tenant build by ref and returns lease, recipe, policy and contract metadata; the row lock alone does not validate a claimant.
	// queryRoleImagesLockBuild блокирует сборку организации по ссылке и возвращает метаданные аренды, рецепта, политики и контракта; одна блокировка строки не проверяет исполнителя.
	//go:embed sql/role_images_lock_build.sql
	queryRoleImagesLockBuild string
	// queryRoleImagesRenewBuild renews build lease and authority generation under tenant/ID/version only; caller must verify live claimant, fence and acceptable stage before this update.
	// queryRoleImagesRenewBuild обновляет аренду сборки и поколение полномочий только по организации, ID и версии; живого исполнителя, ограждение и допустимую стадию до обновления проверяет вызывающий код.
	//go:embed sql/role_images_renew_build.sql
	queryRoleImagesRenewBuild string
	// queryRoleImagesProgressBuild updates build stage and progress under tenant/ID/version; it does not independently validate stage transitions or lease authority.
	// queryRoleImagesProgressBuild обновляет стадию и ход сборки по организации, ID и версии; допустимость перехода и полномочия аренды самостоятельно не проверяет.
	//go:embed sql/role_images_progress_build.sql
	queryRoleImagesProgressBuild string
	// queryRoleImagesCompleteBuild marks a build COMPLETED under tenant/ID/version, records staging/provenance digests and clears lease authority; evidence validation is the caller's responsibility.
	// queryRoleImagesCompleteBuild завершает сборку как COMPLETED по организации, ID и версии, записывает staging и дайджесты происхождения и очищает аренду; доказательства проверяет вызывающий код.
	//go:embed sql/role_images_complete_build.sql
	queryRoleImagesCompleteBuild string
	// queryRoleImagesInsertArtifact inserts an artifact pinned to recipe/build versions, policy, runtime contract and staging evidence; this row does not represent admission or promotion.
	// queryRoleImagesInsertArtifact создаёт артефакт с привязками к версиям рецепта и сборки, политике, runtime-контракту и staging-доказательствам; эта строка не означает допуск или продвижение.
	//go:embed sql/role_images_insert_artifact.sql
	queryRoleImagesInsertArtifact string
	// queryRoleImagesFailBuild marks a build FAILED or DEAD_LETTER by attempt limit under tenant/ID/version, stores diagnostics, revokes lease and delays retry by 30 seconds.
	// queryRoleImagesFailBuild переводит сборку в FAILED либо DEAD_LETTER по пределу попыток при совпадении организации, ID и версии, сохраняет диагностику, отзывает аренду и откладывает повтор на 30 секунд.
	//go:embed sql/role_images_fail_build.sql
	queryRoleImagesFailBuild string
	// queryRoleImagesGetSupplyWorkAvailability reports admission availability for fresh PENDING or expired CLAIMED artifacts and stale-policy cleanup, plus promotion availability under exact evidence/request/recipe pins; it does not claim work.
	// queryRoleImagesGetSupplyWorkAvailability сообщает о доступности допуска для актуальных PENDING, просроченных CLAIMED и очистки устаревшей политики, а также о готовности продвижения по точным привязкам доказательств, запроса и рецепта; работу не захватывает.
	//go:embed sql/role_images_get_supply_work_availability.sql
	queryRoleImagesGetSupplyWorkAvailability string
	// queryRoleImagesRejectStaleAdmissionCandidates rejects PENDING/CLAIMED admissions whose policy revision or digest changed, revokes admission/promotion authority and fails linked open promotion requests.
	// queryRoleImagesRejectStaleAdmissionCandidates отклоняет PENDING/CLAIMED допуски с изменённой ревизией или дайджестом политики, отзывает полномочия допуска и продвижения и завершает связанные открытые запросы отказом.
	//go:embed sql/role_images_reject_stale_admission_candidates.sql
	queryRoleImagesRejectStaleAdmissionCandidates string
	// queryRoleImagesClaimAdmissionCandidate locks one PENDING artifact with current policy and exact ACTIVE recipe/latest completed build pins using SKIP LOCKED; admission claim is a separate write.
	// queryRoleImagesClaimAdmissionCandidate блокирует один PENDING артефакт с текущей политикой и точными привязками ACTIVE рецепта и последней завершённой сборки через SKIP LOCKED; захват допуска записывается отдельно.
	//go:embed sql/role_images_claim_admission_candidate.sql
	queryRoleImagesClaimAdmissionCandidate string
	// queryRoleImagesClaimAdmission sets one tenant artifact to CLAIMED admission at the expected version with workload, generation, fence and token/expiry pins; prior candidate selection supplies eligibility.
	// queryRoleImagesClaimAdmission переводит артефакт организации в CLAIMED допуск при ожидаемой версии с привязками исполнителя, поколения, ограждения, токена и срока; допустимость обеспечивает предшествующий выбор кандидата.
	//go:embed sql/role_images_claim_admission.sql
	queryRoleImagesClaimAdmission string
	// queryRoleImagesLockArtifact locks a tenant artifact by ref and reads complete admission/promotion claim, evidence and recipe/build metadata; caller validates authority and provenance.
	// queryRoleImagesLockArtifact блокирует артефакт организации по ссылке и читает полные метаданные захватов допуска и продвижения, доказательств, рецепта и сборки; полномочия и происхождение проверяет вызывающий код.
	//go:embed sql/role_images_lock_artifact.sql
	queryRoleImagesLockArtifact string
	// queryRoleImagesRecordAdmission records an admission verdict and evidence digests under tenant/ID/version, clears claim authority and selects PENDING or REJECTED promotion; SQL does not inspect evidence bytes.
	// queryRoleImagesRecordAdmission записывает вердикт допуска и дайджесты доказательств по организации, ID и версии, очищает полномочия захвата и выбирает PENDING либо REJECTED продвижение; SQL не проверяет байты доказательств.
	//go:embed sql/role_images_record_admission.sql
	queryRoleImagesRecordAdmission string
	// queryRoleImagesInsertPromotionRequest creates a promotion request with pinned artifact, provenance, manifest and receipt digests plus requester; authorization and artifact linking occur elsewhere.
	// queryRoleImagesInsertPromotionRequest создаёт запрос продвижения с закреплёнными артефактом, дайджестами происхождения, манифеста и квитанции и инициатором; авторизация и привязка артефакта выполняются отдельно.
	//go:embed sql/role_images_insert_promotion_request.sql
	queryRoleImagesInsertPromotionRequest string
	// queryRoleImagesLinkPromotionRequest links a request only to the expected-version PENDING artifact whose admission, recipe, policy, contract and complete evidence pins match; any mismatch returns no row.
	// queryRoleImagesLinkPromotionRequest привязывает запрос лишь к PENDING артефакту ожидаемой версии с совпадающими привязками допуска, рецепта, политики, контракта и полного набора доказательств; любое расхождение даёт отсутствие строки.
	//go:embed sql/role_images_link_promotion_request.sql
	queryRoleImagesLinkPromotionRequest string
	// queryRoleImagesClaimPromotionCandidate locks one accepted, evidence-complete promotion candidate with matching request/active recipe and eligible queued or expired state using SKIP LOCKED.
	// queryRoleImagesClaimPromotionCandidate блокирует через SKIP LOCKED один принятый кандидат на продвижение с полным набором доказательств, совпадающими запросом и ACTIVE рецептом и допустимым ожидающим или просроченным состоянием.
	//go:embed sql/role_images_claim_promotion_candidate.sql
	queryRoleImagesClaimPromotionCandidate string
	// queryRoleImagesClaimPromotion claims an accepted artifact at expected version only for the pinned promotion request/receipt and eligible state, replacing claim and clearing prior authorization.
	// queryRoleImagesClaimPromotion захватывает принятый артефакт при ожидаемой версии лишь для закреплённых запроса и квитанции продвижения и допустимого состояния, заменяя захват и очищая прежнее разрешение.
	//go:embed sql/role_images_claim_promotion.sql
	queryRoleImagesClaimPromotion string
	// queryRoleImagesMarkPromotionRequestPromoting marks a matching QUEUED or already PROMOTING request as PROMOTING by tenant, artifact, receipt and evidence pins; it does not promote the registry image.
	// queryRoleImagesMarkPromotionRequestPromoting переводит совпадающий QUEUED либо уже PROMOTING запрос в PROMOTING по организации, артефакту, квитанции и доказательствам; образ в реестре не продвигает.
	//go:embed sql/role_images_mark_promotion_request_promoting.sql
	queryRoleImagesMarkPromotionRequestPromoting string
	// queryRoleImagesLockPromotionRequest locks one tenant promotion request by artifact ID and returns requester, receipt, provenance, manifest and state; no owner decision is created here.
	// queryRoleImagesLockPromotionRequest блокирует один запрос продвижения организации по ID артефакта и возвращает инициатора, квитанцию, происхождение, манифест и состояние; решение владельца здесь не создаётся.
	//go:embed sql/role_images_lock_promotion_request.sql
	queryRoleImagesLockPromotionRequest string
	// queryRoleImagesAuthorizePromotion authorizes a live CLAIMED promotion under exact version, generation, fence, token, request receipt and provenance pins, replacing claim with short-lived authorization.
	// queryRoleImagesAuthorizePromotion разрешает живое CLAIMED продвижение при точных привязках версии, поколения, ограждения, токена, квитанции запроса и происхождения, заменяя захват краткосрочным разрешением.
	//go:embed sql/role_images_authorize_promotion.sql
	queryRoleImagesAuthorizePromotion string
	// queryRoleImagesCompletePromotion records PROMOTED state and registry readback only for an unexpired AUTHORIZED promotion with exact receipt, request, fence and provenance pins; external promotion is not performed by SQL.
	// queryRoleImagesCompletePromotion фиксирует PROMOTED и результат чтения реестра только для неистёкшего AUTHORIZED продвижения с точными привязками квитанции, запроса, ограждения и происхождения; внешнее продвижение SQL не выполняет.
	//go:embed sql/role_images_complete_promotion.sql
	queryRoleImagesCompletePromotion string
	// queryRoleImagesMarkPromotionRequestPromoted marks the exact PROMOTING request as PROMOTED when tenant, artifact, receipt, provenance and manifest match; it does not activate the recipe.
	// queryRoleImagesMarkPromotionRequestPromoted отмечает точный PROMOTING запрос как PROMOTED при совпадении организации, артефакта, квитанции, происхождения и манифеста; рецепт не активирует.
	//go:embed sql/role_images_mark_promotion_request_promoted.sql
	queryRoleImagesMarkPromotionRequestPromoted string
	// queryRoleImagesActivateArtifact activates a promoted artifact for an ACTIVE recipe at expected version only if spec, artifact, request and readback pins match; this query still expects request state PROMOTING.
	// queryRoleImagesActivateArtifact активирует продвинутый артефакт в ACTIVE рецепте при ожидаемой версии лишь при совпадении спецификации, артефакта, запроса и результата чтения; этот запрос ещё ожидает состояние запроса PROMOTING.
	//go:embed sql/role_images_activate_artifact.sql
	queryRoleImagesActivateArtifact string
	// queryRoleImagesInsertPromotedRevision inserts the next recipe revision after a PROMOTED request, matching admitted/promoted artifact, active recipe version, source digest and readback; max-plus-one ordering belongs to the caller transaction.
	// queryRoleImagesInsertPromotedRevision создаёт следующую ревизию рецепта после PROMOTED запроса при совпадении допущенного и продвинутого артефакта, версии активного рецепта, исходного дайджеста и результата чтения; порядок max-plus-one обеспечивает транзакция вызывающего кода.
	//go:embed sql/role_images_insert_promoted_revision.sql
	queryRoleImagesInsertPromotedRevision string
)
