---
id: FE-MC-CC-001
title: Staff Control Center Kodex
type: frontend-guide
status: approved
owner: manager
version: 3.0.1
updated: 2026-08-26
---

# Staff Control Center

`services/staff/control-center` — основной production web-интерфейс
Kodex. Через него владелец и участники работают с Проектами,
ИИ-сотрудниками, Процессами, запусками, решениями человека, файлами,
автоматизациями, интеграциями, доступом и аудитом. Mattermost, GitHub,
Kubernetes и другие внешние системы не требуются для core-сценариев.

PWA обращается только к browser-facing `control-api-gateway`:

- HTTP source contract —
  `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- WebSocket source contract —
  `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`.

Generated clients не редактируются вручную. Pages собирают feature stores и
UI-компоненты, а business lifecycle, `nextActions`, полномочия, causality графа
и terminal state вычисляет control-plane. В приложении нет fake data,
production mocks, ручного ввода внутренних идентификаторов или второго
источника доменных правил.

## Локальная разработка

```bash
npm ci
npm run codegen
npm run dev
```

Runtime config загружается из `/config/runtime-config.json` до создания Vue
application. Parser закрыто отклоняет HTTP, credentials в URL, query,
fragment, несовпадающие HTTP/WebSocket origins и timeout вне допустимого
диапазона. Deployment-домен задаётся только конфигурацией установки.

OIDC Authorization Code + PKCE хранит временный protocol state в
`sessionStorage`. Bearer применяется один раз при `createOwnerSession`, после
чего browser использует `Secure`/`HttpOnly` host-only session cookie. Control
Center раз в пять минут вызывает `PUT /api/v1/session`; её 15-минутный idle
TTL продлевается только после полностью успешной проверки OIDC/Origin/CSRF
boundary и никогда не выходит за абсолютный срок исходного bearer. Для
`kodex-control-center` bearer живёт 3600 секунд при realm default
300 секунд; OAuth2 Proxy browser cookie и API session остаются независимыми
слоями. Logout отменяет и дожидается текущего renewal, а gateway оставляет
подписанный HttpOnly tombstone до expiry bearer, чтобы запоздавший renewal из
любой вкладки не восстановил закрытую browser session. Mutation adapter
добавляет CSRF token, `Idempotency-Key` и, где
требуется, authoritative `If-Match`. Backend problem detail не показывается:
UI выбирает безопасный локализованный текст по `Problem.code`.

## Пользовательские маршруты

| Route                                          | Назначение                                                                |
| ---------------------------------------------- | ------------------------------------------------------------------------- |
| `/onboarding`                                  | first-run и проверка горячего Системного помощника                        |
| `/`                                            | глобальная сводка активной работы и ожидающих решений                     |
| `/projects`                                    | каталог и создание Проектов                                               |
| `/projects/:projectRef`                        | обзор выбранного Проекта                                                  |
| `/projects/:projectRef/agents`                 | ИИ-сотрудники Проекта                                                     |
| `/projects/:projectRef/agents/:agentRef`       | профиль, инструкции, capabilities, образ роли и запуск сотрудника         |
| `/projects/:projectRef/workflows`              | Процессы Проекта                                                          |
| `/projects/:projectRef/workflows/:workflowRef` | настройка, публикация и запуск Процесса                                   |
| `/projects/:projectRef/runs/new`               | прямой запуск сотрудника или Процесса                                     |
| `/runs`, `/projects/:projectRef/runs`          | глобальный и project-scoped список запусков                               |
| `/runs/:runRef`                                | live graph, timeline, Human Gate, artifacts, cancel, retry и continuation |
| `/projects/:projectRef/files`                  | входные файлы, знания и результаты                                        |
| `/projects/:projectRef/automations`            | расписания сотрудников и Процессов                                        |
| `/integrations`                                | необязательные connections, tests, capabilities и grants                  |

Kodex не имеет отдельного полноэкранного маршрута: на каждом каноническом
экране он открывается через FAB как контекстный desktop drawer или mobile
bottom sheet. Новый диалог получает server-resolved route/entity context.
| `/decisions` | долговечные Human Gates |
| `/administration/access`, `/projects/:projectRef/members` | organization и project access |
| `/administration` | platform capabilities и диагностика установки |
| `/administration/audit` | аудит действий и ошибок конфигурации |

Экранный контракт и утверждённые HTML-макеты перечислены в
`docs/design/mockups/index.md`.

## Realtime и состояние

Platform stream доставляет ограниченные authoritative snapshots общих
коллекций. Для запуска используется resumable stream:

1. graph snapshot и текущий `sequence`;
2. ordered deltas из NATS-backed durable event source;
3. duplicate игнорируется;
4. gap запускает catch-up;
5. неполный catch-up заменяется свежим snapshot;
6. более старый HTTP или WebSocket результат не перезаписывает новое состояние.

Stores нормализуют runs, nodes, edges, events, gates, artifacts и sessions по
opaque refs. Raw provider response, JSONL, stdout/stderr, secret values и
содержимое больших файлов в WebSocket не передаются.

При offline UI явно показывает последний полученный state и блокирует owner
actions. Кнопок ручного обновления нет: после reconnect клиент восстанавливает
пропущенные события сам.

## Browser E2E

Playwright suite работает только с реальной одноразовой установкой. Она не
перехватывает HTTP/WebSocket, не создаёт production test mode и не использует
mock server. Перед запуском оператор обязан явно подтвердить disposable scope.

Установить закреплённый Chromium:

```bash
npx playwright install chromium
```

Создать защищённый SSO bootstrap state через фактический cold OIDC login:

```bash
export KODEX_E2E_BASE_URL='https://<disposable-origin>'
export KODEX_E2E_OWNER_USERNAME='<disposable-owner-login>'
export KODEX_E2E_OWNER_PASSWORD='<read-without-printing>'
export KODEX_E2E_STORAGE_STATE="$PWD/.auth/owner.json"
export KODEX_E2E_CONFIRM_DISPOSABLE='I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION'
npm run test:e2e:auth
```

Значения login/password не добавляются в log, trace или repository. Bootstrap
содержит только OAuth2/Keycloak SSO cookies: Kodex API session и CSRF cookies
перед записью удаляются. Файл должен быть regular, принадлежать текущему
пользователю, иметь mode `0600` и находиться в owner-каталоге `0700` без
symlink; JSON ограничен по размеру и проверяется по закрытой schema. Запись
выполняется атомарно. Trace и video принудительно отключены.

Запустить web-only сценарии на fresh installation без connections:

```bash
export KODEX_E2E_PROFILE='web-only'
export KODEX_E2E_RESOURCE_PREFIX='<unique-lowercase-slug>'
npm run test:e2e
```

Suite проверяет реальный OIDC вход, hot System Assistant, не-IT Проект отдела
продаж, создание и образ роли ИИ-сотрудника, прямой запуск, artifact download,
session continuation, typed assistant action и audit, nested workflow с двумя
дочерними агентами, Human Gate one-winner, WebSocket reconnect/catch-up,
cancel/retry lineage, mobile navigation и работу core без integrations.
Перед каждым тестом его свежий browser context выполняет warm SSO flow и
создаёт отдельную Kodex API session. Дочерние contexts Human Gate winner и
contender создаются из актуального state основного context, а не из bootstrap
файла.

Для optional-Mattermost профиля disposable installation заранее получает два
Mattermost connection без grant к тестовым ресурсам:

- рабочее подключение с фактически материализованным credential и включёнными
  `mattermost.notifications`/`mattermost.result_mirror`;
- outage-подключение, последний authoritative test которого был успешным, но
  его endpoint во время сценария фактически недоступен.

Оба подключения остаются необязательными для core readiness. Для независимого
readback публикации используется отдельный токен disposable Mattermost только
на чтение тестового канала. Значение находится в regular non-symlink файле с
mode `0600`, не передаётся через env и не выводится в reporter:

```bash
export KODEX_E2E_PROFILE='mattermost'
export KODEX_E2E_RESOURCE_PREFIX='<unique-lowercase-slug>'
export KODEX_E2E_MATTERMOST_ORIGIN='https://<disposable-mattermost-origin>'
export KODEX_E2E_MATTERMOST_TOKEN_FILE='<owner-only-token-file>'
export KODEX_E2E_MATTERMOST_TEAM_NAME='<test-team-name>'
export KODEX_E2E_MATTERMOST_CHANNEL_NAME='<test-channel-name>'
export KODEX_E2E_MATTERMOST_HEALTHY_CONNECTION='<exact-control-center-name>'
export KODEX_E2E_MATTERMOST_OUTAGE_CONNECTION='<exact-control-center-name>'
npm run test:e2e
```

Сценарий выдаёт точные grants созданному ИИ-сотруднику, проверяет реальный post
и result mirror через Mattermost API, отдельный `INCIDENT_LINKED` при outage и
неизменное состояние `SUCCEEDED` core Run.

`npm run test:e2e:check` выполняет TypeScript-проверку и перечисляет тесты без
сети, browser binary и credentials для обоих профилей. Это не считается
фактическим E2E PASS.

Поддерживаемый оркестратор полного локального контура запускается из корня
репозитория. Он требует точный non-production context, делегирует
render/deploy/readback существующему `dev.sh`, выполняет auth smoke и полный
browser E2E, а затем сохраняет отдельный redacted summary без credentials:

```bash
./dev.sh full-e2e --context default \
  --resource-prefix local-acceptance-001 \
  --target test-integration-synthetic
