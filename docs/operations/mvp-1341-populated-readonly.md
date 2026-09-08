---
id: OPS-DOC-1341
title: Точечная приёмка наполненных интерфейсов
status: approved
type: operations
owner: manager
version: 1.0.0
updated: 2026-09-08
---

# Точечная приёмка наполненных интерфейсов

Refs #1341, #1260, #1031. Профиль дополняет OPS-DOC-1260: отдельный PASS
означает только измеренный вариант. Все обязательные ветки 64 строк сохраняются.
Live требует отдельного GO и свежей исключительной API session от оператора.
Текущий прототип остаётся в hot-reload профиле; оснастка не требует пересборки
application image или смены source соседей ради общего SHA.

## Выбор существующих данных

`KODEX_E2E_UI_FIXTURE_MANIFEST` указывает на абсолютный канонический путь к
обычному private файлу (0600, не symlink, не более 64 KiB). Схема закрытая:
`schemaVersion: 1`, `fixtures` — от 1 до 32 записей с уникальным `slot`.
Обязательны `slot`, `kind`, `ref`, `version`; дополнительные поля ниже.
Файл и значения refs/query/source не включаются в журнал. В журнал попадает
только SHA256 файла. Пример использует вымышленные, не готовые live данные:

```json
{"schemaVersion":1,"fixtures":[
  {"slot":"project-primary","kind":"PROJECT","ref":"project_fixture","version":1},
  {"slot":"assistant-primary","kind":"ASSISTANT","ref":"conversation_fixture","version":1,"query":"Fixture"},
  {"slot":"configuration-primary","kind":"CONFIGURATION","ref":"configuration_fixture","version":1,"configurationKind":"PROMPT_TEMPLATE"}
]}
```

`projectRef` обязателен для AGENT/WORKFLOW/RUN/VFS; ENVIRONMENT требует его
для inspector. CONFIGURATION требует `configurationKind` из ROLE_IMAGE,
PROMPT_TEMPLATE, INTEGRATION_DEFINITION. Для org configuration projectRef
отсутствует. Для project configuration он должен точно совпасть с ответом.
ASSISTANT может иметь projectRef. VFS дополнительно требует точный `path`
внутри `/projects/<projectRef>` и `lifecycleState` ACTIVE/DELETED/ARCHIVED;
допустима реальная version=0. Нельзя передавать actor, authority или credentials.

Перед использованием pin выполняется authenticated authoritative GET:
projects/agents/workflows/runs/runtime-environments по exact ref;
configuration — через revisions с current configuration; assistant и VFS —
через scoped страницы, максимум восемь. Проверяются ref/version/project/kind,
для VFS также parentPath/lifecycleState. Чужой scope или version drift,
403, 404, пустая страница и исчерпанный бюджет дают отдельный NOT RUN.
401 останавливает зависимые действия. Контрактный malformed ответ и network
failure остаются FAIL. Эти исходы не доказывают permission revoke UI lifecycle.

Ни один pin не создаёт или не изменяет fixture. `CREATE_PROJECTS=1` вместе
с manifest отвергается до browser. Старый discovery без manifest остаётся
доступен для прежнего профиля, но не заменяет exact pins наполненного профиля.

## Закрытые варианты

К прежним targeted и form IDs добавлен реестр `e2e/ui-populated-variants.ts`.
Неизвестный ID отвергается при загрузке public config.

| Selection ID | Pin / предусловие | Измеряемый результат |
| --- | --- | --- |
| `assistant-history-search-{ru,en}-{1440,768,390}` | assistant-primary, query | История независимо от composer: exact search, open, отсутствующий query, clear, close |
| `fixture-assistant-history-pagination` | assistant-primary, непустой cursor | Те же действия и следующий exact server cursor |
| `fixture-environment-inspector` | environment-primary | Выбор строки exact editor href, inspector, Escape |
| `fixture-configuration-history` | configuration-primary | История exact configuration, непустые revisions, Escape/route close |
| `fixture-configuration-source-keys` | configuration-primary, source permission | Tab/ShiftTab и локальный undo возвращают digest текста, Save запрещён |
| `fixture-role-image-project-source` | configuration-project-role-image, projectRef | Source-only редактор новой формы в точном проекте; без создания/Save/build |
| `fixture-global-search-debounce` | search-project, PROJECT + query | Естественные ≥500 ms до GET, новый query/Enter, clear, route change без старых результатов |
| `fixture-global-search-{project,agent,workflow,run}` | search-<kind>, соответствующий kind + query | Exact link route и detail GET данного ref |
| `fixture-vfs-active`, `fixture-vfs-trash` | vfs-active ACTIVE / vfs-trash DELETED | Exact ref/version, selection/Escape, query/clear, counts/total |
| `fixture-vfs-pagination` | vfs-active с cursor | Следующая disjoint страница, stable total, selection/search |
| `fixture-kanban-pages` | project-primary, наполненные четыре колонки с cursors | Раздельные states/cursors каждой колонки, foreign scope/duplicate отклонены |
| `project-{0,1}-environment-inspector` | project pins + matching ENVIRONMENT | Выбор exact environment в выбранном проекте |
| `configuration-history-{prompt-template,role-image,integration-definition}` | matching CONFIGURATION | Exact history без выбора первого произвольного каталожного объекта |
| `project-eight-sections` | project pin | Восемь project routes |
| `project-picker-async-search`, `kanban-independent-scroll`, `global-search-enter-clear` | прежние предусловия OPS-DOC-1260 | Ранее недоступные selector шаги широкого профиля |

