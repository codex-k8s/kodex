---
id: TOOL-ROLE-IMAGE-NATIVE-PLAN-1797
title: Подготовка complex RoleImage и четырёх ENV без исполнения
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-10
---

# Назначение

`role-image-native-plan.mjs` — узкий первый этап штатного upgrade: из exact
приватного owner readback готовит native POST/PATCH intents, но никогда не делает HTTP,
runtime launch/retry, USER_MESSAGE, risk decision или запись owner state.
Он не заменяет авторизацию CP и не объявляет offline snapshot текущей authority.
Existing `role-image-forward-upgrade.mjs` остаётся намеренно FROM-only fixture;
его ограничения и журналы не меняются.

Поддержан один organizational recipe + два recipes одного project, четыре
ENV (одна organizational + три того же project), по38 tools. Рефы конкретной
установки не зашиты. JSON source должен быть полным, доступным и published.
Dockerfile может содержать реальные RUN/USER/install инструкции, но только
одну exact FROM, как canonical CP validator. Меняется лишь FROM base; полный
прочий source остаётся семантически тем же. Base reference берётся из exact
catalog entry по environmentKey и expected runner digest: internal registry
имя не заменяется внешним pull origin даже при совпадающем digest.
При сверке optional полей учтена native projection: пустой installationBlock
опускается, а source packageKeys/toolKeys null,[] или отсутствие равны пустому
Go slice. Непустые значения сравниваются точно; неправильные типы/keys и null
в public projection отвергаются. Это только сравнение: PROJECT raw draft body
сохраняет source null/''. ORG использует описанный ниже exact typed UPDATE и
server-owned canonical source reconstruction без потери settings.

## CLI

Вход — regular single-link файл текущего UID,0600,≤8MiB, SHA256 exact bytes.
Выход — новый файл0600 в существующем real private каталоге0700 текущего UID.
Symlink/overwrite/неприватные права и неизвестные аргументы закрываются.
В stdout только статус/count/hash; source, ENV values, Secret descriptors и
request bodies остаются в private файле. Он не содержит credential values.
Owner transport добавляет штатный CSRF/session при отдельно разрешённом effect.

```sh
node tools/dev/role-image-native-plan.mjs recipes \
  --input /private/recipes.json --input-sha256 HEX64 --output /private/recipe-intents.json
node tools/dev/role-image-native-plan.mjs environments \
  --input /private/environments.json --input-sha256 HEX64 --output /private/environment-intents.json
node tools/dev/role-image-native-plan.mjs selections \
  --input /private/selections.json --input-sha256 HEX64 --output /private/publication-intents.json
node --test tools/dev/role-image-native-plan.test.mjs
```

Input SHA64 — hash всего файла, не JSON-normalized content. Intent сохраняет
стабильный idempotency key от exact body/path/versions/pins; новое имя output
не меняет key. Никакого `apply` нет. Output не является command receipt.

## Входные readback envelopes

Все records — полные ответы существующего native API, без удаления полей
для удобства planner. Expected selectors задаёт owner target allowlist, не
authority. Контракты — `contracts/openapi/control-api-gateway/v1/openapi.yaml`.

`recipes` имеет ровно `{version:1,catalog,runnerDigest,recipes:[3 entries]}`.
`catalog` — GET `/api/v1/role-environments`; runnerDigest — `sha256:HEX64`.
Каждая entry ровно `{expected,detail,historyPages}`:

- expected: `{organizationRef,scopeKind,projectRef,recipeRef,configurationRef,publishedRevisionRef}`; projectRef пустой только при ORGANIZATION.
- detail: GET `/api/v1/organization/role-image-recipes/{recipeRef}` либо `/api/v1/projects/{projectRef}/role-image-recipes/{recipeRef}`.
- historyPages: все страницы GET `/api/v1/managed-configurations/{configurationRef}/revisions?pageSize=50`, в cursor порядке до EOF. Configuration pin одинаков на всех страницах; published source digest проверяется; existing незакрытый draft останавливает подготовку.