```

`--check` выполняет только preflight и `test:e2e:check`, не разворачивая стенд.
`--skip-build` допускается только для уже готового точного local profile и
заменяет `up` на authoritative `status`/readback. Дополнительные цели передаются
повторяемым `--target` и ограничены Make-целями `test-*`; их собственные env и
credential files оркестратор не копирует в summary. Итог находится в
`.kodex-dev/e2e/<resource-prefix>-summary.json` с mode `0600`.

Перед локальным `up` проверяется ревизия реестра Secret projections и NATS
material contract. При drift disposable namespaces `kodex-system` и `identity`
пересоздаются автоматически вместе с installation material. Локальные
`credentials.env`, `provider-accounts/` и `cache/` сохраняются; другие
namespaces, включая соседние проекты, reconciler не адресует.

## Codegen и быстрые проверки

```bash
npm run codegen
npm run format:check
npm run lint
npm run typecheck
npm run test:unit
npm run test:e2e:check
npm run build
```

Повторный `npm run codegen` должен оставлять TypeScript generated diff чистым.
AsyncAPI generator также пишет Go-модели; из корня репозитория выполнить
`make gen-control-api-gateway-asyncapi`, который применяет `gofmt` и проверяет
канонический generated contract. Ручная правка generated файлов запрещена.

## Матрица приёмки MVP #1022

Проверенный накопленный code checkpoint:
`f36f9df41ba256ee8581fe8dde045b238d7093b7`.
Локально PASS: production build с typecheck, unit 821/821 в 168 файлах,
полный lint, format:check, отдельный TypeScript check E2E и synthetic
Playwright 20/20. Synthetic покрывает 1280/1440/1920/2560/2900, 900 и 390;
новые model/history/file-selection/impact/voice сценарии выполняются
на 1440/390. Console/network assertions прошли. Скриншоты этого запуска
сохранены в `/tmp/kodex-1022-f36f9df41-synthetic.tgz`; это архив локальных
безопасных fixtures, не staging и не проверка реального provider.
Повтор OpenAPI generation на принятом HTTP `03564b5f4` дал пустой generated
diff; после него исходный контракт не менялся. CI/live/staging/Safari:
NOT RUN. Полный unit остаётся незавершённым по перечисленным ниже контрактным
зависимостям; 20 тестов не означают 61 успешный приёмочный сценарий.

Матрица связывает обязательный scope с кодом и проверками, но не заменяет
итоговый отчёт exact SHA. Наличие строки не означает завершённую приёмку.
Все пути ниже относительны `services/staff/control-center`; `*.test.ts` рядом
с модулем выполняются через `npm run test:unit`.

| Критерии                                                            | Реализация                                                                                                                                              | Проверка / незакрытая часть                                                                                                                                                   |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01, 07, 08, 53, 54: переводы, PWA, ошибки, поиск                    | `src/app/i18n`, `src/features/search`, `src/shared/api/search-result.ts`, `src/shared/config`                                                           | Search/session/PWA unit; полнота переводов всех экранов ещё проверяется                                                                                                       |
| 02, 03, 04, 06, 10, 12, 13: shell, Home, проекты                    | `src/app/AppShell.vue`, `src/pages/HomePage.vue`, `src/pages/ProjectsPage.vue`, `src/features/projects`                                                 | `e2e/synthetic.spec.ts`; дополнительные detail viewport ещё проверяются                                                                                                       |
| 05, 19, 22, 27: ограниченные списки, selectors, глобальные каталоги | `src/shared/ui/AsyncEntityPicker.vue`, `src/features/catalogs`, `src/features/managed-configurations/ConfigurationCatalog.vue`                          | Picker unit; synthetic catalog; распространение bounded/expand на все списки ещё выполняется                                                                                  |
| 09: модальный ассистент                                             | `src/features/assistant`                                                                                                                                | API/store unit и `e2e/fixtures/assistant-history.ts`: cursor, scope, search/state, archive OCC и read-only архив; debounce/отмена/неопределённая команда проверены отдельно   |
| 11: продление сессии                                                | `src/features/session/store.ts`, `src/features/realtime`                                                                                                | Session renewal/backoff unit; exact expiry/reconnect после интеграции требует отдельной проверки                                                                              |
| 14, 20, 55-60: общий voice и Tab                                    | `src/shared/ui/VoiceTextarea.vue`, `VoiceInputButton.vue`, `CodeEditor.vue`, `code-editor-keymap.ts`, `src/features/speech`, `src/shared/api/speech.ts` | Voice/lease/API unit, `e2e/voice.synthetic.spec.ts`, bootstrap STT в `e2e/synthetic.spec.ts`; real-provider smoke только у владельцев backend                                 |
| 15, 16, 21, 23, 30, 31: инструкции, preview, runtime и полномочия   | `src/features/agents/detail`, `src/features/runtime`                                                                                                    | Существующие detail/runtime unit; сквозная матрица user∩agent и все preview ещё проверяются                                                                                   |
| 17, 18: модели и reasoning                                          | `src/features/providers`, `src/features/agents/detail`                                                                                                  | `model-catalog.test.ts`, `e2e/models.synthetic.spec.ts`: account-scoped SDK, поиск, cursor, исчезнувший ID; persist/readback effort и catalog revision/digest ещё отсутствуют |
| 24-26, 37, 61: Kanban, файлы, VFS и workspace                       | `src/features/workboard/components/RunsBoard.vue`, `src/features/files`, `src/features/context-resources`                                               | VFS/context API unit, synthetic Skill/Memory lifecycle и exact binding readback; runtime write proof принадлежит runner/runtime                                               |
| 28, 29, 32, 33, 38: карточки, аватары, запуск, решения              | `src/features/agents/catalog`, `src/pages/WorkflowsPage.vue`, `WorkflowDetailPage.vue`, `DecisionsPage.vue`                                             | Базовые workboard/agent unit; полная ручная приёмка ещё не выполнена                                                                                                          |
| 34-36, 43: workflow/continuation/automation prompts, cron           | `src/features/automations`, `src/pages/WorkflowDetailPage.vue`, `src/features/runs`                                                                     | Schedule model/API/editor unit; все materialization previews ещё подключаются                                                                                                 |
| 39-42: типизированные интеграции и Human Gate                       | `src/features/integrations`, `src/features/managed-configurations/ConfigurationFields.vue`                                                              | Integration/typed YAML unit, synthetic details/form/YAML; server Gate policy и SMTP/IMAP отсутствуют в текущем contract                                                       |
| 44-48: environment draft/impact/rebind                              | `src/pages/RuntimeEnvironmentsPage.vue`, `RuntimeEnvironmentEditorPage.vue`, `src/features/runtime`                                                     | Draft/reauth unit; `revision-impact.test.ts`; synthetic save/reload/validate/publish/discard и выборочный environment rebind на 1440/390                                      |
| 49, 50: редактор и нормализация Secret                              | `src/features/runtime-secrets`, `src/features/runtime/SecretImpactDialog.vue`                                                                           | Base64/page-normalization/store unit; `revision-impact.test.ts`; synthetic JSON editor и rebind окружения без агентов                                                         |
| 51, 52: provider delete/verify/reauth                               | `src/features/providers`                                                                                                                                | Store/model unit; DELETE возвращает REVOKED, terminal cleanup/impact ещё не представлен контрактом                                                                            |
| UI-managed RoleImage/IntegrationDefinition                          | `src/features/managed-configurations`, `src/features/role-images`                                                                                       | Model/document unit, synthetic draft→validate→publish; полный build/provenance/source ownership ещё проверяется                                                               |
| Mattermost: административная привязка внешней identity              | `src/features/integrations/interaction-identities.ts`, `ui/InteractionIdentitiesPanel.vue`, `src/pages/IntegrationsPage.vue`                            | `interaction-identities.test.ts`: OCC bind/revoke и closed receipt; `e2e/fixtures/interaction-identities.ts`: list/create/revoke. Live NOT RUN                                |

Дополнение к интеграционным зависимостям:

- Mattermost bind фиксирует exact connection version через `If-Match`, revoke
  использует version identity. Actor не вводится пользователем. Для target
  нужен активный `USER` с активным platform membership; существующий
  `listAccessSubjects(kind=USER)` поддерживает query/cursor, но не фильтрует
  platform membership. Требуются eligible selector, team/channel каталоги и
  каноническое правило SHA256 внешнего user ID. Наличие записи не означает
  готовности inbound: решение принадлежит серверу.
- `RuntimeEnvironmentDraft` в checkpoint `04957a990` не содержит времени
  последнего server save и номера base revision. `expectedEnvironmentVersion`
  не подменяет номер immutable revision; локальное время не выдаётся за
  серверное. Save/validate/publish/discard и защита несохранённых изменений
  подключены к реальному draft API. Selective rebind подключён к HTTP #1045:
  `revision-impact.ts`, `EnvironmentImpactDialog.vue`, `SecretImpactDialog.vue`.
  `revision-impact.test.ts` проверяет OCC, исходную/целевую ревизии, запрет
  чужой выборки, неполную квитанцию и публикацию окружения без агентов.

### Локальная синтетическая проверка

Адресная передача #1045 исполнителю Meitner (`01a06dee-d29a-72c2-8d22-3a67d150c8a7`):

| Приоритетный контракт | Требуемые данные для PWA                                                                                                                                                                                                                                                                                                                      |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Mattermost identity   | HTTP list/bind/revoke получены и подключены. Остались eligible USER selector с активным platform membership, team/channel и правило SHA256 внешнего user; текущая форма принимает готовый lowercase SHA256                                                                                                                                    |
| Полные параметры STT  | Получены и подключены; bootstrap `speechTranscription` остаётся единственным источником пользовательской eligibility, config readiness её не заменяет. Каталог совместимых моделей ещё отсутствует                                                                                                                                            |
| Skills/Memory         | HTTP `441564286` и CP Skill lifecycle/binding получены. UI подключает bindings из runtime configuration, If-Match agent version и обязательный readback после mutation. Реальные scanner/runtime и staging acceptance остаются у владельцев backend                                                                                           |
| VFS lifecycle         | Eligibility/nextActions, состояние active/trash и версии узлов для выбора и массовых операций; текущий VfsNode не даёт права выводить их из вида узла                                                                                                                                                                                         |
| Home                  | Server-filtered cursor-каталоги active/failed/continuable runs и owner gates; поиск и totals. Организационные assistant artifacts не подменяют глобальные результаты проектов                                                                                                                                                                 |
| RoleImage recipes     | `listRoleImageRecipes` сейчас имеет только `roleDefinitionRef/pageSize/pageToken`; нужны server `query/state` и связь recipe/build с managed ownership, не сопоставление по имени                                                                                                                                                             |
| Model/reasoning       | HTTP `11401f0ac` получен: model catalog account/query/page подключён через `ProviderModelSelector`. Модель сохраняется отдельной от default runtime profile; старый ID не подменяется. Остаются catalog revision/digest и reasoning effort в publish/readback immutable runtime configuration; отдельный overlay не выдаётся за этот контракт |
| Workflow user∩agent   | `PlatformCapability` из `/platform-capabilities` не содержит user eligibility; `availableWithoutIntegration` не является полномочием. Нужен authoritative effective capability catalog для текущего пользователя и конкретного agent/шага; неизвестные полномочия нельзя выводить из имени роли                                               |

Точка интеграции HTTP/SDK `441564286` (локальный cherry-pick `c2adafe95`),
CP dependencies `695ae1e15`/`ae9cb517f` (локально `1a6f50310`/`55e7b65ce`).
Ранее интегрирован `e9eeaaeac` (merge `1a948c1a0`); generated TS принимается только через
commit #1045 с генерацией из суммарного OpenAPI. Этот список фиксирует запрос,
а не подтверждает доставку сообщения в другой тред или наличие нового HTTP.

Уточнение Home/Kanban после `11401f0ac`: CP уже имеет
`ListRunsRequest.states` (proto:1962) и `ListOwnerGatesRequest.state`
(proto:2045), grpc/queries.go:272/334 передаёт их дальше. HTTP
query_endpoints.go:182/233 и SDK эти поля не выводят. Нужны typed
states[]/state в существующих `/runs` и `/owner-gates`; totals и query
owner gates остаются отдельным producer gap.

Дополнительно приняты committed checkpoints: Skill/Memory VFS `4004bd66c`
с predecessors (локальный `fbc965c94`), email SDK `c61bcdca3` с CP `d31cd4c70`
(локально `37feb5c1b`/`85ec98e11`), immutable managed Save/Discard `43cdb2792`
с CP dependencies `3380a7e98`/`23bc30d65`/`f7c2d2ecb` (локальный HTTP `094628325`).
Receipt-bound email session `5c32fa683` принят как `e075e1247`.
Собственная PWA-реализация ещё не является завершённым unit.

HTTP `055d8e050` и `11401f0ac` приняты поверх чистого PWA как `267a7007e`
и `60bfaab72`, handwritten файлы сохранены. Повторная OpenAPI-генерация
проверяется из суммарного контракта. `src/features/providers/model-catalog.ts`
и `ProviderModelSelector.vue` подключают поиск/cursor и точную проверку модели
для каждой выбранной account; `AgentRuntimePanel.vue` не ограничивает выбор
default моделью runtime profile. `model-catalog.test.ts` проверяет scope,
cursor, отмену и исчезновение ID; `e2e/models.synthetic.spec.ts` прошёл на
1440/390, включая замену модели, пустой поиск и несовместимую вторую account.
Ручная приёмка: выбрать доступную account и модель вне default профиля,
сохранить, перечитать runtime configuration; отозвать доступ и убедиться,
что ID остаётся видимым, а подтверждение заблокировано.
Типизированный publish/readback reasoning effort и свежесть provider snapshot
не подтверждены этим synthetic-сценарием; см. D1..D7 в
`docs/operations/http-surface-1045.md` от корня репозитория.
После подключения моделей: production build с typecheck PASS, unit 809/809
в 168 файлах PASS, focused model browser 2/2 PASS, повтор OpenAPI generation
без generated diff. Остальные browser-сценарии на этом изменении повторно
не запускались; предыдущие результаты привязаны к предыдущим checkpoint.

История ассистента потребляет pageSize=40/pageToken из HTTP `11401f0ac`.
`src/features/assistant/api.ts`, `store.ts` и `AssistantWorkspace.vue` добавляют
догрузку, сброс cursor при смене project, отмену чтения при закрытии и scoped
readback вместо использования первой глобальной страницы как полной истории.
Выбранный диалог при refresh ищется по cursor в пределах 30 страниц;
повтор cursor, чужой project и превышение бюджета закрыто отклоняются.
Профильный unit-набор ассистента: 57 PASS; synthetic 1440/390: 2 PASS.
Ручная приёмка: открыть историю с более чем 40 диалогами, догрузить страницу,
выбрать старый диалог, дождаться realtime refresh; затем сменить project во
время догрузки и убедиться, что прежние результаты не добавляются.
HTTP `03564b5f4` потреблён как `087960d39`: D1 использует настоящий
query/state ACTIVE/CLOSED/ARCHIVED и специализированную archive-команду.
Поиск отменяет старое чтение, сбрасывает cursor и выбор диалога, выполняется
через 500 ms; поле несохранённого сообщения не перезаписывается. Архивирование
требует явного подтверждения, exact version и idempotency; неизвестный исход
не повторяется до нового чтения. Архивный диалог, вложения и планы read-only.
`api.test.ts`/`store.test.ts` проверяют фильтры, debounce/cancellation,
OCC/receipt и неопределённый исход; synthetic D1 lifecycle прошёл на 1440/390.

D7 подключён в трёх impact-диалогах: environment, secret и managed configuration.
Используются server query, pageSize=40, cursor и total; смена поиска очищает
старую выборку, повтор cursor и изменившийся snapshot закрыто отклоняются.
`e2e/impact.synthetic.spec.ts` проверяет каждый диалог на 1440/390:
две страницы, поиск с новым cursor, сброс checkbox и exact selective rebind.
Fixture `impact.html` не включена в production build. Эти шесть synthetic
сценариев проверяют PWA с безопасными HTTP fixtures, не CP SQL или live authority.
Через Context7 `/websites/vuejs` сверены watcher cleanup, AbortController,
flush sync и освобождение timers при закрытии/unmount.

### Передача текущего checkpoint D3/D5

Сохранён связный checkpoint предыдущего исполнителя; работа над полным #1022
продолжается. HTTP D3 `2b7a0c18d` принят как `76ec96f59`,
exact HTTP D5 `cfb18a17e2048f5056ddd46c8fccbd3f1e18a3d6` как `d53ee870f`.
Generated SDK вручную не редактировался.

| Критерий                                                                                 | Файл                                                                                                                                      | Проверка                                                                                                            |
| ---------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| D3 available/reason, точный agent/runtime context, запрет недоступной вставки            | `src/features/agents/detail/{api,model}.ts`, `AgentInstructionsPanel.vue`, `TemplateVariableCatalog.vue`, `src/pages/AgentDetailPage.vue` | `api.test.ts`, `model.test.ts`; `e2e/checkpoint.synthetic.spec.ts` на 1440/390 px                                   |
| D5 CA/username/auth secret, byte limits, exact OCC/idempotency, safe descriptor          | `src/features/integrations/email-credentials.ts`, `ui/EmailMailboxCredentialPanel.vue`, `src/pages/IntegrationsPage.vue`                  | `email-credentials.test.ts`, `ui/EmailMailboxCredentialPanel.test.ts`; тот же browser scenario                      |
| Очистка ввода до ответа, отсутствие voice, неизвестный исход без автоматического повтора | Те же D5 файлы                                                                                                                            | Unit: mismatch, поздний ответ после закрытия, redacted error; browser: пустое поле после save, отсутствие микрофона |

Локально для содержимого этого checkpoint: targeted unit **40 PASS / 4 файла**,
typecheck + production build **PASS**, lint **PASS**. Targeted synthetic
**2 PASS**: 1440/390 px, screenshots, page overflow, console/network.
Production build сохраняет предупреждение о главном chunk >500 kB.
При передаче повторены 40 targeted unit, production build/typecheck,
lint/format и два synthetic сценария 1440/390: PASS. Для совместимости с
интеграционной JSON Schema handwritten `PackageFieldSchema.const` допускает
JSON scalar, включая boolean; schema и generated validator не менялись.
Полные unit/synthetic, повторный codegen и live/staging на этом checkpoint
**NOT RUN**; предыдущие полные результаты выше не перенесены на новый SHA.

Воспроизведение из каталога PWA:

```bash
npm run test:unit -- src/features/agents/detail/api.test.ts src/features/agents/detail/model.test.ts src/features/integrations/email-credentials.test.ts src/features/integrations/ui/EmailMailboxCredentialPanel.test.ts
npm run build
npm run lint
npx vite build --config vite.synthetic.config.ts
npx playwright test --config playwright.synthetic.config.ts e2e/checkpoint.synthetic.spec.ts
```

Ручная приёмка: в инструкциях агента открыть каталог, проверить причины
недоступности и запрет вставки, сменить контекст и поиск. В деталях EMAIL
поочерёдно отправить три типа credential, проверить пустое поле после отправки,
safe descriptor и новую connection version после authoritative refresh.
Пробелы не обрезаются; CR/LF допустимы только в CA. При сетевой неопределённости
команда не повторяется сама: повтор требует заново введённого точного значения,
сохраняет исходные If-Match и Idempotency-Key. Значение и digest не сохраняются
в browser storage/Pinia/логах; digest попытки существует только в памяти формы.

Остаток для нового manager:

- D3 не заменяет effective user∩agent capability projection. Каталог готовности
  не доказывает runtime materialization; D4 exact target/diff остаётся зависимостью.
- D5 credential сохранён отдельно от mailbox publication: D5-config отсутствует.
  После закрытия/перезагрузки формы теряется контекст неопределённой попытки;
  durable recovery/read path требует отдельного согласованного решения, без
  сохранения credential или имитации authoritative receipt.
- D2: version/digest model catalog и persisted reasoning effort; D4: exact
  AGENT/WORKFLOW_STEP/SCHEDULE_DRAFT preview и RuntimeRevision diff;
  D6: server-side staged SecretDraft lifecycle.
- Остальные контрактные пробелы таблицы выше сохраняются: VFS lifecycle,
  Home server states, Mattermost selector eligibility, RoleImage ownership/build,
  UI/GIT IntegrationPackage execution. Полная MVP-UI-01..61 и staging-приёмка
  остаются незавершёнными; новые блоки в рамках этой передачи не реализовывались.

Локальный checkpoint `adeed52b71eca951f2104220d5c69b95c6c575ce` на базе `e075e1247`: `npm run typecheck`,
`npm run build` и `npm run test:unit` прошли (796 тестов, 165 файлов).
Повторный `npm run codegen` побайтово воспроизвёл весь
`src/shared/api/generated`. Build сообщает о JS chunk больше 500 kB,
AsyncAPI parser рекомендует более новую версию спецификации; предупреждения
не скрыты настройками. Synthetic UI прошёл 1280/1440/1920/2560/2900,
дополнительный 900 и mobile 390. Полные detail lifecycle проверяются только
на 1440/390; эти запуски не являются полной приёмкой 61 критериев,
staging, CI или проверкой Safari.

После checkpoint `npm run build` с typecheck прошёл повторно,
`npm run test:unit` прошёл с 804 тестами в 167 файлах. После отдельной
`npm run build:synthetic` полный Playwright synthetic прошёл: 10/10 тестов.
Существующие detail-сценарии расширены на все пять обязательных desktop-ширин
и mobile; дополнительный 900 проверяет сокращённый набор. Fixture выполняет
`SESSION_READY` и `PLATFORM_READY`, проверяется состояние realtime `live`.
Предыдущий запуск прерван при обнаружении отсутствующего `PLATFORM_READY`
в fixture и не учитывается как полный PASS. Новый
`e2e/fixtures/organization-catalog.ts` проверен на всех семи ширинах: server grouping,
шесть строк, длинный текст, cursor reset после `AGENT_CHANGED`, project-scoped
expand и серверный поиск. Отдельные unit `OrganizationCatalog.test.ts`
проверяют отмену in-flight запроса, отказ membership reload и logout;
`EmailEffectPanel.test.ts` проверяет timeout без autoretry и ручной decision
readback. `i18n/index.test.ts` разбирает статические ключи Vue/TS штатными
компиляторами; динамические server-owned ключи остаются отдельной проверкой.
Browser-проверка общего picker на 1440/390 покрывает стрелки после фокуса
на варианте, Enter/Space, пропуск disabled-варианта, отсутствие запроса
заблокированного inline-списка и повторную загрузку после разблокировки.
`e2e/fixtures/file-selection.ts` дополнительно прошёл на 1440/390:
checkbox использует объявленное `nextActions` до загрузки impact, перед
подтверждением проверяется exact impact, запрет блокирует команду.
Переключение списка/сетки сохраняет выбор и доступно на mobile без overflow.
Дополнительная responsive-проверка требует ширину имени файла больше 100 px
на 390 и скрытый desktop-header: команды вынесены в отдельную строку.
Существующие segmented controls Files/VFS и FORM/YAML используют общий
design-system стиль. Профильные file unit: 31 PASS; synthetic 1440/390:
2 PASS; production build с typecheck PASS.
Предыдущие mobile-запуски выявили скрытый переключатель и intrinsic-ширину
grid (FAIL); после исправления повторный запуск обоих размеров PASS.
Файловые model/API/layout unit: 23 PASS; synthetic build с typecheck PASS.
Context7 проверен для Vue compiler-sfc `parse/compileTemplate` и Pinia
`$onAction/after/onError` с unsubscribe; API дополнительно сверены с локальными
типами и browser-сценарием.

Текущий `PlatformResourceKind` не содержит отдельных invalidation kinds для
environment, secret, managed configuration, Skill и Memory. Пять каталогов в `OrganizationCatalog`
перечитываются на существующих `PROJECT/MEMBERSHIP/PLATFORM_MEMBERSHIP` и полном
resync; новые event kinds в PWA не выдумываются. Для адресного realtime этих
видов нужен producer/AsyncAPI contract и рабочий gateway mapping.

| Критерий                                       | Файл                                                                        | Тест и ручная приёмка                                                                                                                                                                                                                                                                |
| ---------------------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Immutable Save/Discard четырёх managed kinds   | `src/features/managed-configurations/api.ts`, `ConfigurationEditor.vue`     | `drafts.test.ts`; synthetic prompt сохраняет пустой текст и новую revision, IntegrationPackage показывает старую DISCARDED и exact discard. Вручную: сохранить пустой draft, проверить новый ref/parent, открыть историю, отбросить новый draft; опубликованная revision не меняется |
| Email UNKNOWN и отдельное решение владельца    | `src/features/integrations/email-effects.ts`, `ui/EmailEffectPanel.vue`     | `email-effects.test.ts`, `e2e/fixtures/email-effects.ts`. Вручную: открыть email connection, найти exact invocation receipt, выбрать подтверждённый исход, подтвердить решение; первоначальная UNKNOWN receipt остаётся в аудите                                                     |
| Skill files: шесть строк, раскрытие, read-only | `src/features/context-resources/SkillManifestFiles.vue`                     | `SkillManifestFiles.test.ts`; при восьми файлах шесть строк, раскрытие доступно и после архивации                                                                                                                                                                                    |
| Memory retention                               | `src/features/context-resources/retention.ts`, `ContextEditor.vue`          | `retention.test.ts`, synthetic expiry: содержимое убирается по deadline, сервер перечитывается, локальный код не назначает EXPIRED                                                                                                                                                   |
| Полнота переводов                              | `src/app/i18n/index.ts`                                                     | `index.test.ts`: все RU keys имеют EN; единственный русский autonym в EN — название языка Русский                                                                                                                                                                                    |
| Workflow: инструкции и ожидаемый результат     | `src/pages/WorkflowDetailPage.vue`, `src/shared/ui/CodeEditor.vue`          | `e2e/fixtures/workflow.ts`: Tab, сохранение исходных пробелов, dirty navigation; server materialization и provenance не подменяются браузерной интерполяцией                                                                                                                         |
| Kanban: bounded columns и общий cursor         | `src/pages/RunsPage.vue`, `src/features/workboard/components/RunsBoard.vue` | `e2e/fixtures/runs-catalog.ts`: восемь длинных карточек, scroll до следующей страницы, search/reset cursor, page overflow; layout входит во все семь synthetic viewport                                                                                                              |

Email receipt UI не получает worker grant/effect key/external receipt ref и не
повторяет reconciliation автоматически. При ошибке мутации нужно новое чтение;
историческое решение показывается без восстановления его срока действия.
Пока нет HTTP каталога integration invocations или ссылки invocationRef из
run history: ввод exact ref не заменяет требуемый список. Receipt-bound fresh
session подключена к HTTP #1045; live email authority и полный consumer
lifecycle не подтверждаются synthetic UI.

Перед email reconciliation выполняется существующий OIDC redirect с
`prompt=login/max_age=0`, затем `createOwnerSession` с exact receipt purpose
без project/secret. После callback пользователь явно выбирает исход и
подтверждает команду. Примечание и transcript не помещаются в storage.
Для неопределённой попытки `email-attempt.ts` хранит только refs/version/digest,
outcome, хеш тела и прежний idempotency key (не более 20 незавершённых попыток).
После нового SSO прежнее примечание нужно ввести снова: несовпадающее тело
закрыто отклоняется, ключ не заменяется автоматически. Авторитетное decision
readback удаляет запись; logout очищает локальные метаданные.
`session/reauth.test.ts`, `session/store.test.ts`, `email-attempt.test.ts`
проверяют binding, одноразовое локальное подтверждение, deadline и ключ.
`e2e/fixtures/email-oidc.ts` использует синтетический IdP с PKCE, purpose и
replacement cookies; этот тест не проверяет подпись реального IdP на gateway.
Context7 проверен для `/authts/oidc-client-ts`; дополнительно прочитаны API
установленной версии и `security-headers.conf`. Popup не используется: текущий
COOP `same-origin` сохранён без ослабления защитных headers.

После HTTP `9c1231ab7` каталог запусков передаёт серверу точные `states[]`
для ACTIVE/TERMINAL. `features/workboard/run-catalog.ts` хранит страницы по 40
записей, отменяет старый запрос и сбрасывает cursor при смене проекта, поиска
или фильтра. Ответ с чужим проектом, состоянием вне запроса, дублем либо
повторным cursor отклоняется. Scroll любой колонки догружает следующую общую
страницу; локальные lane counts не выдаются за серверные totals.
Unit проверяет scope, смену фильтра и поздний ответ; synthetic каталог запусков
проверяет network states, сброс cursor и отображение нового набора.

HTTP `5717e7cf4` добавил обязательные `catalogRevision/catalogDigest` и
ожидаемый снимок для страниц моделей. `features/providers/model-catalog.ts`
закрепляет эти поля на следующей странице и в exact ID lookup; несовпадение
снимка закрыто останавливает выбор. Смена query/account начинает новое чтение.
`model-catalog.test.ts` и `models.synthetic.spec.ts` проверяют pinning,
pagination и исчезнувшую модель на 1440/390. Сохранение reasoning через
опубликованный TOML overlay не заменяет отсутствующую server-side проверку
модель/effort/catalog при публикации; эта producer-зависимость остаётся открытой.

Уточнение после `261b577ce`: typed Skill/Memory HTTP и STT parameters уже
получены. `src/features/context-resources` подключает отдельные каталоги,
Skill draft/save/validate/review/publish, Memory immutable revision/retention,
archive/restore/purge и history; `api.test.ts` проверяет scope, redaction и OCC.
`e2e/fixtures/context-resources.ts` содержит synthetic lifecycle, не CP/runtime
acceptance. Memory producer checkpoint `e9eeaaeac` интегрирован без изменения SDK.
Skill owner lifecycle и bind/unbind подключены через готовые checkpoints.
`ContextBindingPanel.vue` и `bindings.ts` используют обязательные
`skillBindings`/`memoryBindings` из runtime view с точным agent ETag. Только
пустой авторитетный список допускает `expectedBindingVersion=0`; неизвестный
snapshot закрывает команды. Unbind использует прежнюю связанную revision,
а не новую current revision ресурса. После команды обязательны GET и проверка
квитанции; ошибка не вызывает повтор mutation. `bindings.test.ts` проверяет
два OCC, пустые массивы, старую revision и повреждённую квитанцию.

`SkillImportDialog.vue` использует существующий artifact upload, затем exact
revision read для scan. Импортирует выбранные файлы/папку либо SKILL.md из общего
CodeMirror; не реализует ZIP extraction на клиенте и не выдает импорт за
validation/review. Файлы добавляются в manifest после CLEAN. Формат draft
сверен с `context-http-1045.md`: root SKILL.md, closed extensions, Unicode
name/description, 240 UTF-8 bytes на path, 128 файлов, file digest с `sha256:`.
Локальные bounds upload: 32 MiB на файл, 64 MiB на очередь, 256 KiB на SKILL.md;
совокупный размер уже существующего bundle окончательно проверяет CP.
`skill-import.test.ts` проверяет paths, дубли, byte limits и exact receipt.
Memory source-run selector использует project/query/cursor и сохраняет
owner-returned sourceRef при новой revision (`selectors.test.ts`).

`ConfigurationFields.vue` отображает STT languages/keywords/prompt/temperature,
chunking и три лимита; `ConfigurationFields.test.ts` проверяет полный профиль
и disabled-форму. Server modelprofile остаётся источником совместимости.
Отсутствующий HTTP model catalog не заменён списком придуманных моделей.
Guard `shared/ui/unsaved-changes.ts` защищает workflow/managed/context формы;
unit и synthetic workflow проверяют отмену ухода. Через Context7 сверены
Vue Router Composition API leave/update guards и их lifecycle cleanup.

Canonical IntegrationPackage: `IntegrationPackageField.vue` строит форму по
`contracts/integrations/v1/integration-package.schema.json`: вложенные
capabilities, credential slot без значений секретов, typed connection/input/output
fields, network destinations, health check, retry/idempotency и Human Gate policy.
`npm run generate:integration-schema` генерирует standalone browser validator
(Ajv 2020-12, без runtime eval); генератор включён в `npm run codegen`.
Context7: Ajv standalone ESM/CSP и esbuild stdin/resolveDir/browser bundle.
`integration-package.test.ts` проверяет все
семь shipped manifests, bounds, conditional destination и отсутствие значений
в diagnostics; `document.test.ts` проверяет нормализованный diff без перестановки
массивов. `e2e/fixtures/integration-package.ts` проверяет form→JSON→save и diff
на синтетическом HTTP, не объявляет публикацию работающей на CP.

Конкретная незакрытая producer-зависимость: `revision.IntegrationPackage()`
в control-plane принимает только digest из `integrationpackage.LoadShipped()`.
Любая UI-правка manifest отклоняется даже для известного adapter. Schema пока
разрешает только `metadata.origin=SHIPPED`; UI/GIT origin и исполнение новой
опубликованной managed revision требуют producer/consumer contract. PWA не
меняет schema и не объявляет draft готовым runtime package. Публичный flattened
IntegrationDefinition не подменяется полным manifest; источник полного content
для существующей managed configuration читается через history.

Дополнительная воспроизводимая приёмка #1022:

| Критерий                                                                                          | Файл                                                                                            | Тест                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Secret JSON: позиция ошибки без фрагмента значения, форматирование, отсутствие voice              | `src/shared/ui/json-diagnostic.ts`, `src/features/runtime-secrets/RuntimeSecretValueDialog.vue` | `json-diagnostic.test.ts`, `e2e/fixtures/secrets.ts`                                                                                                                    |
| Подтверждённая Secret revision не откатывается отставшими GET/list; plaintext не попадает в Pinia | `src/features/runtime-secrets/model.ts`, `store.ts`                                             | `model.test.ts`, `store.test.ts`, synthetic create/readback                                                                                                             |
| Provider selector и остановка device polling после закрытия                                       | `src/features/providers/ProviderAccountsWorkspace.vue`, `store.ts`                              | `store.test.ts`, `e2e/fixtures/providers.ts`, `e2e/synthetic.spec.ts`                                                                                                   |
| RoleImage bounded/expand, version/cursor/project scope и отмена чтения                            | `src/features/role-images/RoleImageCatalog.vue`, `store.ts`                                     | `store.test.ts`, `e2e/fixtures/role-images.ts`                                                                                                                          |
| Voice скрыт в disabled managed fields, запись отменяется без транскрипции                         | `src/features/managed-configurations/ConfigurationFields.vue`                                   | `e2e/voice.synthetic.spec.ts`: блокировка во время записи, сохранение четырёх полей, повторная диктовка в курсор, отзыв доступности; 1440/390 px                        |
| Voice скрыт в disabled workflow fields                                                            | `src/pages/WorkflowDetailPage.vue`                                                              | `e2e/fixtures/workflow.ts`: Save во время записи, CodeMirror read-only до ответа, ноль транскрипций, сохранение исходных инструкций и восстановление ввода; 1440/390 px |

В ручной приёмке JSON-секрета ввести невалидный документ, убедиться в отсутствии
POST, исправить и отформатировать, затем сохранить. После закрытия диалога
значение не должно оставаться в DOM/Pinia/browser storage. При задержке
каталога подтверждённая ревизия остаётся видна; повторный POST не выполняется.
На 390 px кнопки действий Secret и footer RoleImage должны целиком помещаться
в строку/карточку. Synthetic проверяет их геометрию отдельно от page overflow.

`npm run test:e2e:synthetic` собирает отдельный `dist-synthetic`, запускает
локальный preview `http://127.0.0.1:43122` и закрывает его после тестов.
Обязательные desktop screenshots: 1280, 1440, 1920, 2560 и 2900 px,
mobile: 390 px; дополнительно проверяется 900 px. Результаты находятся
в `test-results/synthetic`. Запросы приложения перехвачены безопасными fixtures,
MediaRecorder использует искусственное устройство Chromium. Это не live,
не проверка OIDC-провайдера и не реальная транскрипция. Успех этого набора
не означает приёмку всех MVP-UI-01..61: подробные lifecycle-сценарии
проверяются на пяти обязательных desktop-ширинах и 390, дополнительный 900
покрывает сокращённый набор, явно выполняемый в `synthetic.spec.ts`.
Voice/keyboard primitive проверяется на 1440/390. Полная staging-матрица принадлежит #1031.
Service Worker в этом
контуре заблокирован; его контракт проверяется отдельно. Fixture-страница voice
не входит в production-сборку `npm run build`.

