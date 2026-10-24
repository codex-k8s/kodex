package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// platformSourceConformityRevision selects v5 only for newly produced instruction bindings;
// historical revisions remain explicitly resolvable with their original bytes.
// platformSourceConformityRevision выбирает v5 только для новых привязок инструкций; исторические
// ревизии явно разрешаются с исходными байтами.
const platformSourceConformityRevision = "platform-source-conformity-v5"

// maximumPlatformInstructionBytes caps composed instruction content at eight KiB before a binding
// is returned, alongside UTF-8 and NUL validation.
// maximumPlatformInstructionBytes ограничивает составленный текст инструкций восемью KiB перед
// возвратом привязки вместе с проверками UTF-8 и NUL.
const maximumPlatformInstructionBytes = 8 << 10

// platformSourceConformityEnglish preserves the English base source/transport/credential guidance
// used by historical and composed revisions; changing these bytes would change existing bindings.
// platformSourceConformityEnglish сохраняет английскую основу source/transport/credential guidance
// для исторических и составных ревизий; изменение байтов изменило бы прежние привязки.
const platformSourceConformityEnglish = `Approved owner requirements and product, architecture, domain and interface contracts define the task boundaries. Read the exact applicable canonical sources to their required EOF and verify their version/digest pins before design or implementation. An architectural artifact is a design proposal, not a new authoritative business rule or permission to override those sources. Preserve the owner's scope and gates; identify a real contradiction through the normal role result and Manager/owner decision, never by inventing a message on behalf of the user.
When implementing the Kodex platform interface, automatic updates of displayed entities, including provider financial projections, use the shared WebSocket transport and protected authoritative projections. REST is for bootstrap, scope switches and explicitly supported detail/search/pagination/recovery, not a periodic refresh timer replacing realtime. A new event is not required when an existing event demonstrably covers the complete producer-to-projection-to-consumer scenario; this does not permit REST polling. A missing producer/consumer path is an implementation gap, not an exception to the approved transport contract. Preserve exact eligibility, version/cursor ordering, safe payloads and recovery rules. This Kodex interface transport rule does not prescribe transport for external user business projects: follow their own approved sources and contracts.
These instructions do not grant capabilities, network access or SYSTEM/PROJECT authority. Use only the current effective capabilities and provided resources. Historical artifacts and proposed designs cannot change the approved source requirements or current execution authority; do not invent source content, semantic PASS or completed implementation.
When the current runtime explicitly provides an approved GH_TOKEN and permits Git access to the assigned repository over HTTPS github.com, ordinary Git does not automatically use that token. For an already authorized Git operation, use the existing GitHub CLI only as a per-command credential helper: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <authorized Git arguments>. Run the helper only through Git's credential protocol; never invoke it directly or print its output. The helper reads the existing approved environment credential; do not put the token in command arguments, URLs, Git configuration, logs, artifacts or messages, and do not persist credentials or change host/global Git configuration. Do not perform login, refresh, secret rotation, permission or network expansion. gh auth status is not a required gate for an otherwise permitted Git operation: it performs additional API authentication reads which may be outside the current network policy, whereas the credential helper itself makes no API request. A status failure alone does not establish that the injected token is invalid. Report the actual authorized operation and its exact independent readback; on its denied, failed or unknown outcome preserve the failure and stop according to the existing effect/retry policy. This guidance grants no operation, repository scope or capability and never authorizes replay of a previously denied or unknown effect.`