Result для двух PROJECT — POST `/api/v1/role-image-configurations/drafts` с
If-Match configuration.version и полным source, затем штатная validation→
publication с fresh configuration OCC. Общий drafts endpoint требует PROJECT;
он не используется для ORG и искусственный projectRef не назначается.
Для ORG — PATCH `/api/v1/organization/role-image-recipes/{recipeRef}` с body
`{name,environment}` и If-Match recipe.version. Это existing frontend
`RoleImageEditor.save → updateRoleImage → updateSystemRoleImageRecipe` и
owner RPC `ManageOrganizationRoleImageRecipe` action UPDATE. Owner transaction
атомарно создаёт DRAFT→VALID→PUBLISHED managed source и build; отдельных
validation/publication/build commands нет. Source JSON должен точно содержать
name/roleImage, roleDefinitionRef/environment и все пять environment fields.
Любые extra/missing fields, изменение имени и [] вместо canonical null arrays
закрываются: typed ORG path не вправе молча терять/нормализовать исходник.
Полный complex Dockerfile и package/tool/install сохранены; null arrays не
отправляются в non-nullable public selection, но сервер сохраняет nil как null
в новой managed revision. Private expectedPublishedSource и nextContentSHA256
задают exact Go map JSON (sorted keys и HTML/U+2028/U+2029 escaping) для
обязательного authoritative published-source readback.
Publication/ORG UPDATE уже создаёт build: второй REQUEST_BUILD не нужен.
Дождаться exact новой
managed revision/build generation, admission ACCEPTED и реальной promotion.
HUMAN OWNER/ADMIN risk-decision остаётся отдельным явным шлюзом; машина не
выбирает action/reason и не переписывает REJECTED. ORG promotion path singular
`/organization/role-image-recipes/{recipeRef}/promotion`; PROJECT plural
`/projects/{projectRef}/role-image-recipes/{recipeRef}/promotions`.

`environments` имеет ровно `{version:1,environments:[4 entries]}`.
Каждая entry ровно `{expected,environment,artifactDetail,bindings}`:

- expected: `{organizationRef,scopeKind,projectRef,environmentRef,environmentVersion,baseVersionRef,recipeRef,publishedRevisionRef}`. environmentVersion — wrapper `environment.version`, не currentVersion.version/revision.
- environment: GET `/api/v1/runtime-environments/{environmentRef}`; ACTIVE и ready=true, пустые readinessBlockers. enabled в этом projection отсутствует. ORG projectRef может быть опущен; это только canonical empty scope, PROJECT ref обязателен.
- artifactDetail: новый GET exact recipe после promotion; activeArtifact новой managed revision, ACTIVE recipe и exact activeArtifact/ref/promotedReference, ACCEPTED/PROMOTED/VERIFIED, immutable version/build/provenance/promotion pins. Каждый прежний command всех38 tools обязан быть VERIFIED executable basename на каждой платформе нового image; declaredTools — не источник допуска.
- bindings: только intended exact pins `{organizationRef,scopeKind,projectRef,consumerRef,consumerVersion,bindingRef,bindingVersion,sourceRevisionRef}`. Native `/agents/{agentRef}/runtime-configuration` имеет top-level agentVersion и environmentBinding `{agentRef,digest,environmentRef,ref,version,versionRef}`: consumerRef=environmentBinding.agentRef, consumerVersion=agentVersion, bindingRef=environmentBinding.ref, bindingVersion=environmentBinding.version, sourceRevisionRef=environmentBinding.versionRef. Нет выдуманного agent object. Scope берётся из authoritative owner resource, не payload. SYSTEM floating binding сверять по materialized current version, не назначать пустой ref.

Result — четыре POST organization/project `runtime-environment-drafts`.
ENV name/description,38 полных tool objects, values, ресурсы/volumes/web policy
сохранены; Secret bindings имеют exact revision>0 из старых descriptors.
Никаких defaults/current Secret revision или policy ослабления. Старые UID/RV/
hash private descriptors остаются baseline pin в comparison digest; их не
включать как writable binding fields. Owner отдельно выполняет draft validation
и POST `/runtime-environment-drafts/{draftRef}/impact-plans` с fresh draft OCC.
Не применять перед этим RoleImage consumer-bindings: это альтернативный clone
путь, который создаст лишнюю ENV revision и сделает draft base stale.

### Унаследованные инструменты и verified inventory

FROM-only recipe может иметь declaredTools=[] и полноценные инструменты базы.
Authoritative `resolveScopedRuntimeEnvironmentImage` выбирает их через
`verifiedImageToolCommands` (`platform/role_images_inventory.go`): только
basename VERIFIED path и пересечение всех платформ. Нельзя подставлять
declarations, использовать display name (ripgrep/typescript вместо rg/tsc),
объединять платформы или считать MISSING/PROBE_FAILED доступным executable.