### Ручная приёмка владельцем

1. Проверить постоянную навигацию и выбор проекта, глобальные каталоги,
   группировку, поиск с задержкой 500 ms, курсорное продолжение и expand.
2. Создать managed draft, проверить невалидный документ, затем validate,
   publish, history, exact impact и выборочное rebind. Git-owned запись
   изменять только после явного detach/copy.
3. Проверить файлы без автоселекции, VFS breadcrumbs, просмотр объекта,
   корзину и exact bulk receipts; неизвестные виды не должны открывать чужой UI.
4. Проверить provider verify/reauth/revoke/delete, cron preview, environment
   save/validate/publish/discard и Secret rotation; неполный environment draft сохраняется без публикации, API-ошибка сохраняет введённые данные.
5. Для STT проверить `available=false`, просроченный `validUntil`, отзыв,
   browser deny, запись длиннее 30 секунд с успешным refresh, cancel и undo.
   На Secret/password/readonly/disabled полях микрофона быть не должно.
6. Проверить все detail/editor панели на desktop/mobile, console/network,
   session renewal и realtime reconnect. Live выполняет root после общей
   интеграции и отдельного разрешения владельца.
7. Импортировать Skill с root SKILL.md и supporting files; проверить отказ на
   traversal/дубли/превышение limits, PENDING/CLEAN/INFECTED, отдельные
   save/validate/review/publish. Source SKILL.md редактируется общим CodeMirror
   с Tab и voice eligibility, не становится опубликованным после upload.