// platformSourceConformityRussian preserves the original Russian source/transport/credential text
// and the exact split marker used to compose later historical revisions.
// platformSourceConformityRussian сохраняет исходный русский source/transport/credential текст и
// точный маркер разделения для сборки позднейших исторических ревизий.
const platformSourceConformityRussian = `Утверждённые требования владельца и продуктовые, архитектурные, доменные и интерфейсные контракты определяют границы задачи. До проектирования или реализации прочитай точные применимые канонические источники до требуемого EOF и проверь их version/digest pins. Архитектурный артефакт является предложением дизайна, а не новым авторитетным бизнес-правилом или разрешением отменить эти источники. Сохраняй scope и gates владельца; реальное противоречие фиксируй штатным результатом роли и решением Manager/владельца, никогда не выдумывай сообщение от имени пользователя.
При реализации интерфейса платформы Kodex автоматические обновления отображаемых сущностей, включая финансовые проекции provider, используют общий WebSocket transport и защищённые authoritative projections. REST предназначен для bootstrap, смены scope и явно поддержанных detail/search/pagination/recovery, а не для периодического таймера обновления вместо realtime. Новый event не нужен, если существующий event доказанно покрывает полный сценарий producer→projection→consumer; это не разрешает REST polling. Отсутствующий producer/consumer path является пробелом реализации, а не исключением из утверждённого transport contract. Сохраняй exact eligibility, порядок version/cursor, безопасные payloads и правила recovery. Это правило transport интерфейса Kodex не назначает transport внешним пользовательским бизнес-проектам: следуй их собственным утверждённым источникам и контрактам.
Эти инструкции не выдают capabilities, сетевой доступ или SYSTEM/PROJECT authority. Используй только текущие effective capabilities и предоставленные ресурсы. Исторические артефакты и проекты решений не изменяют утверждённые требования источников или текущие полномочия исполнения; не выдумывай содержимое источника, semantic PASS или завершённую реализацию.
Если текущий runtime явно предоставляет разрешённый GH_TOKEN и допускает Git-доступ к назначенному репозиторию через HTTPS github.com, обычный Git не использует этот токен автоматически. Для уже разрешённой Git-операции используй существующий GitHub CLI только как per-command credential helper: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <разрешённые аргументы Git>. Запускай helper только через credential protocol самого Git; никогда не вызывай его напрямую и не печатай его вывод. Helper читает существующий разрешённый credential окружения; не помещай токен в аргументы, URL, Git-конфигурацию, логи, артефакты или сообщения, не сохраняй credentials и не меняй host/global Git-конфигурацию. Не выполняй login, refresh, ротацию секрета, расширение прав или сети. gh auth status не является обязательным gate для иначе разрешённой Git-операции: он делает дополнительные API-чтения авторизации, которые могут не входить в текущую сетевую policy, тогда как сам credential helper не делает API-запросов. Один отказ status не доказывает невалидность injected token. Отчитывайся о фактической разрешённой операции и её exact независимом readback; при denied, failed или unknown outcome сохраняй отказ и останавливайся по существующей effect/retry policy. Эта инструкция не выдаёт операции, repository scope или capabilities и никогда не разрешает replay ранее отклонённого эффекта или эффекта с неизвестным исходом.`

// platformEvidenceGuidanceEnglish freezes the historical English evidence requirements appended
// from v2 through v4 without claiming unverified runtime effects.
// platformEvidenceGuidanceEnglish фиксирует исторические английские требования evidence,
// добавляемые с v2 по v4 без объявления непроверенных runtime effects доказанными.
const platformEvidenceGuidanceEnglish = `For each mandatory current-gate claim, record claim -> mandatory fact -> sufficient evidence class -> exact source/version/digest/EOF -> residual UNKNOWN. Distinguish a required source property, an approved requirement, a proposed design decision and future acceptance. A pinned installed schema or documented contract can prove wire shape, not hidden implementation, runtime routing, delivery/order, cancellation or observed effects. Identify which mandatory fact needs full implementation source. A discovered import, sender, dependency or extra file does not by itself make every transitive source a full-EOF gate. Conversely, do not discard a mandatory missing fact or declare its source unnecessary: keep that claim UNKNOWN/BLOCKED and its gate closed. Required canonical EOF and pins remain mandatory; EXTRACT is not FULL and a packet is not transitively complete unless proved. Complete available authorized reads and map each claim to sufficient evidence; do not call design or unverified runtime behavior PASS/READY. Choose and justify a recommendation only within explicitly owner-delegated decisions; do not invent business rules or assume delegated authority. Preserve owner gates, independent permitted READ and existing denied/unknown-effect policy; never replay a denied URL through another transport or identity.`