Planner повторяет этот закрытый public profile: полный фиксированный набор50
observations в canonical порядке, registered probe paths/required flags,
известные statuses, версии/хэши и одна либо две различные linux/amd64|arm64
платформы. Inventory status/hash, imageDigest и provenanceSha256 должны быть
точно связаны с новым artifact; platformDigest/manifestSha256 обязательны.
Исторический UNAVAILABLE и неверные/неполные/чужие evidence закрываются.
Пустой declaredTools сохраняется пустым: он не заполняется fixtures или
invented recipe tools. Artifact ref/version/build, promotion receipt и
inventory SHA/projection SHA/platform pins включены в immutable request pins
и стабильный idempotency key;38 полных прежних tool objects остаются в body.

Safe public projection не содержит spec/build/runtime manifest fields,
поэтому planner не заявляет повторную криптографическую проверку inventory
SHA по этому DTO. Владелец уже проверяет raw inventory/hash/image/provenance/
spec/build/runtime/platform bindings при authoritative hydration и повторяет
current artifact eligibility при ENV validation/publication. Private native
input digest и offline pins не заменяют этот owner predicate или authority.

`selections` имеет ровно `{version:1,selections:[4 entries]}`.
Каждая entry ровно `{draft,pages,intendedBindings}`:

- draft: current native GET `/runtime-environment-drafts/{draftRef}`, именно VALID.
- pages: полные GET `/revision-impact-plans/{planRef}?pageSize=50` до EOF, без query-фильтра; immutable plan/digest, total, source/draft/target pins и expiresAt проверяются.
- intendedBindings: прежний exact allowlist bindings, как в environments; не весь список consumers и не refs из имён.

Result — четыре POST `/runtime-environment-drafts/{draftRef}/publication` с
`{planRef,selectedItemRefs}` и If-Match current draft.version. RoleImage
`impactDigest` и legacy consumers здесь недопустимы. После отдельно разрешённой
publication HTTP200 не доказывает успешные bindings: нужен fullEOF readback
APPLIED/CONFLICT/FORBIDDEN/NOT_SELECTED и exact ENV/runtime binding preservation.

## Матрица жизненного цикла

| Этап | Planner / owner эффект | Guards и authoritative read |
| --- | --- | --- |
| Offline prepare | Только новый private intent файл; events/grants отсутствуют | Exact hashed input; owner native state проверяется до отдельно разрешённого POST |
| PROJECT native draft/validate | Старые publication/ENV/turn неизменны | Owner до configuration OCC, sourceEditable/UI, VALID и digest; exact receipt/GET |
| ORG typed UPDATE | Owner atomic new generation + managed DRAFT→VALID→PUBLISHED + build/audit/event/receipt | OWNER/ADMIN organization.manage и image source/build eligibility, recipe OCC; UI/not archived/no existing draft; exact source shape и после эффекта source digest/lineage readback |
| Publication/autobuild | Owner atomic revision/generation/build/audit/event | Fresh VALID/version/catalog; exact recipe/build lineage |
| Admission/risk/promotion | Existing worker lifecycle; human risk отдельно | Technical evidence/fence/current policy; rejected/failed не обходятся |
| ENV draft/impact | Publication ещё не меняется | Wrapper/base pins,38 tools, exact Secret; immutable prepublication fullEOF plan |
| Inherited tool eligibility | Planner только проверяет pinned public evidence; state/event отсутствуют | VERIFIED basename intersection всех платформ, exact inventory/image/provenance и promotion pins; declarations не fallback; owner повторяет raw evidence/current eligibility до effect |
| Selected publication | Owner создаёт ENV revision и только выбранные binding effects | Fresh plan/draft/artifact/source/consumer pins; per-item readback |
| Cancel/archive/delete/discard | Planner не создаёт такие intents | Только существующий owner lifecycle; terminal history не переписывается |
| Retry/lease expiry | Planner не повторяет executor work | Existing bounded attempt/fence cleanup; no invented grant/claim |
| Unknown HTTP / restart | Offline файл не ACK и не разрешение повторить POST | Exact native receipt/readback; новый key не назначать наугад |
| Expired/stale plan или revocation | Closed preparation/owner rejection | Новый current readback/план только штатно; old history сохранена |
| Next turn/continuation | Вне planner | Свежая server-owned RuntimeRevision; immutable текущий turn не меняется |

HTTP400 invalid,403 forbidden,404 hidden,409 state,412 OCC,503 unavailable и502
invalid projection не получают автоматического повторения/ослабления. Проверка
права и текущего источника выполняется владельцем до выдачи idempotent receipt.
Planner offline не утверждает успешный rollout или свежесть полномочий.