Shared lists ≤6, expanded collections и keyboard/Escape/focus также остаются
отдельными существующими readonly form IDs. Успешная пустая оболочка списка
не заменяет populated pagination. Недостающие fixtures/cursors — NOT RUN.
История source здесь не доказывает задержанный ответ после permission revoke;
это отдельный обязательный вариант. Preview POST не включён. Ни Run,
provider/device/STT, ни rename/archive/new chat, ни fixture create не разрешены.

## Публичная команда

Из `services/staff/control-center`, после root выдачи env с точными значениями:

```bash
KODEX_E2E_UI_CREATE_PROJECTS=0 \
KODEX_E2E_RUN_TIMEOUT_MS=900000 \
KODEX_E2E_UI_VARIANTS=fixture-configuration-history,fixture-configuration-source-keys \
npx playwright test --config e2e/ui-acceptance.config.ts
```

Обязательные env прежнего профиля: KODEX_E2E_BASE_URL,
KODEX_E2E_STORAGE_STATE (API-only private state), KODEX_E2E_RESOURCE_PREFIX,
KODEX_E2E_CONFIRM_DISPOSABLE, KODEX_E2E_UI_EVIDENCE_DIR (новый private каталог),
KODEX_E2E_SOURCE_REVISION (harness), KODEX_E2E_API_REVISION,
KODEX_E2E_PWA_REVISION, KODEX_E2E_SERVING_MANIFEST_SHA256.
KODEX_E2E_BROWSER — chromium/firefox/webkit; каждому свой процесс и session.
KODEX_E2E_UI_FIXTURE_MANIFEST обязателен для новых `fixture-*` вариантов.
Без него вариант выдаёт NOT RUN/MISSING, не выбирает случайные данные.
Бюджет 60–1800 s; один worker, retries=0, trace/video/screenshot отключены.

Без live/session проверяется public entrypoint:

```bash
KODEX_E2E_CHECK_ONLY=1 KODEX_E2E_UI_CREATE_PROJECTS=0 \
KODEX_E2E_UI_VARIANTS=fixture-vfs-active \
npx playwright test --config e2e/ui-acceptance.config.ts --list
```

## Причинность и безопасный журнал

Только в этом opt-in browser профиле wrapper сохраняет native fetch и signal,
добавляя неавторитетный `X-Kodex-E2E-Read-ID` для same-origin GET/HEAD `/api/v1/`
и уже разрешённого exact effective-access/query POST. Body guards сохраняются;
ни ticket, ни refresh, ни разрешения сервера не меняются. Внутри памяти
сопоставляются уникальный request identity, метод/адрес, native START,
AbortSignal AbortError и rejected promise либо точный navigation intent,
созданный раньше failure для уже начатого pending request. Код отмены сам по
себе ничего не разрешает. Late abort, timeout, duplicate identity, method
mismatch и socket reset остаются unexplained failure. Лимит 4096 events/requests;
overflow закрывает PASS. Console CORS и HTTP ошибки не исключаются.

Каждый variant record содержит browser, fixtureManifestSHA256, versions,
requirement IDs, timestamp UTC и закрытые численные/boolean assertions.
Журнал сохраняет rawFailedRequests отдельно от confirmedCancellations.
Отдельная bounded запись read-network содержит closed route/method/resourceType,
request sequence, stage, error enum/hash и признаки native identity/signal/navigation.
Ни один сырой адрес, query, header или текст ошибки в эту запись не попадает.
`ui-evidence-ledger.ts` строит проекцию по browser + requirement + variant +
fixture digest: поздний Chromium PASS не стирает Firefox/WebKit FAIL.
Исходные журналы append-only; исходный FAIL и история сохраняются.
Полный MVP-ID не становится PASS из суммы частичных наблюдений.

## Локальная проверка и пределы

```bash
npx vitest run e2e
npm run typecheck
npm run build:synthetic
npx playwright test --config e2e/ui-populated.fixture.config.ts
```

Профиль synthetic имеет budget 300 s, workers=3 и retries=0. Он использует
настоящие браузеры, Vue AssistantWorkspace/CodeEditor/VfsBrowser/RunsPage и
контролируемые HTTP fixtures, включая native abort и network reset. Эти
fixtures не являются vendor/live evidence. Точные исходы и SHA — в PR.
Актуальный Context7 `/microsoft/playwright/v1.61.0` проверен для request failure,
route.fetch/fulfill и отсутствия автоматического retry при maxRetries=0.