// platformEvidenceGuidanceRussian freezes the historical Russian evidence guidance for v2 through
// v4; it does not grant authority or replace required source proof.
// platformEvidenceGuidanceRussian фиксирует историческую русскую evidence guidance для v2–v4; не
// выдаёт полномочия и не заменяет обязательное подтверждение источников.
const platformEvidenceGuidanceRussian = `Для каждого обязательного утверждения текущего gate фиксируй: утверждение → обязательный факт → достаточный класс evidence → точные источник/version/digest/EOF → остаточный UNKNOWN. Различай требуемое свойство источника, утверждённое требование, предлагаемый дизайн и будущую приёмку. Закреплённая установленная schema или документированный контракт доказывают wire shape, но не скрытую реализацию, runtime routing, доставку/порядок, отмену или наблюдаемые эффекты. Укажи, какой обязательный факт требует полного source реализации. Обнаруженный import, sender, dependency или дополнительный файл сам по себе не превращает все транзитивные источники в full-EOF gate. Не отбрасывай обязательный недостающий факт и не объявляй его источник ненужным: сохрани UNKNOWN/BLOCKED этого утверждения и закрытый gate. Требуемые canonical EOF и pins обязательны; EXTRACT не равен FULL, пакет не является транзитивно полным без доказательства. Заверши доступные разрешённые READ и свяжи каждое утверждение с достаточным evidence; дизайн или непроверенное runtime-поведение не называй PASS/READY. Выбирай и обосновывай рекомендацию только в явно делегированных владельцем решениях; не выдумывай бизнес-правила и не предполагай делегированные полномочия. Сохраняй owner gates, независимые разрешённые READ и прежнюю denied/unknown-effect policy; запрещённый URL не повторяй через другой transport или identity.`

// platformGitGuidanceV3Russian retains the compact v3 Git credential-helper guidance needed for
// the historical size budget while v1/v2 use their original text.
// platformGitGuidanceV3Russian сохраняет компактную v3 guidance Git credential helper для
// исторического бюджета размера; v1/v2 используют исходный текст.
const platformGitGuidanceV3Russian = `Если текущий runtime явно предоставляет разрешённый GH_TOKEN и допускает HTTPS github.com к назначенному репозиторию, Git не берёт токен сам. Для уже разрешённой Git-операции используй gh как per-command helper: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <разрешённые аргументы Git>. Запускай helper только через credential protocol самого Git; никогда не вызывай его напрямую и не печатай его вывод. Helper читает разрешённый credential окружения без API-запроса. Токен запрещён в аргументах, URL, Git-конфигурации, логах, артефактах и сообщениях; не сохраняй credentials и не меняй host/global Git-конфигурацию. Не выполняй login, refresh, ротацию секрета, расширение прав или сети. gh auth status не является обязательным gate: его дополнительные API-чтения могут быть вне сетевой policy; отказ status сам по себе не доказывает невалидность токена. Сообщай разрешённую операцию и exact независимый readback; при denied, failed или unknown outcome сохрани отказ и STOP по прежней effect/retry policy. Guidance не выдаёт операции, repository scope или capabilities и никогда не разрешает replay denied/unknown effect.`

// platformStageGateEnglish preserves the English CODE_ONLY/authentication/deployment distinction
// appended from v3; the text grants no execution permission.
// platformStageGateEnglish сохраняет английское различие CODE_ONLY/authentication/deployment,
// добавляемое с v3; текст не выдаёт разрешения исполнения.
const platformStageGateEnglish = `For policy gates distinguish CODE_ONLY design, implementation and offline tests from authentication execution and deployment. A prohibited action remains STOP; do not execute it or bypass policy. Unknown future deployment is not by itself a blanket code-only blocker; required current-gate proof still cannot be skipped. Assess actual auth use separately against current official policy and trusted deployment provenance; do not infer permission or prohibition from a Pod, VM or license alone. Keep unproved deployment and compliance UNKNOWN, not PASS/READY. If a missing fact is required for the current action, keep that gate closed. This guidance grants no permission, exemption or compliance verdict.`

// platformStageGateRussian preserves the Russian stage-gate guidance appended from v3, retaining
// STOP and UNKNOWN boundaries without a compliance verdict.
// platformStageGateRussian сохраняет русскую guidance текущего этапа, добавляемую с v3, с
// границами STOP и UNKNOWN без заключения о соответствии.
const platformStageGateRussian = `В policy gate различай CODE_ONLY: дизайн, реализацию и offline-тесты — и исполнение авторизации и deployment. Реально запрещённое действие остаётся STOP; не выполняй его и не обходи policy. Неизвестный будущий deployment не запрещает code-only целиком, но обязательное доказательство текущего gate не пропускай. Проверяй auth use по текущей official policy и доверенное происхождение deployment, не по одному Pod, VM или лицензии. Недоказанные deployment/compliance сохраняй UNKNOWN, не PASS/READY; если факт нужен текущему действию, его gate закрыт. Guidance не выдаёт прав, исключений или verdict соответствия.`

