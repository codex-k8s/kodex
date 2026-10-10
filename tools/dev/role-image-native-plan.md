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
приватного owner readback готовит POST intents, но никогда не делает HTTP,
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
в public projection отвергаются. Это только сравнение: source поля null/'' не
удаляются и не заменяются defaults в draft body.

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

Result — три exact POST `/api/v1/role-image-configurations/drafts` с If-Match
configuration.version и полным source. Владелец отдельно выполняет штатную
validation→publication (fresh configuration OCC из receipts). Publication
уже создаёт build: второй REQUEST_BUILD не нужен. Дождаться exact новой
managed revision/build generation, admission ACCEPTED и реальной promotion.
HUMAN OWNER/ADMIN risk-decision остаётся отдельным явным шлюзом; машина не
выбирает action/reason и не переписывает REJECTED. ORG promotion path singular
`/organization/role-image-recipes/{recipeRef}/promotion`; PROJECT plural
`/projects/{projectRef}/role-image-recipes/{recipeRef}/promotions`.

`environments` имеет ровно `{version:1,environments:[4 entries]}`.
Каждая entry ровно `{expected,environment,artifactDetail,bindings}`:

- expected: `{organizationRef,scopeKind,projectRef,environmentRef,environmentVersion,baseVersionRef,recipeRef,publishedRevisionRef}`. environmentVersion — wrapper `environment.version`, не currentVersion.version/revision.
- environment: GET `/api/v1/runtime-environments/{environmentRef}`.
- artifactDetail: новый GET exact recipe после promotion; activeArtifact новой managed revision, ACCEPTED/PROMOTED/VERIFIED, immutable provenance/promotion pins. Каждый прежний command всех38 tools обязан быть declared в новом artifact.
- bindings: только intended exact pins `{organizationRef,scopeKind,projectRef,consumerRef,consumerVersion,bindingRef,bindingVersion,sourceRevisionRef}`. Agent/binding versions брать из current `/agents/{agentRef}/runtime-configuration`, sourceRevisionRef из effective binding versionRef. SYSTEM floating binding сверять по materialized current version, не назначать пустой ref.

Result — четыре POST organization/project `runtime-environment-drafts`.
ENV name/description,38 полных tool objects, values, ресурсы/volumes/web policy
сохранены; Secret bindings имеют exact revision>0 из старых descriptors.
Никаких defaults/current Secret revision или policy ослабления. Старые UID/RV/
hash private descriptors остаются baseline pin в comparison digest; их не
включать как writable binding fields. Owner отдельно выполняет draft validation
и POST `/runtime-environment-drafts/{draftRef}/impact-plans` с fresh draft OCC.
Не применять перед этим RoleImage consumer-bindings: это альтернативный clone
путь, который создаст лишнюю ENV revision и сделает draft base stale.

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
| Native draft/validate | Старые publication/ENV/turn неизменны | Owner до OCC, sourceEditable/UI, VALID и digest; exact receipt/GET |
| Publication/autobuild | Owner atomic revision/generation/build/audit/event | Fresh VALID/version/catalog; exact recipe/build lineage |
| Admission/risk/promotion | Existing worker lifecycle; human risk отдельно | Technical evidence/fence/current policy; rejected/failed не обходятся |
| ENV draft/impact | Publication ещё не меняется | Wrapper/base pins,38 tools, exact Secret; immutable prepublication fullEOF plan |
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