8. Создать Memory с source run и retention, выпустить revision, проверить
   archive/restore/purge/redacted history. Привязать Skill/Memory к агенту,
   сменить exact revision, отвязать: agent ETag и binding version независимы;
   stale version не повторяет mutation. Уже работающий attempt не меняется.
9. Открыть canonical IntegrationPackage, изменить типизированное поле,
   переключить JSON/YAML и проверить нормализованный diff. Невалидная schema
   не подтверждает runtime readiness; публикация UI/GIT требует закрытия
   описанной producer-зависимости, synthetic save не заменяет её приёмку.

Для #1022 дополнительно проверены Context7 Vue (`/websites/vuejs`), CodeMirror
(`/websites/codemirror_net`), MediaRecorder (`/mdn/content`), oidc-client-ts
(`/authts/oidc-client-ts`), js-yaml (`/nodeca/js-yaml`) и Vite (`/vitejs/vite/v8.0.10`).

## Deploy ownership

`deploy/k8s/base/staff-control-center` содержит Deployment, Service, Ingress,
runtime ConfigMap, PDB, ServiceAccount и default-deny NetworkPolicy. Nginx
работает без root, с read-only filesystem и same-origin TLS proxy к
`control-api-gateway`. Image reference закрепляется digest. Pod не получает
service-account token или server credentials.

## Проверенная документация библиотек

Для Playwright проверены Context7 `/microsoft/playwright/v1.61.0` и актуальный
package API: изоляция BrowserContext, automatic fixtures и storage state. Для
безопасного file IO проверена документация Node.js
`/websites/nodejs_latest-v24_x_api`: `open`, `O_EXCL`, `O_NOFOLLOW`, `fstat`,
`fsync` и `rename`. Для остальных frontend-зависимостей применяются источники
из `FE-DOC-001` и закреплённые версии `package-lock.json`.