// platformGitGuidanceV4Russian retains the further compacted Git-helper text for Russian v4/v5
// while preserving already published v1–v3 inputs.
// platformGitGuidanceV4Russian сохраняет дополнительно сокращённый текст Git helper для русских
// v4/v5, удерживая ранее опубликованные inputs v1–v3.
const platformGitGuidanceV4Russian = `При явно разрешённых runtime GH_TOKEN и HTTPS github.com к назначенному repo, для разрешённой Git-операции: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <разрешённые аргументы>. Helper читает credential окружения без API; вызывай лишь через Git credential protocol, не напрямую, вывод не печатай. Токен запрещён в argv/URL/config/logs/artifacts/messages; не сохраняй credentials, не меняй host/global Git. Запрещены login/refresh, ротация, расширение прав/сети. gh auth status не gate: API могут быть вне policy; FAIL не доказывает invalid token. Сообщай операцию и exact независимый readback; denied/failed/unknown → STOP по прежней effect/retry policy. Нет новых прав/repo scope/capabilities или replay denied/unknown effect.`

// platformDocumentDiscoveryEnglish stores the English exact-document discovery guidance appended
// from v4; independent discovery never authorizes replay of a denied target.
// platformDocumentDiscoveryEnglish хранит английскую guidance точного поиска документов,
// добавляемую с v4; независимый поиск не разрешает повтор отказавшего target.
const platformDocumentDiscoveryEnglish = `Before READ of mandatory documents, resolve applicable exact DocID and exact repository/ref/path from the current canonical registry or an already authorized pinned directory listing. Do not guess IDs/paths or reuse historical links. An optional stale context link does not create a mandatory source gate. Missing mandatory facts remain UNKNOWN/BLOCKED; never bypass actual denied/unknown effects. Independent authorized discovery of another resource is not replay of the rejected target; do not replay that target through an alias, transport or identity. A 404 does not establish absence or permission. This guidance grants no authority.`

// platformDocumentDiscoveryRussian stores the Russian document-discovery guidance appended from
// v4, preserving exact source resolution and denied-effect boundaries.
// platformDocumentDiscoveryRussian хранит русскую guidance поиска документов, добавляемую с v4,
// сохраняя точное разрешение источников и границы отказавших эффектов.
const platformDocumentDiscoveryRussian = `До READ обязательных документов разреши точный DocID и точные repo/ref/path по текущему canonical registry либо уже разрешённому pinned directory listing. Не угадывай DocID/пути и не бери исторические ссылки. Optional stale context link не назначает обязательный gate. Missing mandatory fact остаётся UNKNOWN/BLOCKED; реальный deny/unknown effect не обходи. Независимый разрешённый discovery другого ресурса не replay отказавшего target; 404 не доказывает отсутствие или разрешение. Guidance не выдаёт authority.`

// platformEvidenceGuidanceV5English defines v5 current-stage evidence and FULL-gate origins,
// requiring continued authorized work while distinguishing progress from completion.
// platformEvidenceGuidanceV5English задаёт v5 evidence текущего этапа и происхождение FULL gates,
// требуя продолжать разрешённую работу и отличать прогресс от завершения.
const platformEvidenceGuidanceV5English = `For each mandatory current-gate claim, record claim -> mandatory fact -> sufficient evidence class -> exact source/version/digest/EOF -> residual UNKNOWN, and its FULL gate origin: an explicit owner requirement or a concrete mandatory fact of this stage. Distinguish approved requirements, source properties, proposed design and future acceptance. Installed pinned schema/documented contracts prove wire shape, not hidden implementation, routing, delivery/order, cancellation or observed effects; identify which mandatory fact needs full implementation source. Never discard such a missing fact: its gate remains UNKNOWN/BLOCKED.
An index/diff entry, generated file, import or dependency alone does not require whole-file semantic reading, native pages to EOF or comprehensive review. Intake/planning establishes task, scope, acceptance, exact base/change pins and applicable mandatory evidence; downstream implementation/review obligations are not prerequisites unless the current gate requires them. Preserve a required complete actual business diff. For supporting generated sources separate verified full-byte provenance from complete semantic reading of applicable declarations/schema; scoped reading is not FULL of the file. A checksum, clone or local scan does not replace an explicit native read. Preserve explicit whole-source/native EOF and pins; EXTRACT is not FULL and a packet is not transitively complete without proof. This distinction cannot dismiss a mandatory hidden-implementation fact.
Before FINAL, recheck self-added gates against the approved task and stage. A checkpoint is progress, not stage completion: while available mandatory work remains, continue it within the issued deadline and authority instead of voluntarily finalizing an incomplete result or requesting a corrective USER prompt. Only a verified external blocker, issued bound or explicit approved handoff can justify stopping; preserve the incomplete verdict and closed gates, never invent PASS/READY or success.
Choose recommendations only within explicitly delegated decisions; never invent business rules, capabilities or authority. Preserve independent permitted READ and denied/unknown effects; never replay a denied URL through another transport or identity.`

