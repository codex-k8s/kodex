---
id: OPS-DOC-1419
title: Приёмка UI lifecycle шаблонов и IntegrationDefinition
type: operations
status: approved
owner: sre
version: 1.1.0
updated: 2026-09-09
---

# Приёмка UI lifecycle шаблонов и IntegrationDefinition

Профиль закрывает воспроизводимый staging-путь создания собственных
`PROMPT_TEMPLATE` и `INTEGRATION_DEFINITION` без Git. Это дополнительное
доказательство для `MVP-UI-15`, `MVP-UI-30`, `CFG-02` и применимой части
`CFG-03`; оно не закрывает остальные варианты этих строк.

## Граница

Все изменения выполняет браузер через реальные формы Control Center. API-only
сессия используется только для авторитетного `GET` readback. Профиль не создаёт
connections/grants, не запускает provider, Run, STT или email и не изменяет
чужие объекты. Он использует только уникальные имена с operator-owned prefix.

`PROMPT_TEMPLATE` проходит create, validate, publish и history. Forward restore,
copy и archive у этого вида отсутствуют в текущем специализированном UI/API и
не отмечаются как выполненные. Create требует exact выбранный Проект; один ref
связывает navigation query, HTTP body, command authority и последующий GET
readback. `ROLE_IMAGE` использует ту же project-required границу.

`INTEGRATION_DEFINITION` проходит Form/YAML, create, validate, publish, history,
создание нового forward draft из выбранной опубликованной revision, copy и
archive собственной копии. Этот вид остается organization-wide: caller не
передаёт project ref, а operation policy фиксирует `project_required=false`.
Restore не меняет published pointer и не перепривязывает consumers. Изменение
policy и gateway доставляется согласованно с exact readback обслуживаемого
operation binding; частичное обновление не является готовым rollout.

## Безопасные входы

Для live-запуска нужны:

- отдельный API-only `storageState`, подготовленный штатным setup;
- exact HTTPS origin;
- SHA исходников harness, API и PWA;
- приватный serving manifest и его SHA-256;
- новый приватный journal path в каталоге `0700`;
- уникальный prefix длиной 4–40 символов;
- exact ref активного синтетического Проекта, предварительно проверенного
  авторитетным `GET /api/v1/projects/{projectRef}`;
- tracked `contracts/integrations/v1/definitions/synthetic.yaml`.

Журнал имеет mode `0600`. Перед каждой UI mutation он синхронно записывает
`intent` и вызывает `fsync`. В нём остаются только закрытый operation, sequence,
HTTP status, версии и SHA-256 входа/request/ref/published pointer. Исходники,
prompt, cookies, CSRF, headers, ответы и персональные данные не сохраняются.

## Запуск

Сначала получить отдельную API-only сессию штатным `api-session` setup. Затем
из точного checkout выполнить:

```bash
cd services/staff/control-center
KODEX_E2E_CONFIGURATION_LIFECYCLE_CONFIRM=RUN_CONFIGURATION_UI_LIFECYCLE \
KODEX_E2E_CONFIGURATION_LIFECYCLE_STATE=/absolute/private/new-state.jsonl \
KODEX_E2E_SERVING_MANIFEST=/absolute/private/serving-manifest.json \
KODEX_E2E_STORAGE_STATE=/absolute/private/api-session.json \
KODEX_E2E_RESOURCE_PREFIX=<unique-prefix> \
KODEX_E2E_CONFIGURATION_LIFECYCLE_PROJECT_REF=<exact-synthetic-project-ref> \
KODEX_E2E_BASE_URL=https://control.kodex.works \
KODEX_E2E_SOURCE_REVISION=<40-hex> \
KODEX_E2E_API_REVISION=<40-hex> \
KODEX_E2E_PWA_REVISION=<40-hex> \
KODEX_E2E_SERVING_MANIFEST_SHA256=<64-hex> \
npx playwright test --config e2e/configuration-lifecycle.config.ts
```

`workers=1`, `retries=0`, trace/video/screenshot выключены. При неизвестном
исходе команда не повторяется. Для отдельного readback того же journal:

```bash
cd services/staff/control-center
KODEX_E2E_CONFIGURATION_LIFECYCLE_RESUME=1 \
<те же exact scope variables> \
npx playwright test --config e2e/configuration-lifecycle.config.ts
```

Resume выполняет только `GET`, сопоставляет точное deterministic имя,
kind/state/version/published pointer и завершает pending intent как `PASS` либо
`UNKNOWN`. Продолжение mutation требует отдельного решения после авторитетного readback.
Новый prefix не разрешает повтор неизвестного effect; автоматического повтора
после `UNKNOWN` нет.

## Исходы

- `PASS`: принят HTTP command и совпал авторитетный readback либо resume нашёл
  состояние, однозначно доказывающее исход.
- `REJECTED`: получен terminal HTTP 4xx; следующий intent не создаётся.
- `UNKNOWN`: нет ответа или HTTP 5xx, либо readback не доказывает эффект.
- `NOT RUN`: live-профиль не запускался на обслуживаемом staging candidate.

Платный или внешний provider effect всегда `NOT RUN` в этом профиле.

## Проверка session boundary и диагностика #1424/#1425/#1432

До первого intent выполняются GET session с проверкой authoritative срока,
GET exact активного Проекта и GET отсутствия всех трёх собственных
deterministic fixtures. Ответ 401
останавливает запуск до create; сессия и политика не продлеваются оснасткой.
PWA продолжает обычный coordinated refresh через разрешённый PUT session.

Соседний приватный файл `<journal>.diagnostics.json` содержит только serving
versions, закрытые session endpoint/status, относительные сроки от serverTime,
SHA-256 ref и безопасные version/lifecycle exact Проекта, а также безопасные
console digests. Cookie, CSRF, actor, generation, ticket,
исходники, transcript и сырые ошибки туда не попадают. Старые journals не
перезаписываются, неизвестные console ошибки остаются FAIL. Безопасный session
trace также сохраняется в обычном readonly UI acceptance.

Перед повтором прежнего отвергнутого `stop1422-cfg` оператор отдельно выполняет
GET поиска имён `stop1422-cfg-prompt`, `stop1422-cfg-integration` и
`stop1422-cfg-integration-copy`. Наличие объекта требует readback, не нового
create. `UNKNOWN` не разрешает новый prefix как способ повторить effect.
Штатный безопасный режим: с теми же scope variables, старым prefix и НОВЫМ
journal path задать `KODEX_E2E_CONFIGURATION_LIFECYCLE_READ_ONLY=1`.
Он выполняет только пять GET (session, exact Проект и три поиска), сохраняет
boolean наличия, name digest/version и завершает работу без UI mutation. Старый
journal не изменяется, отсутствие найденного имени само по себе не закрывает
неизвестный effect.

Для естественного refresh используется существующий
`e2e/session-renewal.config.ts`: fresh session, настоящий server renewAfter,
две вкладки и исходная absolute boundary; время не перематывается. Дополнительно
readonly `ui-acceptance.config.ts` на свежем и естественно состаренном отдельном
state сохраняет session trace возле console evidence. Оба live результата
остаются NOT RUN до фактического запуска root.

Context7: `/microsoft/playwright`, документация BrowserContext storageState,
Route.abort и ConsoleMessage.location проверена 2026-09-09. Она не доказывает
причину исторических #1416/#1424 или успешную живую приёмку #1425.