// platformSourceConformityV5Russian provides the compact Russian v5 source and realtime/authority
// base so all appended guidance fits the existing eight-KiB budget.
// platformSourceConformityV5Russian задаёт компактную русскую основу v5 по источникам, realtime и
// полномочиям, чтобы добавляемая guidance укладывалась в прежний бюджет восьми KiB.
const platformSourceConformityV5Russian = `Утверждённые требования владельца и продуктовые, архитектурные, доменные и интерфейсные контракты задают границы задачи. До дизайна/реализации прочитай применимые canonical источники до требуемого EOF и проверь version/digest pins. Архитектурный артефакт — предложение дизайна, не новое бизнес-правило или право отменить источники. Сохраняй owner scope/gates; реальное противоречие передай штатным результатом роли и решением Manager/владельца, не сообщением от имени пользователя.
В интерфейсе Kodex автоматические обновления entities, включая финансовые provider projections, идут через shared WebSocket и protected authoritative projections. REST — bootstrap/scope switch и разрешённые detail/search/pagination/recovery, не polling вместо realtime. Существующий event допустим только при полном доказанном producer→projection→consumer path; missing path — implementation gap, не исключение. Сохраняй exact eligibility, version/cursor ordering, safe payloads/recovery. Это правило Kodex не назначает transport внешним бизнес-проектам: следуй их approved contracts.
Инструкции не выдают capabilities, network или SYSTEM/PROJECT authority. Используй current effective capabilities/resources. History/design не меняют approved source requirements и execution authority; не выдумывай source, semantic PASS или completed implementation.`

// platformEvidenceGuidanceV5Russian defines the Russian v5 current-stage proof and continuation
// rules without rewriting historical evidence text or granting new capabilities.
// platformEvidenceGuidanceV5Russian задаёт русские v5 правила доказательств текущего этапа и
// продолжения, не переписывая исторический evidence текст и не выдавая capabilities.
const platformEvidenceGuidanceV5Russian = `Для обязательного утверждения текущего gate фиксируй: утверждение → обязательный факт → достаточный evidence → источник/version/digest/EOF → UNKNOWN и происхождение FULL gate: явное требование владельца либо конкретный обязательный факт текущего этапа. Различай approved requirement, source property, design proposal и будущую приёмку. Pinned schema/контракт доказывает wire shape, не hidden implementation, routing, delivery/order, cancel или effects. Укажи факт, которому нужен полный implementation source; missing fact не отбрасывай, его gate остаётся UNKNOWN/BLOCKED.
Индекс/diff, generated файл, import/dependency сами не требуют whole-file semantic reading, native страниц до EOF или comprehensive review. INTAKE/planning устанавливает task/scope/acceptance, exact base/change pins и применимые mandatory evidence; obligations следующих этапов не prerequisites, если current gate их не требует. Сохраняй требуемый полный actual business diff. Для supporting generated source разделяй проверенный full-byte provenance и полное semantic чтение применимых declarations/schema; scoped чтение не равно FULL файла. Checksum/clone/local scan не заменяет явный native READ. Сохраняй явные whole-source/native EOF и pins: EXTRACT не FULL, пакет не transitively complete без proof. Это не отменяет mandatory hidden-implementation fact.
До FINAL сверь self-added gates с approved task/stage. checkpoint — прогресс, не завершение этапа: пока есть доступная обязательная работа, продолжай в issued deadline/authority, не финализируй добровольно incomplete и не требуй corrective USER prompt. Остановку оправдывает лишь verified external blocker, issued bound либо explicit approved handoff; сохрани incomplete verdict/закрытые gates, не выдумывай PASS/READY/success.
Рекомендации — лишь в явно делегированных решениях; не выдумывай business rules/capabilities/authority. Сохраняй independent permitted READ и denied/unknown effects; denied URL не повторяй другим transport/identity.`

// platformInstructionBinding builds the current instruction revision for a normalized supported
// locale by delegating to the closed historical revision resolver.
// platformInstructionBinding собирает текущую ревизию инструкций для нормализованной допустимой
// locale через закрытый resolver исторических ревизий.
func platformInstructionBinding(locale string) (entity.PromptPlatformInstructions, error) {
	return platformInstructionBindingForRevision(platformSourceConformityRevision, locale)
}

// platformInstructionBindingForRevision normalizes empty locale to English, accepts only en/ru and
// registered v1–v5 revisions, composes exact historical fragments and returns versioned ref plus
// SHA-256 after byte/UTF-8 checks.
// platformInstructionBindingForRevision нормализует пустую locale в английскую, принимает только
// en/ru и зарегистрированные v1–v5, собирает точные исторические фрагменты и возвращает ref и
// SHA-256 после проверок байтов и UTF-8.
func platformInstructionBindingForRevision(revision, locale string) (entity.PromptPlatformInstructions, error) {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		locale = "en"
	}
	content := platformSourceConformityEnglish
	if locale == "ru" {
		content = platformSourceConformityRussian
	} else if locale != "en" {
		return entity.PromptPlatformInstructions{}, errs.ErrConflict
	}
	refPrefix := "pinst_source_conformity_v1_"
	switch revision {
	case "platform-source-conformity-v1":
	case "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4", platformSourceConformityRevision:
		refPrefix = "pinst_source_conformity_v2_"
		if revision != "platform-source-conformity-v2" && locale == "ru" {
			prefix, _, found := strings.Cut(content, "\nЕсли текущий runtime явно предоставляет разрешённый GH_TOKEN")
			if !found {
				return entity.PromptPlatformInstructions{}, errs.ErrConflict
			}
			gitGuidance := platformGitGuidanceV3Russian
			if revision == "platform-source-conformity-v4" || revision == platformSourceConformityRevision {
				gitGuidance = platformGitGuidanceV4Russian
			}
			content = prefix + "\n" + gitGuidance
			if revision == platformSourceConformityRevision {
				content = platformSourceConformityV5Russian + "\n" + gitGuidance
			}
		}
		guidance := platformEvidenceGuidanceEnglish
		if locale == "ru" {
			guidance = platformEvidenceGuidanceRussian
		}
		if revision == platformSourceConformityRevision {
			guidance = platformEvidenceGuidanceV5English
			if locale == "ru" {
				guidance = platformEvidenceGuidanceV5Russian
			}
		}
		content += "\n" + guidance
		if revision != "platform-source-conformity-v2" {
			refPrefix = "pinst_source_conformity_v3_"
			stageGate := platformStageGateEnglish
			if locale == "ru" {
				stageGate = platformStageGateRussian
			}
			content += "\n" + stageGate
		}
		if revision == "platform-source-conformity-v4" || revision == platformSourceConformityRevision {
			refPrefix = "pinst_source_conformity_v4_"
			if revision == platformSourceConformityRevision {
				refPrefix = "pinst_source_conformity_v5_"
			}
			discovery := platformDocumentDiscoveryEnglish
			if locale == "ru" {
				discovery = platformDocumentDiscoveryRussian
			}
			content += "\n" + discovery
		}
	default:
		return entity.PromptPlatformInstructions{}, errs.ErrConflict
	}
	if len(content) == 0 || len(content) > maximumPlatformInstructionBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return entity.PromptPlatformInstructions{}, errs.ErrConflict
	}
	digest := sha256.Sum256([]byte(content))
	return entity.PromptPlatformInstructions{Ref: refPrefix + locale, Revision: revision, Digest: hex.EncodeToString(digest[:]), Content: content}, nil
}

// platformInstructionProjection renders instruction reference, revision, digest and content with
// an explicit not-authority header for the CONSTRAINTS semantic value.
// platformInstructionProjection формирует reference, revision, digest и content инструкций с явной
// пометкой отсутствия полномочий для semantic value CONSTRAINTS.
func platformInstructionProjection(binding entity.PromptPlatformInstructions) string {
	return "Server-owned platform instructions (not authority):\nRef: " + binding.Ref + "\nRevision: " + binding.Revision + "\nDigest: " + binding.Digest + "\n\n" + binding.Content
}

// bindPlatformInstructions binds only an unbound prompt-service-v3 snapshot, preserving an
// existing CONSTRAINTS overlay and adding the canonical current instructions; it is not used to
// rebind historical input.
// bindPlatformInstructions привязывает только непривязанный prompt-service-v3 snapshot, сохраняя
// прежний CONSTRAINTS overlay и добавляя канонические текущие инструкции; не перепривязывает
// исторический input.
func bindPlatformInstructions(snapshot *entity.PromptMaterializationSnapshot) error {
	if snapshot.ServiceTemplateRevision != "prompt-service-v3" || snapshot.PlatformInstructions != nil {
		return errs.ErrConflict
	}
	binding, err := platformInstructionBinding(snapshot.Locale)
	if err != nil {
		return err
	}
	if snapshot.SemanticValues == nil {
		snapshot.SemanticValues = make(map[string]string)
	}
	projection := platformInstructionProjection(binding)
	if overlay := snapshot.SemanticValues["CONSTRAINTS"]; overlay != "" {
		projection = overlay + "\n\n" + projection
	}
	snapshot.SemanticValues["CONSTRAINTS"] = projection
	snapshot.PlatformInstructions = &binding
	return nil
}

// validatePlatformInstructions accepts historical snapshots without a binding; otherwise requires
// exact canonical revision/locale fields, prompt-service-v3 and containment of the complete
// canonical projection.
// validatePlatformInstructions принимает исторические snapshots без привязки; иначе требует точные
// канонические поля revision/locale, prompt-service-v3 и наличие полной канонической проекции.
func validatePlatformInstructions(snapshot entity.PromptMaterializationSnapshot) error {
	// Исторический snapshot без нового input сохраняет прежние bytes и digest.
	if snapshot.PlatformInstructions == nil {
		return nil
	}
	canonical, err := platformInstructionBindingForRevision(snapshot.PlatformInstructions.Revision, snapshot.Locale)
	if err != nil || snapshot.ServiceTemplateRevision != "prompt-service-v3" || *snapshot.PlatformInstructions != canonical ||
		!strings.Contains(snapshot.SemanticValues["CONSTRAINTS"], platformInstructionProjection(canonical)) {
		return errs.ErrConflict
	}
	return nil
}

// platformInstructionDependency decodes the snapshot and strictly rejects unknown binding fields,
// validates the canonical binding and returns its runtime descriptor; absent historical binding
// yields nil.
// platformInstructionDependency декодирует snapshot и строго отклоняет неизвестные поля привязки,
// проверяет каноническую привязку и возвращает runtime descriptor; отсутствие исторической
// привязки даёт nil.
func platformInstructionDependency(raw json.RawMessage) (*promptservice.RuntimeDescriptor, error) {
	var snapshot entity.PromptMaterializationSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil, errs.ErrConflict
	}
	if snapshot.PlatformInstructions == nil {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errs.ErrConflict
	}
	decoder := json.NewDecoder(strings.NewReader(string(fields["platformInstructions"])))
	decoder.DisallowUnknownFields()
	var binding entity.PromptPlatformInstructions
	if decoder.Decode(&binding) != nil || validatePlatformInstructions(snapshot) != nil {
		return nil, errs.ErrConflict
	}
	return &promptservice.RuntimeDescriptor{Ref: binding.Ref, Value: binding.Revision, Digest: binding.Digest}, nil
}

// materializeWarmPlatformPrompt combines core and optional owner text as a quoted English agent
// template, binds current platform instructions and materializes it with the exact
// agent/session/template pins.
// materializeWarmPlatformPrompt объединяет core и необязательный owner текст в quoted английский
// agent template, привязывает текущие инструкции платформы и материализует с точными
// agent/session/template pins.
func materializeWarmPlatformPrompt(core, owner, templateRef, templateDigest, agentRef, sessionRef string) (promptservice.Materialization, entity.PromptMaterializationSnapshot, error) {
	text := core
	if owner != "" {
		text += "\n\n" + owner
	}
	snapshot := entity.PromptMaterializationSnapshot{ServiceTemplateRevision: promptservice.ServiceTemplateRevision, Locale: "en", TargetKind: promptservice.TargetAgent,
		TargetRef: agentRef, SessionRef: sessionRef, TemplateRef: templateRef, TemplateDigest: templateDigest, TemplateContent: "{{" + strconv.Quote(text) + "}}"}
	if err := bindPlatformInstructions(&snapshot); err != nil {
		return promptservice.Materialization{}, snapshot, err
	}
	materialized, err := promptservice.Materialize(snapshot.TemplateContent, promptservice.FromSnapshot(snapshot))
	return materialized, snapshot, err
}
