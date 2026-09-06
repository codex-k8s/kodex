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

Историческая карта передачи ранних checkpoints. Указанные ниже отсутствующие
контракты отражают состояние на момент передачи, а не текущий SDK. Актуальный
остаток и результаты приведены в разделе «Текущий остаток 641/72 и #1075».

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
| Workflow user∩agent   | `getAgentEffectiveCapabilities` подключён для Agent grants и требований Workflow: draft intent использует agent-only effective, опубликованный exact step читается отдельно. Assigned capability и `availableWithoutIntegration` не заменяют authority. Aggregate preview запуска и guard вложений остаются отдельным остатком.               |

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

Продолжение PWA после передачи:

- D2 страницы каталога моделей связаны с авторитетными catalogRevision и
  catalogDigest; последующие страницы передают оба expected-поля и закрыто
  отклоняют смену snapshot. Runs получает ACTIVE/TERMINAL через server states,
  сбрасывает cursor при смене фильтра и не принимает поздний ответ старого scope.
- D4 Run details показывает безопасный исторический RuntimeRevision diff:
  текущая материализованная ревизия и выбранная сервером предыдущая ревизия
  той же Session. Это не preview будущего continuation. Первая ревизия и
  отсутствие изменений имеют отдельные состояния; смена Run/version отменяет
  прежнее чтение. Ошибка boundary не превращается в пустой успешный diff.
- Runtime editor сохраняет независимые несохранённые правки модели и overlay
  при сохранении другой части. Во время команды редактор и voice блокируются;
  route leave предупреждает о правках. При смене агента/unmount отменяется
  чтение, поздний mutation response не перезаписывает новый контекст.
- Browser fixture runtime-detail проверяет read-only во время сохранения,
  сохранность overlay после ответа и historical diff на 1440/390 px. Fixtures
  не доказывают producer authority или runtime materialization.
- D6 использует staged SecretDraft HTTP/SDK `3ad562f0f` и prepublication impact
  `bcdfa2063`: save/create → authoritative GetDraft → validate → prepare impact
  → explicit selection → publish → APPLIED outcomes. Ввод существует только
  в открытой форме до подтверждённого save; неизвестная попытка повторяется с
  прежними input/key/OCC. Закрытие неопределённой попытки требует явного
  подтверждения потери возможности повторить ввод. Новая metadata-команда
  читает свежую owner version, retry сохраняет исходный snapshot.
- План показывает immutable total отдельно от текущей доступной выдачи,
  поддерживает server search/cursor и environment-only строки. Пустой выбор
  имеет отдельное действие публикации без замены. По умолчанию отмечены все
  доступные строки: UI догружает серверные страницы в пределах 1000 элементов
  и общего 15-секундного бюджета до разблокировки публикации. Пустой выбор
  явно публикует без замены потребителей. После публикации APPLIED читается с
  первой страницы, поскольку прежний cursor принадлежит PREPARED. Ошибки
  CONFLICT/FORBIDDEN остаются результатами отдельных строк. Ссылки draftRef и
  planRef в URL позволяют восстановить только безопасные метаданные; после
  reload опубликованный Secret перечитывается авторитетно, без значения.

Матрица D6: `draft-api.test.ts` проверяет scope, safe metadata, OCC и exact
retry; `RuntimeSecretDraftDialog.test.ts` — lost ACK, блокировку повторного
submit и свежий GetDraft; `draft-impact.test.ts` — plan/page totals, pins,
cursor, explicit empty selection и частичные результаты;
`RuntimeSecretDraftImpact.test.ts` — повтор публикации и смену owner pins.
`e2e/fixtures/secrets.ts` проходит JSON save/validate/plan/select/publish,
результат environment-only замены и reload по сохранённым ссылкам. Это
synthetic-профиль; реальный путь CP/broker/runtime/staging остаётся NOT RUN.

Для продолжения проверены Context7 Vue watcher cleanup, Pinia reset setup
state, Playwright route mocking/viewports и CodeMirror dynamic configuration
через Compartment. Версии библиотек не менялись.

Остаток полного unit:

- D3 не заменяет effective user∩agent capability projection. Каталог готовности
  не доказывает runtime materialization; D4 exact target preview остаётся зависимостью.
- D5 credential имеет безопасный GET receipt и восстановление исходного key
  после закрытия/reload. Mailbox UI подключает typed fields/YAML, server preview,
  draft/validate/publish и отдельный bind с delivery readback. Реальная
  consumer delivery/READY и protocol-specific SMTP/IMAP/POP3 readiness ещё
  не подтверждены; latest publication READY не заменяет protocol readiness.
- D2: mutation catalog pins и schema-driven reasoning effort подключены по
  HTTP `5d09619a`; реальный runtime путь ещё NOT RUN. D4: exact
  AGENT/WORKFLOW_STEP/SCHEDULE_DRAFT preview. D6 UI lifecycle подключён выше;
  его реальная сквозная приёмка проводится после общего integration gate.
- Остальные контрактные пробелы таблицы выше сохраняются: VFS lifecycle,
  Home owner-gate query/total, Mattermost selector eligibility, RoleImage ownership/build,
  UI/GIT IntegrationPackage execution. Полная MVP-UI-01..61 и staging-приёмка
  остаются незавершёнными; новые блоки в рамках этой передачи не реализовывались.

### Исторический checkpoint UI mailbox lifecycle и требований

`EmailMailboxConfigurationPanel.vue` и `email-mailbox-editor.ts` используют
только server `View.nextActions` / `List.nextActions`, включая создание при
пустом каталоге. Перед новой командой сверяются configuration и connection
версии; изменение не вызывает скрытого перебазирования пользовательского ввода.
Неизвестный результат повторяется с прежними input/key/OCC. Закрытие или
переход при незавершённой попытке требуют явного отказа от локального контекста.
Plaintext credential не входит в эти конфигурационные команды.

Форма и YAML синхронизируются только через server preview: semantic-invalid
incomplete draft может возвращать specification и diagnostics, syntax failure
не переключает редактор на восстановленную модель. Публикация immutable
revision не означает применение к connection. Первый bind разрешается
authoritative action без требования прежней READY; PENDING читается ограниченно
с последующим ручным refresh. Историческая revision, boundRevisionRef и revision
последней delivery отображаются независимо.

`email-mailbox-editor.test.ts` проверяет authority denial, изменение owner pin,
exact retry после потери ACK, syntax failure и поздний ACK после закрытия.
`mailbox.synthetic.spec.ts` воспроизводит create с потерей ACK → exact retry →
validate → publish → first bind → PENDING/READY readback → reload на 390/2900.
Это локальные безопасные fixtures; runtime mailbox и owner/consumer готовность
ими не доказываются. DETACH/COPY используют существующие специализированные
managed commands и последующий GET mailbox по owner configurationRef без
revisionRef: сервер выбирает новый UI draft. Опубликованный currentRevision
не подставляется вместо нового draft. COPY получает отдельный mailboxRef и
не наследует boundRevisionRef; owner подтвердил этот mapping локальными PG
проверками, общий путь всё ещё требует интеграционной проверки.

Следующая историческая таблица относится к исходному scope 01–61 и CFG на момент
mailbox checkpoint. Последующие поставки контрактов описаны ниже; таблица не
является актуальным списком отсутствующих producer paths.

| Требования     | Существующий consumer/API                                                                   | Незавершённая producer/сквозная часть                                                                                                                                                                  |
| -------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 17–19          | ProviderModelSelector, AgentRuntimePanel; `/model-capabilities`, config-overlay publication | Account catalog status/expiry и exact mutation pins подключены по HTTP5d; output default не возвращается в mutation. Реальная model/effort/runtime приёмка NOT RUN.                                    |
| 21             | Runtime editor; config-overlay draft/validation/publication/history/rollback                | Owner schema и protected published history подключены по HTTP6e3; реальная приёмка NOT RUN.                                                                                                            |
| 16, 34–36, 43  | agents/detail/api, WorkflowDetailPage, automation editor; prompt-template preview           | Exact будущие AGENT/WORKFLOW_STEP/SCHEDULE_DRAFT target/context. SYNTHETIC preview и исторический RuntimeRevision diff не заменяют этот путь.                                                          |
| 31, 40         | AgentAccessPanel/Workflow editor; `/agents/{agentRef}/effective-capabilities`               | Typed requested/effective/grantable/reason и exact connection rows подключены. Черновик не получает fake published identity. Aggregate target/attachment guards и runtime acceptance ещё не завершены. |
| 37, 61         | VfsPage, files/context resources; `/vfs/nodes`, `/vfs/search`                               | Node version/state/nextActions, eligibility выбора и массовых операций. Skill/Memory typed lifecycle уже подключён; реальная runtime запись принадлежит owner/runner.                                  |
| 04–06          | HomePage/NewRun; `/runs`, `/owner-gates`, global/project artifacts                          | Distinct resumable Session catalog подключён по owner f9af/HTTP b093: server query/total/cursor и парный target filter. Реальная runtime приёмка NOT RUN.                                              |
| CFG, 42        | RoleImage/managed configuration editors; role-image-recipes                                 | Server query/state/total, managed ref/revision/source association и build pin подключены по HTTP022869. Полный write-back и сквозная приёмка UI/GIT package/build остаются в работе.                   |
| Mattermost, 39 | InteractionIdentitiesPanel; identity bind/revoke                                            | Eligible active platform USER, team/channel catalog и canonical external-user hash rule.                                                                                                               |
| 46             | RuntimeEnvironmentEditorPage; environment drafts                                            | Server savedAt и immutable base reference подключены по HTTP6e3; неизвестная legacy база не выводится из current set. Реальная приёмка NOT RUN.                                                        |
| 41             | Mailbox panel; typed email-mailbox endpoints                                                | Интеграционная проверка COPY/DETACH, реальная delivery и protocol readiness.                                                                                                                           |
| 56             | ConfigurationFields; `/system-stt/model-catalog`, speech bootstrap и transcriptions         | Каталог подключён к выбору модели и параметрам. Реальная bootstrap authority/adapter readiness проверяется владельцами STT/HTTP; каталог не означает READY.                                            |

Остальные строки исходной матрицы выше требуют общей интеграционной и ручной
приёмки; локальный unit/browser PASS не превращает их в выполненные business
acceptance criteria. `rotation.synthetic.spec.ts` отдельно проверяет staged
rotation на 390/2900: lost save ACK → exact retry с исходными Secret OCC/key/value →
validate/impact → lost publish ACK → reload с авторитетными PUBLISHED/APPLIED
readbacks. APPLIED, CONFLICT и FORBIDDEN остаются отдельными результатами
потребителей; повторная публикация для такого recovery не отправляется.
Живой путь CP→broker→runtime по этим fixtures не считается проверенным.

Прямая ссылка на решение читает существующий `GET /owner-gates/{gateRef}`:
отсутствие Gate на первой странице не выбирает другое решение. Смена query
внутри страницы перечитывает адресованный Gate; terminal открывает историю,
а скрытый ресурс оставляет отдельную ошибку без подстановки соседней строки.
`gate-navigation.synthetic.spec.ts` проверяет эти переходы на 390/2900.
Вкладка показывает короткое «Ожидают» с полным доступным именем и tooltip
по MVP-UI-38; счётчик расположен отдельно от переключателя.
Подписи последствий, причин ответа, вложений, аудита и применения решения
используют общий ru/en-каталог. Browser-сценарий меняет локаль на английскую
без remount; авторитетные пользовательские тексты Gate не переводятся клиентом.
Агрегированный synthetic Home проверяет более двадцати экранов за один запуск
и имеет отдельный бюджет 75 секунд. Общий бюджет остальных сценариев остаётся
45 секунд; лимиты ожиданий элементов и сетевых запросов не увеличены.

Общий API lifetime инвалидируется при очистке owner state. Запоздалые данные,
401 и автоматические retry прежней сессии не применяются к следующему входу;
сброс локального query generation не открывает эту границу повторно.
Регрессии проверяют старый ACK после нового запроса с тем же ключом, поздний
401, mutation receipt и смену контекста между попытками чтения и записи.
Отмена браузерного ожидания не означает отмену уже принятой сервером команды.

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

Перед email reconciliation PWA вызывает BFF `beginOwnerAuthorization` с
exact receipt purpose и `freshAuthentication=true`, без project/secret.
BFF владеет OIDC code/PKCE exchange; браузер передаёт только code/state в
`completeOwnerAuthorization`. После callback пользователь явно выбирает исход и
подтверждает команду. Примечание и transcript не помещаются в storage.
Для неопределённой попытки `email-attempt.ts` хранит только refs/version/digest,
outcome, хеш тела и прежний idempotency key (не более 20 незавершённых попыток).
После нового SSO прежнее примечание нужно ввести снова: несовпадающее тело
закрыто отклоняется, ключ не заменяется автоматически. Авторитетное decision
readback удаляет запись; logout очищает локальные метаданные.
`session/reauth.test.ts`, `session/store.test.ts`, `email-attempt.test.ts`
проверяют binding, одноразовое локальное подтверждение, deadline и ключ.
`e2e/fixtures/email-oidc.ts` проверяет redirect, exact purpose, BFF callback и
replacement cookies; этот тест не проверяет реальный PKCE exchange или подпись
IdP на gateway. Историческая проверка Context7 `/authts/oidc-client-ts` относится
к прежнему consumer: эта зависимость удалена при исправлении #1086.
Popup не используется: текущий
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
pagination и исчезнувшую модель на 1440/390.

После HTTP `5d09619a4535319662fe04d1a380b1fc38c6ce51` exact lookup сохраняет
отдельные revision/digest/status для каждого account. READY без действующего
expiresAt не разрешает выбор; смена scope и истечение срока отзывают прежний
выбор. Mutation строится явным whitelist `accountRef/weight` и тремя pins,
без возвращения серверного `defaultReasoningEffort`. Сервер остаётся владельцем
проверки модели, полномочий и совместимости опубликованного overlay.

`overlay-editor.ts` использует owner `overlaySchema` для allowed values,
completion и hover. Селектор reasoning меняет TOML через `smol-toml`, сохраняя
семантические значения остальных допустимых полей; форматирование и комментарии
могут измениться. Неизвестные поля, неверные типы, повреждённый UTF-8 и превышение
owner byte limit закрывают преобразование. Типизированные diagnostics имеют
1-based UTF-8 byte column, преобразуемый в UTF-16 позицию CodeMirror; нулевые
и повреждённые позиции показываются в списке без выдуманного inline marker.
Публикация требует VALID с текущими schema revision/digest. При несовместимой
смене модели UI поясняет порядок: убрать explicit effort, проверить и
опубликовать overlay, сохранить модель, затем выбрать effort новой схемы.
Атомарная публикация модели и overlay не предполагается.

Context7 проверен для CodeMirror completion/hover и позиций документа.
Точный пакет smol-toml не найден в Context7: прочитан официальный
`https://github.com/squirrelchat/smol-toml` README версии 1.8.0.
Через openai-docs прочитан `https://developers.openai.com/codex/config-reference`;
статический перечень effort не используется вместо серверного каталога.
Unit проверяют pins, expiry, whitelist, TOML scope и Unicode diagnostics;
runtime synthetic проверяет effort save и readonly на 390/1440/2900.
Это локальные fixtures, реальная PWA→HTTP→CP→provider приёмка NOT RUN.

HTTP `6e3adbca9e7d194641d912beb83712548ecfe2aa` подключает следующие read paths:

- `OverlayHistoryPanel` читает server-side search/cursor/total опубликованных
  immutable revisions; перед preview выполняет protected exact GET. Readonly
  TOML и ref/digest показываются перед rollback. Команда отправляет выбранный
  published ref и текущий agent OCC, сервер повторно проверяет модель/схему и
  создаёт новую публикацию. Закрытие/смена агента отбрасывают поздние ответы.
- Environment показывает `baseVersionRef/baseRevision` отдельно от draft/set
  version, а `savedAt` — отдельно от validation/update. Отсутствующая legacy
  база остаётся неизвестной; историческая квитанция без savedAt требует GET
  черновика. Обычные create/save/validate fixtures закрепляют время сохранения.
- Home получает Runs, failed Runs и файлы через авторитетные страницы по 30.
  Compact и expanded view разделяют scope/query/cursor, высота ограничена
  шестью строками, нижняя граница догружает страницу. Server total не заменяется
  длиной items. Общие файлы читаются одним `listOrganizationArtifacts`, без
  обхода проектов; project filter использует существующий `listArtifacts`.
  Личный файл открывает exact protected metadata и доступное owner download,
  а не ошибочный переход в `/projects`. Logout/unmount не завершают позднее
  скачивание в новом контексте.

Новый rollback synthetic обнаружил общий popover defect на 390 px: уже сжатая
высота панели заставляла выбирать недостаточное место снизу, и search/footer
перекрывали option. `dismissible-popover.ts` выбирает сторону по доступному
viewport независимо от текущего сжатия. Unit закрепляет повторные измерения,
runtime synthetic проверяет реальный pointer selection на 390/1440/2900.
Home synthetic на 390/2900 проверяет разные page length/total, cursor, поиск,
modal и private-file exact read. Реальная сквозная приёмка остаётся NOT RUN.

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

Историческая producer-зависимость до поставки UI/GIT source contract: `revision.IntegrationPackage()`
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

## Checkpoint 6931d7d52: совместимость D5 и точных Secret revision

Новый HTTP SDK включает типизированный mailbox lifecycle, безопасную квитанцию
credential и точную опубликованную `RuntimeSecretDescriptor.revision`.
Редактирование остальных полей Environment сохраняет этот pin, включая
восстановление несекретного черновика после повторной авторизации. Явный выбор
другого Secret назначает `revision=0` для серверного выбора текущей revision.
Generic managed configuration dispatcher отклоняет `EMAIL_MAILBOX`: его
команды принадлежат специализированному API.

Незавершённая credential попытка сохраняет в sessionStorage только
connectionRef/version, kind и исходный idempotency key. После закрытия формы и
reload результат восстанавливается авторитетным GET receipt без plaintext
или value digest. Неизвестный исход остаётся неизвестным до readback; logout
удаляет локальные указатели попыток.

Добавлены типизированные mailbox API wrappers и поля SMTP/IMAP/POP3; подключение
полного редактора к странице и authoritative action projection ещё в работе.
Это промежуточная совместимость, а не завершение D5 или MVP-UI-41. Реальная
доставка mailbox и переход READY, CP/broker/browser lifecycle, staging и live
остаются NOT RUN. Полная синтетическая проверка этой новой области ещё NOT RUN;
24/24 предыдущего checkpoint относятся к D6 и прежнему интерфейсу.

Генератор integration schema закрыто завершает работу на strictTypes и иных
предупреждениях Ajv/esbuild до записи generated validator. Проверка на отдельной
временной схеме без object type подтверждает этот отказ; исправленный исходный
контракт проходит генерацию.

## Каталог STT в форме

`ConfigurationFields` читает типизированный `/system-stt/model-catalog` до
создания первой конфигурации. Общий AsyncEntityPicker ищет в полном ограниченном
каталоге; версия и дата относятся к проверке адаптера, а не к живому provider probe.
Идентификаторы моделей не зашиты в форму. Сохранённая отсутствующая модель и
параметры не теряются при чтении, ошибке каталога или выборе другой модели.
Не подтверждённые профилем значения остаются видимыми для исправления и
окончательной серверной проверки. `chunking_strategy` явно связывается с DTO
`chunkingStrategy`; будущие значения не расширяют текущую OpenAPI enum.

Именованные границы формы взяты из принятого OpenAPI: размер 1024..26214400 байт,
длительность 1000..1800000 мс, timeout 1000..15000 мс. Рекомендации каталога
показаны отдельно и не подменяют эти границы. Профиль задаёт ограничения prompt,
keywords и temperature; текущий публичный API по-прежнему не допускает stream=true.
Динамическая проекция общей policy min/max отсутствует в текущем каталоге.
Только новый пустой профиль один раз получает recommended model и совместимые
начальные language hints/параметры из текущего каталога; существующий документ
не перезаписывается. Timeout требует явного ввода в серверных границах.
Очистка числового поля удаляет значение из неполного черновика, не оставляет
скрытый прежний номер и не подставляет temperature=0. Обычный STT JSON/YAML
редактор содержит credential reference и допускает общий voice input;
credential value редакторы остаются sensitive и не получают микрофон.
`stt-catalog.synthetic.spec.ts` проверяет сохранение значений, поиск, ошибку
каталога и геометрию на 390/2900; реальный STT/provider/runtime остаётся NOT RUN.

## Серверные страницы решений

Home/Decisions потребляют HTTP checkpoint `358687bc7b55878225adeba897ada92b820c284e`:
`listOwnerGates` передаёт literal query, project и явный набор состояний истории;
страницы по 30, total и cursor принадлежат CP. Смена фильтра отменяет старое чтение,
повтор cursor/строк отклоняется; realtime invalidation перечитывает первую страницу.
Home раскрывает тот же список с поиском и серверным выбором проекта, сохраняет
порядок и ограничивает компактный список шестью строками. Decisions сохраняет
отдельный protected GET для адресного решения вне загруженных страниц.
Это не закрывает MVP-UI-05: Run/global Artifact totals и реальная сквозная приёмка
остаются обязательными; browser fixtures не доказывают runtime authority.

## Источники Git: CFG-01/02/03

`GitSourcePanel` потребляет четыре специализированных Configure/Refresh команды
из HTTP `5e59abbea53dbfcfa2188e08343753d4f442add4`. Выбор соединения использует
серверный поиск и exact GET/version; поддержаны JSON/YAML и закрытые adapter keys
GitHub/GitLab. Полномочия, состояние соединения, credential и доступ к репозиторию
проверяет владелец. Configure переводит конфигурацию под Git и закрывает сохранённый
неопубликованный черновик; прежняя опубликованная ревизия и история сохраняются.

Несекретный исходный intent с двумя OCC и idempotency key сохраняется в sessionStorage
до подтверждения; закрытие формы и reload не создают новый ключ. Logout удаляет
указатели. При неопределённом исходе доступны точный повтор и authoritative refresh;
прекратить повтор можно после чтения более новой версии, без заявления об отмене
или успехе прежней команды. Пока исход неизвестен, другие изменения заблокированы.

После команды редактор читает `ListManagedConfigurationHistory`; QUEUED/CLAIMED
опрашиваются каждые две секунды, максимум 150 чтений, с остановкой при закрытии.
Поколение источника сбрасывает бюджет; изменение только source.version учитывается
даже при неизменной configuration.version. Принятые commit/content/revision pins
показываются отдельно; source READY не означает готовность или продвижение образа.
Отдельного realtime события для SourceWork нет. Detach/copy перечитывают историю,
чтобы открыть новый UI-черновик, сохраняя исходную опубликованную ревизию.

`git-source.synthetic.spec.ts` проверяет на 390/2900 сохранение intent после потери
ответа и reload, исходные OCC/key и переход QUEUED→READY через чтение истории.
Producer checkpoint `dfd0621c52d982459d218973d37fac9ab424a716` сообщает executable
owner/profile62; локальные browser fixtures не доказывают реальный HTTP→CP→Git
path. Его приёмка, live/staging, полный write-back и UI recipe projection остаются
NOT RUN либо незавершённым исходным scope; CFG не объявляется полностью закрытым.

Три закрытых `i18n:INTERACTION_DELIVERY_*` сообщения и `INTERACTION_AUTHORITY_CHANGED`
переводятся в generic result и решениях. Произвольный owner content не
интерпретируется как ключ перевода.

## Каталог продолжений Session и происхождение RoleImage

Home и NewRun используют `listRuns(resumableSessionsOnly=true)`. CP возвращает одну
последнюю допустимую Run на Session и distinct total. Home использует серверные
query/project/cursor и ограниченную шестью строками область с расширением; NewRun
передаёт выбранные targetType/targetRef парой. Рекурсивный browser обход Run,
дедупликация Session и отбор target удалены. Повреждённая страница отклоняется
целиком, без изменения total. Ответ 412 при догрузке сбрасывает старые строки и
начинает первую страницу; ошибка первой страницы не запускает бесконечный retry.
AddSessionTurn сохраняет независимую owner authority/OCC/runtime проверку.

RoleImage каталог передаёт query/state/page владельцу и показывает server total.
Карточка и редактор сохраняют managedLineage UI/GIT/SHIPPED, точные configuration
ref/revision/source pins; build показывает configurationRevisionRef. Ссылка ведёт
в существующий managed editor, а отсутствие lineage не назначает UI ownership.
Readonly полей определяется серверным UPDATE; исходный Dockerfile не становится
доступным для записи из-за наличия клиентского черновика.

Producer Session `f9af1bc528` и HTTP `b0939429d` проверены отдельно. Synthetic
проверяет distinct total, bounded list, stale cursor recovery, парный target
NewRun и server RoleImage search/state/count. Его ожидаемый HTTP412 учитывается
отдельно по точному fixture cursor; остальные console warnings/errors остаются
ошибками. Real protected browser→HTTP→CP и полный Git write-back — NOT RUN.

## Авторитетные полномочия сотрудника и этапа

В Files существующую связь с сотрудником можно снять после отзыва его
`platform.artifact.manage`, если Artifact по-прежнему разрешает `BIND`.
Это соответствует owner-команде: capability требуется только для добавления
связи. UI сериализует изменения версии файла; после ответа сервера снятая
связь не может быть добавлена снова без capability. Unit и synthetic проверяют
этот переход с точными OCC/idempotency и авторитетным ответом. `runtimeReady`
не используется как разрешение настройки связи. Удаление связей архивированного
сотрудника и полный server-owned каталог допустимых целей остаются отдельными
owner-зависимостями; реальные browser→HTTP→CP проверки этого пути — NOT RUN.

`AgentAccessPanel` читает `getAgentEffectiveCapabilities`: поиск, total и cursor
принадлежат серверу. UI сохраняет отдельные requested/effective/grantable и
закрытые причины отказа; одинаковые capability key разных connection/grant
не объединяются. Отозвать requested право можно при разрешённом управлении
сотрудником, даже если новое предоставление уже запрещено. Mutation сохраняет
OCC версии сотрудника и перечитывает проекцию после ответа.

Редактор требований Workflow использует agent-only effective для нового intent.
Проекция опубликованного этапа читается отдельно по настоящему owner step key;
несохранённый этап не получает вымышленную опубликованную identity. Поздние ответы
отменяются при смене scope, а конфликт digest между страницами сбрасывает каталог
к первой странице. Browser не вычисляет authority из назначенного списка прав.

Unit и synthetic проверяют запрет нового grant, допустимый revoke, OCC, серверную
пагинацию/поиск, отдельные integration rows и опубликованный step readback.
Реальный HTTP→CP/browser lifecycle остаётся NOT RUN. Каталог допустимых целей
привязки Files остаётся незавершённой частью исходного scope.

Context7: проверены Vue `/websites/vuejs` (watch cleanup и отмена устаревших
запросов) и Playwright `/microsoft/playwright/v1.61.0` (fullPage screenshots).

## Deploy ownership

`deploy/k8s/base/staff-control-center` содержит Deployment, Service, Ingress,
runtime ConfigMap, PDB, ServiceAccount и default-deny NetworkPolicy. Nginx
работает без root, с read-only filesystem и same-origin TLS proxy к
`control-api-gateway`. Image reference закрепляется digest. Pod не получает
service-account token или server credentials.

## Дополнительный аудит MVP-UI-01/09

Дополнительный аудит MVP-UI-01/09: неизвестный exact server token `i18n:KEY`
в generic результатах заменяется локализованным сообщением несовместимости.
Произвольный пользовательский текст с упоминанием префикса сохраняется.
Закрытие чата через Escape, фон или кнопку предупреждает о неотправленном
тексте/вложениях; отказ сохраняет открытый чат и ввод. SSR-проверки ru/en и
browser-сценарии на 390/1440 PASS. Этот аудит не завершает приёмку файлового
каталога, RoleImage impact и Git write-back.

Закрытый реестр переводов ru/en охватывает literal server tokens владельца,
семейства permissions, system roles и безопасных кодов ошибок. Проверка сверяет
исполняемые Go/SQL источники и закрытые реестры, а не только прямые вызовы `t`.
Назначаемые сервером роли запуска переводятся до безопасного отображения.
Неизвестный token по-прежнему не отображается как пользовательский текст.

## Доступность вложений и метаданные VFS

Generated SDK потреблён из HTTP `820ad57f1a64700ed27c0f31165d19d3ee579289`
через исходный OpenAPI и каноническую генерацию Go/TypeScript. NewRun читает
`getRunAttachmentEligibility` для точных Project/target и, при продолжении,
серверного Run. Один owner query проверяет весь Процесс. Браузер не загружает
исполнителей по отдельности для вычисления разрешения. Смена scope отменяет
прежний запрос и закрывает выбор вложений; отказ показывает server reason,
ошибка допускает повторное чтение. Запуск с уже выбранными файлами недоступен
до положительного ответа; окончательную проверку выполняет launch/continuation.

VFS search передаёт текущий path серверу вместе с query и поддерживает typed
lifecycle/kinds filters. Ответ проверяется по закрытым metadata/action enums,
версиям и scope; каталог с version=0 остаётся допустимым. Эти поля не выводятся
из browser state. UI массового выбора VFS ещё требует подключения; каталог
`listArtifactBindingTargets` подключён следующим описанным ниже пакетом.
RoleImage643 и Git write-back потребляются следующим пакетом.

До пакета Files67 получены unit 1016/1016 и targeted synthetic 390/2900 21/21 PASS;
browser-сценарий проверяет точный continuation Run и отказ по assigned Files
capability. Полный baseline фиксируется по SHA в PR. Реальный protected
browser→HTTP→CP, staging и live-провайдеры — NOT RUN.

Начальная загрузка общих Run теперь завершается до первого чтения отдельных
RUN/SESSION/FAILED каталогов Home. Иначе позднее завершение общего запроса
очищало уже прокрученные карточки и теряло запрос следующей страницы. Последующие
изменения Run по-прежнему инвалидируют каталог. ARTIFACT работает независимо;
непереданный компоненту `ready` имеет явное значение `true`.

Regression fixture удерживает только первый общий запрос, проверяет отсутствие
преждевременных чтений каталогов, затем освобождает его и проверяет оба курсора
Home и последующий Session recovery. Targeted 390/2900 — 2/2 PASS. Первый полный
прогон `9bfe88a95` имел 34/35 PASS и FAIL Home390; диагностические ошибки default
булевого prop и времени жизни fixture исправлены, их история сохранена в PR.

Exact `50d5810a1` прошёл unit 1016/1016, lint/format, production и synthetic
build/typecheck, E2E TypeScript/discovery. Полный synthetic: **FAIL 34/35 за
6,1 минуты**, Chromium `Target crashed` в Home2560 после Email OIDC. Исправленный
Home390 прошёл. Отдельный диагностический Home2560: PASS 42,5 s; последовательность
Home2900→Home2560 в одном worker/browser: PASS 2/2, 46,8/40,9 s. В stderr verbosity
нет fatal/OOM. Причина crash не доказана, диагностический PASS не отменяет FAIL.
Временный диагностический config вне репозитория сохранял исходные проверки и
ограничивал окружение browser безопасным списком; live credentials не передавались.

Каталог связей Files теперь использует `listArtifactBindingTargets`: серверные
query/total/cursor/digest, имена и `canBind/canUnbind` с закрытыми причинами. При
смене файла/версии/поиска прежнее чтение отменяется, snapshot drift на следующей
странице сбрасывает каталог и допускает только одно новое первое чтение. Панель
ограничена по высоте, поддерживает прокрутку, загрузку следующих страниц, поиск и
расширение в modal. Локальный обход Agents и вывод eligibility из их capability
удалены. Архивная существующая связь снимается по owner projection; configured
Agent не требует runtimeReady для знания. После mutation применяется точная
Artifact receipt и инвалидируется Agent cache: eager GET архивного Agent,
способный дать unhandled 404 после успешного unbind, больше не выполняется.
Подключён additive OpenAPI checkpoint HTTP `1bd6823f0`: группа `sourceKinds[]`
передаётся одним `listArtifacts` запросом с прежними query/state/type/scan
фильтрами. Клиентские per-source cursors, fanout и сортировка объединённых
страниц удалены. PWA сохраняет серверный порядок, единый cursor и total; общий
collection primitive переносит optional total вместе с lifetime страницы и
сбрасывает его при новом поиске. Если total стал меньше уже собранных строк,
старый snapshot закрыто сбрасывается с единственным чтением первой страницы.
Toolbar показывает загруженное количество и authoritative total. Generated
Go/TS получены канонической генерацией из source; для рабочего HTTP пути
требуется соответствующий checkpoint gateway, его mapper в PWA ветке не копируется.

До group consumer пакет Files67 прошёл полный unit 1031/1031, lint/format,
production/synthetic build/typecheck и E2E TypeScript/discovery. Scoped browser
390/2900: PASS 2/2 за 1,7 минуты (50,8/50,6 s). Первый lint обнаружил шесть
локальных ошибок, следующий typecheck — отсутствие error:undefined в typed mock;
исправленные проверки прошли. После sourceKinds consumer targeted unit 69/69,
typecheck и E2E TypeScript PASS. Полный новый functional baseline: unit
1037/1037, lint/format, production/synthetic build/typecheck, E2E discovery и
canonical Go/TS byte replay PASS. Scoped 390/2900 после group consumer:
**FAIL 1/2** — 390 прошёл, 2900 получил прежний Chromium `Target crashed` в
Email после OIDC ещё до Files. Это показывает, что сбой не ограничен 2560;
причина пока не доказана. Новый полный synthetic остаётся NOT RUN до следующего
согласованного пакета SDK; исходный full50d FAIL сохраняется. Отдельная CDP/heap
диагностика продолжается без изменения timeout, GPU или sandbox.

### Диагностика временного каталога Chromium

На dirty consumer tree HTTP `4c8dc9184` отдельный Home2900 зафиксировал
`Target.targetCrashed`, `status=crashed`, `errorCode=133` ещё на начальном Home,
до Email/OIDC. Счётчики cgroup `oom/oom_kill/oom_group_kill/high/max` были нулевыми.
Проверка среды выявила `/tmp` на tmpfs с `usrquota`: для текущего UID
`bhardlimit=bsoftlimit=25673840 KiB`, `curspace=26290012160 bytes`, то есть
свободная пользовательская квота равнялась нулю. Пробная временная запись 4096
байт получила `errno=122`, `Disk quota exceeded`; файл закрыт и удалён автоматически.
Свободные 6,2 GiB в `df` описывали файловую систему, а не эту квоту.

Закреплённый Playwright 1.61.0 создаёт временные profile/artifacts/tracing через
`os.tmpdir()` (`playwright-core/lib/coreBundle.js`, создание
`playwright_*dev_profile-*`, `playwright-artifacts-*`, `playwright-tracing-*`).
При незаданном `TMPDIR` использовался `/tmp`. Для воспроизводимого повторного
запуска используется отдельный каталог на диске, без очистки чужих файлов:

```bash
install -d -m 700 "$HOME/.cache/kodex-pwa-browser-tmp"
TMPDIR="$HOME/.cache/kodex-pwa-browser-tmp" npm run test:e2e:synthetic
```

Флаги Chromium, sandbox и timeout не меняются. Прежние full/scoped FAIL
сохраняются; результат повтора фиксируется отдельно. Опциональная диагностика
`KODEX_SYNTHETIC_DIAGNOSTICS=1` сохраняет безопасные CDP metrics/crash metadata
и trace с начала synthetic-сценария. Чтения CDP и cleanup имеют независимый
бюджет 5 s; JSON сохраняется до ожидания и после него, включая аварийный исход.
Эта диагностика не применяется к live-профилю.

Повтор того же Home2900 с дисковым `TMPDIR` прошёл: PASS 1/1 за 55,1 s,
Chromium завершился с exitCode=0. Сохранились все assertions Email/OIDC,
Files/Session; CDP получил metrics до и после смены протокола. Проверены также
Prompt publication 504 до эффекта → явный повтор исходного intent и Environment
publication 504 после эффекта → terminal protected GET без второй mutation.
Это отдельный диагностический PASS, не замена полного synthetic baseline.

### Планы публикации и восстановление исходного запроса

OpenAPI source HTTP `4c8dc9184` потреблён целиком через canonical Go/TS generation.
Prompt, Instructions и Environment используют specialized Prepare и общий
`RevisionImpactPlan`; RoleImage — отдельный admitted image impact plan и typed
Rebind. Подтверждение показывает immutable total отдельно от текущего owner
total, серверный поиск/cursor и явный selection (включая пустой). APPLIED плана
не скрывает CONFLICT/FORBIDDEN/NOT_SELECTED отдельных строк; смена состояния
сбрасывает cursor и читает новый отчёт с первой страницы.

Неизвестный исход сохраняет только kind/ownerRef/planRef, исходные version,
idempotency key и selected item refs в sessionStorage. Новый intent не заменяет
незавершённый. Пользовательское восстановление читает protected plan/source:
APPLIED подтверждается без mutation, EXPIRED закрывает прежнюю попытку,
PREPARED допускает только отдельное явное повторение того же запроса после
проверки исходной версии. Latest version и новый key при повторе не подставляются.
Содержимое инструкций, конфигурации и credential values в этом storage отсутствуют.

История Instructions выделяет точный `instructionBinding.revisionRef`, отдельно
от latest publication. `effective=false` означает, что сейчас используется
назначенный шаблон промпта; выбранная Instructions revision при этом сохраняется.
Новые typed SDK 641/writeback/72 сами по себе не означают завершённые consumers:
их оставшаяся реализация и полный runtime путь остаются в scope #1022.

Промежуточный пакет планов прошёл полный unit 1055/1055 (201 файл), lint,
format:check, production/synthetic build/typecheck, E2E TypeScript/discovery и
canonical Go/TS replay (пять generated файлов byte-identical). Первичные
format/lint ошибки диагностического helper и избыточных guards исправлены;
повтор прошёл. Известный production chunk warning сохраняется. Полный synthetic
на этом промежуточном пакете NOT RUN по согласованному порядку: один новый
полный прогон следует после оставшихся typed consumers 641/72/writeback и
общих E2E исправлений. Прежний full50d 34/35 FAIL остаётся в истории.

## Промежуточное подключение 641/72 и Git write-back

Потреблён coherent OpenAPI источник HTTP `65b38abf4e5aa619ea17c2eb1f493ce74e32123c`
с canonical Go/TS generation. Четыре server-owned каталога интеграционных
грантов сохраняют query/cursor/total, disabled reasons и точные версии выбранного
connection/project/recipient/capability. Изменение connection version сбрасывает
зависимые страницы. Existing grant command использует исходный connection OCC;
новые mutation pins не придуманы. Отдельного экрана ради USE query нет: текущие
формы выдают GRANT, а Git source не имеет Agent/capability context.

Просмотр инструкций и сохранённых этапов использует typed context catalog,
exact context digest и owner materialization. Для продолжения выбранная Session,
задача и finalized AttachmentSet поступают серверу до preview; изменение формы
сбрасывает результат. Пустой template у Workflow/continuation означает действующий
owner snapshot, а полный текст требует явного запроса `prompt.full.view`.
Declared promptScope передаётся create/save шаблона и сохраняет immutable refs.
Редактор Workflow использует typed `draft` при наличии; после save выполняется
защищённый refetch. Published-first карточки не подменяются Draft; preview этапа
связан с тем же body/ref, ordinal revision не преобразуется в opaque ref.

В ConfigurationEditor подключён GitWriteBackPanel: prepare, exact history/view,
явные approve/reject/cancel и recovery неизвестного исхода. Общий i18n registry
содержит локальные ru/en сообщения панели. При выходе очищаются recovery всех
закрытых writeback и publication форм. Повтор прежней UNKNOWN публикации не
очищает исходный intent при новом authority/OCC отказе; terminal outcome читается
через защищённый план. Raw live E2E helpers обновлены под catalog pins, разрешённый
моделью effort и обязательные планы Instructions.

На промежуточном дереве unit 1107/1107 PASS. Initial i18n registry FAIL исправлен
подключением сообщений без изменения проверки; initial lint FAIL исправлен.
Новые browser-сценарии 641/72/writeback, общий полный synthetic и live runtime
пока NOT RUN. Исторические browser FAIL и установленная причина EDQUOT остаются
выше; итоговый browser baseline использует disk TMPDIR и прежние flags/timeouts.
Продолжается полный scope #1022, checkpoint не означает окончание приёмки.

## Текущий остаток 641/72 и #1075

Coherent source SDK `65b38abf4` подключён: 641 typed context и Workflow Draft,
72 четыре каталога разрешений, VFS/67, планы 643/644/645/70 и Git write-back65.
Их отсутствие в ранних таблицах выше является историческим состоянием.
Публикация source, наличие UI и синтетический ответ не заменяют реальный
сквозной runtime результат.

- Workflow редактор сохраняет и повторно читает exact Draft, показывает
  серверную диагностику, Markdown и материализованный preview. После изменения
  входа уже проверенного продолжения Session запуск требует свежего preview.
- Каталог разрешений показывает server resource scope и disabled reasons.
  Human Gate открывается через существующие «Решения», без выдуманного фильтра
  по подключению. Intent/scope/effect preview отображаются из typed DTO.
- Bug #1075: HTTP `5726cd8dd` и исполняемый CP `a7761d6e` опубликованы с локальными
  проверками PASS; source/SDK shape не менялся. PWA scoped Gate390/2900 на
  `52219acda` PASS. Следующий consumer follow-up сохраняет literal i18n-like
  значения, ref-like строки и whitespace в scope/preview; unit/typecheck PASS,
  обновлённый browser fixture пока NOT RUN.
- Write-back contribution browser `84efbd16` прошёл 3/3 локальных сценария
  390/2900 у исполнителя; подключён в основной набор, теперь 38 тестов.
- Exact `52219acda`: unit 1114, lint, format, production build/typecheck и E2E
  check PASS; scoped Home390/2900 + Gate390/2900 — 4/4 PASS за 1,8 min.
  Exact `455b47825`: unit 1118/210 files PASS; полный synthetic38 пока NOT RUN.
- Новые scoped Home 390/2900 последовательно дали 0/2 FAIL: неточный tab label,
  неоднозначный вложенный summary, затем «Задача» вместо accessible «Задание».
  Селекторы исправлены без изменения timeout/assertion смысла; повтор 52219
  прошёл 4/4. Workflow, четыре каталога разрешений и обновление continuation
  preview пройдены полностью. Renderer crash на этих повторах не было.

Историческая повторная сверка всех первоисточников 01–61/CFG выявила три обязательных
контрактных остатка в том же unit: #1077 context-specific provider usage
(существующие account/credential/catalog/model/effort не подменяют capacity и
provider health), #1078 точная причина disabled Workflow launch, #1076
materialized prompt Автоматизации с schedule/occurrence и CONTINUE_ONE context.
Generic Agent preview не заменяет Automation preview. До этих source contracts
PWA не добавляет выдуманные DTO/reasons. Доступные consumer исправления включают
пять ближайших occurrences, provider/scope выбранного подключения, локализацию
catalog status и closed Environment readiness reasons. Disabled launch geometry
сохранена. Этот статус предшествует consumer 19/33/43, описанным ниже.

Исторические full34/35 FAIL и EDQUOT evidence сохраняются. После scoped исправлений
и финального SDK19/33/43 нужен полный synthetic набор с фактическим числом тестов на итоговом SHA. Реальные provider/browser/staging paths,
полнота исходных 01–61/CFG и review/owner gates ещё не приняты.

### Дополнение #1077: контекстная доступность аккаунта

Source и проверенный Go artifact взяты из HTTP `38c5967745d5b893505ba0c077928e3086378700`,
PWA SDK получен canonical generation. CONFIGURE передаёт выбранные Agent,
runtimeProfileRef, provider/model и effort уже опубликованного overlay.
Изменение любого поля контекста снимает прежний допуск; запоздалая страница,
другая account version и истёкшая usage не разрешают сохранение.
Выбор account до модели использует eligibleForSelection, полная форма —
allowedToSubmit. Занятая capacity и UNKNOWN health сами по себе не запрещают
разрешённую сервером отправку. Шесть факторов и способы исправления локализованы.
Health READY означает только подтверждённую доступность каталога конкретной
учётной записи с текущими credentials, область и время наблюдения показаны явно.
Admin читает без candidate context: model/actor NOT_EVALUATED не превращаются
в runtime READY; Source credential selector остаётся отдельным административным чтением.

Targeted unit 50/50 PASS; typecheck, E2E TypeScript и полный lint PASS.
Первичные ошибки порядка ru/en spread и alias/unused import тестовой оснастки
исправлены; они не были browser FAIL. Browser fixture в существующем
runtime-detail дополнен limited health/capacity/permission revoke и контрольными
screenshots; выполнение фиксируется отдельным exact checkpoint.
Home теперь проверяет точные viewport плана, включая 768×1024; прежний900×1080
сохранён дополнительным вариантом. Предыдущее количество38 относится только
к историческому набору. Полный актуальный запуск выполняется после consumer SDK33/43.

На `9bbee2a9f` functional browser19 прошёл2/2, но просмотр screenshot390 выявил
узкую колонку usage details. На `e292a9b34` детали занимают полную строку:
повтор2/2 PASS5.5s с дополнительным geometry assertion; оба screenshot390/2900
просмотрены исполнителем и root. Exact e292 unit1129/212 files, format,
production build/typecheck и E2E check PASS; lint FAIL по двум non-null assertions
в новом browser assertion. Они заменены явным закрытым guard; верхний badge
теперь явно обозначает доступность runtime profile, отдельно от account usage.

### Дополнение #1076/#1078: Automation renderer и допуск Workflow

Итоговый HTTP/SDK — `f1af3df7e74c2d336d11e977edcbb6c858514c92`, с исполняемым
CP `bebb`. OpenAPI и generated SDK совпадают с первоначально потреблённым
`5bf26db397ed529e4e3e51be68e3c38f17c0ee57`; повторная генерация не требуется.
Формулировки выше об отсутствии изменений SDK относятся к своим историческим
checkpoints: контракты 19/33/43 расширили текущий SDK.

Workflow показывает постоянное действие запуска и авторитетную причину отказа.
Сохранённый Draft требует публикации, даже если карточка ещё содержит прежнюю
Published revision. `allowedToSubmit` отделён от `operationalState`: разрешённая
постановка в очередь не запрещается из-за UNKNOWN или занятых ресурсов.

Обязательная «Задача» Автоматизации сохраняет `automationText`, который получает
Turn или root coordinator. Старый `input.task` остаётся самостоятельным входным
параметром и не подставляется вместо пустой задачи. Cron использует один источник
спецификации и пять ближайших запусков. Материализованный preview вызывает
существующий Schedule Preview с точным target/spec/version. DRAFT показывает
будущую ревизию текущего исполнителя и `REVISION_NOT_SAVED`; CURRENT_REVISION —
сохранённую задачу, revision/base и её исполнителя. Смена контекста отменяет
запрос и очищает результат. Полный текст запрашивается явно; непрошенный полный
ответ закрыто отклоняется. Каталог шести переменных и continuation Session diff
проверяются по точным owner pins.

Первый scoped Automation browser: 2900 PASS, 390 FAIL из-за длинного JSON output
тестовой оснастки. Визуально также обнаружен лишний alert без ошибки в новом
компоненте. Оба дефекта исправлены; повтор 2/2 PASS 4.6 s. Расширенный повтор с
полным текстом AGENT/WORKFLOW и задержанным ответом после смены задачи — 2/2 PASS
4.2 s. Это synthetic, не deployed renderer acceptance. Новый полный набор
использует фактическое количество сценариев; окончательный exact ledger находится
в PR #1067. Прежние FAIL и все NOT RUN live/review проверки сохраняются.

Полный synthetic на `8a0cfd03c5b855e5cd6d29d08fa7b339dba3b1ef`:
39/41 PASS, два Git source сценария FAIL за 7.0 min. После подключения Git
write-back panel правильно читала историю, но старый fixture не обслуживал
этот существующий GET и возвращал404. Fixture дополнен exact history read.
Все восемь Home viewport и три write-back сценария прошли. Визуальный readback
при этом обнаружил отдельный общий дефект: nullable preview/scope показывали
fallback alert без ошибки. `PromptTargetPreview` и `PromptScopeFields` теперь
показывают alert только при problem; намеренный AuthGate fallback сохранён.
Общий SSR сценарий и browser no-alert assertions закрепляют различие. Первый
SSR запуск упал из-за browser-only i18n import, изолированный createI18n исправил
оснастку. Финальный полный повтор относится к следующему exact checkpoint.

## Исправление BFF session lifecycle #1086

Источник OpenAPI и generated SDK: HTTP
`cad2fd1620c3544e0c51bf6d88f38aded70203ff`. PWA больше не выполняет token
exchange и не хранит OIDC tokens. BFF authorization/callback возвращают
HttpOnly cookies и metadata; браузер хранит только безопасное намерение возврата
и state. Таймер использует serverTime/renewAfter и пределы idle/absolute;
legacy REAUTHENTICATION требует нового входа. Повтор временного отказа ограничен
по числу попыток и сроку сессии. Wake и межвкладочный сигнал требуют защищённого
readback. Изменение generation/version обновляет WS handshake с сохранением
курсоров, а расход Reveal/Email purpose завершается GET session.

Локальные проверки пакета: 218 unit-файлов / 1172 теста PASS, typecheck и
E2E typecheck PASS, полный ESLint PASS, production build и build:synthetic PASS.
Scoped Home 390/2900 — 2/2 PASS за 1,8 минуты: BFF Email redirect/callback,
на 390 также background renewal и повторный SESSION_RESUME. Предупреждение
сборки о размере chunk сохранено. Полный synthetic всех исправлений review и
deployed/live acceptance пока NOT RUN.

Первый полный unit запуск завершился FAIL из-за EDQUOT в `/tmp`; повтор тех же
тестов с `TMPDIR=/home/s/.cache/kodex-pwa-browser-tmp` на диске прошёл.
Этот TMPDIR требуется и для Node unit, и для Playwright. Исторический отказ
не заменяется успешным повтором. Первые lint/format отказы исправлены без
отключения правил; поздний format отказ затронул только тест Reveal.

## Защищённый RoleImage source и STT rate limit

Следующий canonical OpenAPI/SDK взят из HTTP
`de81d413dc48a9b5125dd03df83b2819b7d96de4`: сохранён BFF #1086,
добавлены recipe/build sourceAvailable, provider lifecycle prerequisite и
typed STT #1088. Generated code создан штатным generate:openapi.

Recipe/build editor показывает Dockerfile и diff только при sourceAvailable.
Отказ свежего detail и несовпадение проекта очищают прежний cache/draft.
Metadata-only просмотр оставляет историю и собственные owner build actions.
Create affordance проверяет три self decisions точного проекта; чужой subject
не запрашивается. Для managed ROLE_IMAGE конфигураций owner ещё готовит
отдельную source projection: пустой content не доказывает source authority.
Полное закрытие #1083 этим checkpoint не заявляется.

STT TRANSCRIPTION_RATE_LIMITED сохраняется отдельно от generic quota и
недоступности. UI принимает только целый Retry-After 1..300 секунд, без
корректной подсказки срок не выдумывается. Ошибка не меняет текст пользователя
и не повторяет billable POST автоматически.

Проверено локально: 48 targeted unit-тестов, typecheck/E2E typecheck,
scoped lint и build:synthetic PASS. Home 390/2900 и typed STT 390/2900 —
4/4 PASS за 1,9 минуты. Screenshot STT390 просмотрен: сообщение помещается
в поле без горизонтального overflow. Первый typecheck FAIL выявил ALLOW
вместо canonical ALLOWED; fixtures исправлены также на DENIED. Первый scoped
lint FAIL (9 ошибок) исправлен без подавления правил. Production build нового
пакета, полный synthetic repair, managed source и provider owner acceptance
ещё NOT RUN; предыдущий production PASS относится к BFF checkpoint.

Context7 `/websites/vuejs` проверен для watch cleanup, flush sync и teardown.

## Проверенная документация библиотек

Для Playwright проверены Context7 `/microsoft/playwright/v1.61.0` и актуальный
package API: изоляция BrowserContext, automatic fixtures и storage state. Для
безопасного file IO проверена документация Node.js
`/websites/nodejs_latest-v24_x_api`: `open`, `O_EXCL`, `O_NOFOLLOW`, `fstat`,
`fsync` и `rename`. Для остальных frontend-зависимостей применяются источники
из `FE-DOC-001` и закреплённые версии `package-lock.json`.

# Промежуточная проверка исправлений Provider Account 1089/1091

Панель зависимостей использует authoritative blockers с фильтрами, cursor,
hiddenCount и выбором только разрешённых ожидающих Run. Команды удаления,
проверки авторизации, повторной авторизации и отмены очереди сохраняют исходные
OCC, idempotency key и неизменяемый intent. Неизвестный исход восстанавливается
явным повтором после защищённого чтения; автоматического resend нет.
DELETING остаётся в списке до серверного DELETED; удаление не имитируется отзывом.
Проверка действующей авторизации наблюдается защищёнными GET с ограниченным бюджетом.

Локально: 55 unit в 10 файлах, typecheck, scoped ESLint и E2E TypeScript — PASS.
Первые typecheck/ESLint выявили ошибки нового UI и fixture; они исправлены,
успешные повторные проверки указаны выше. Обновлённый browser fixture,
production build и полный synthetic набор этого пакета — NOT RUN.
Canonical SDK пока de81d413dc48a9b5125dd03df83b2819b7d96de4;
полный executable CP lifecycle 1089/1091 ещё WIP, его acceptance не заявляется.
Проверки Node используют TMPDIR на диске; библиотечный контракт Vue watch cleanup
сверен через Context7. Публикация, общий review и deployed/live — NOT RUN.

# Дополнение managed RoleImage 1083

Canonical OpenAPI/SDK обновлён с bfb336f7efa3d4abf85d3a72523e76a0ea89348b:
`sourceAvailable` определяет чтение revision, `sourceEditable` — изменение
ROLE_IMAGE. Для остальных configuration kinds отсутствие этих полей не меняет
поведение. Редактор убирает исходник, diff и write-back при отзыве доступа;
ошибка защищённого readback очищает прежнюю проекцию. Пагинация истории также
обновляет доступ. Impact/rebind сохраняют собственную authority; Git ownership
и OCC не заменяются новым флагом. Создание проверяет три self permission на
точном PROJECT либо ORGANIZATION, существующий объект использует owner projection.

Локально: canonical generation, typecheck/build:synthetic, 11 scoped unit,
scoped ESLint и E2E TypeScript — PASS. Home 390/2900 и write-back 390/2900 —
4 PASS. Два новых browser сценария отзыва source сначала FAIL из-за неоднозначного
локатора заголовка; после ограничения до прямого заголовка редактора — 2/2 PASS
(1.7 s). Исходники приложения между этими browser запусками не менялись.
Полный synthetic набор, production build текущего repair package, executable CP
acceptance, общий review и deployed/live остаются NOT RUN.

# Browser lifecycle учётной записи провайдера

`e2e/provider-lifecycle.synthetic.spec.ts` проверяет настоящий workspace на
390/2900 px: фильтрацию зависимостей и hiddenCount, выбор только ожидающего Run,
потерянный ответ отмены и явный повтор с исходными key/OCC/digest, отдельную
проверку AUTHORIZED и её защищённый GET readback, затем новую device authorization.
Используется безопасная synthetic оснастка, production provider не вызывается.
Повторный набор 4/4 PASS (12.5 s), build:synthetic PASS. Первый набор 4/4 FAIL:
оснастка задавала неверный CSRF cookie и использовала неточный locator select;
обе причины исправлены. Снимок выявил также отсутствующие общие переводы
AUTHORIZED/PENDING_AUTHORIZATION/REAUTHORIZATION_REQUIRED — они добавлены.

# История первой общей проверки repair package

Полный ESLint, format:check, production build с typecheck и публичный
test:e2e:check прошли локально. Полный unit запуск дал 1194 PASS / 3 FAIL:
два старых ROLE_IMAGE fixture не передавали sourceEditable; после их исправления
focused drafts 11/11 PASS. Третий отказ — строгая сверка двух новых image.source
permissions с ещё старым CP engine registry в этой ветке; проверки не ослаблены,
до потребления owner source и нового полного запуска весь unit остаётся FAIL.
Полный synthetic набор этого repair package пока NOT RUN.

# Последующая общая проверка repair package

После точного CP source prerequisite 727f90ca59904fd4d09680d844a67a292bf0e065
(engine permissions и migration649; owner остаётся CP1071) полный unit на
8c7207aa42fa9db0512ca31baf49bf55b8913344 прошёл: 1197/1197 в 220 файлах.
Предыдущий отказ 1194 PASS / 3 FAIL сохранён выше как история.

Полный synthetic на 8c720 был остановлен после доказанных ошибок оснастки:
27 PASS / 4 FAIL / 1 interrupted / 19 NOT RUN. Git source fixture не передавал
обязательные owner flags ROLE_IMAGE; VFS fixture принимал временную пустоту
loading за завершённую фильтрацию. Добавлены реальные source flags и ожидание
нового server request с kinds=MEMORY/lifecycleState=DELETED, затем возвращённой
Памяти и отсутствия Документа. Scoped повтор 4/4 PASS (15.2 s), E2E TypeScript
PASS. Runtime source между этими browser запусками не менялся.
На момент этого scoped checkpoint новый полный synthetic оставался NOT RUN;
его последующий результат приведён ниже. Executable CP и deployed/live
приёмка этими fixtures не заменяются.

# Итог полного synthetic repair package

Публичный `npm run test:e2e:synthetic` на точном
680dc57c776618f50b060be6558be753b4eb897b завершён: **51/51 PASS (7.4 min)**,
один worker, процесс завершён с кодом 0. Использован дисковый
`TMPDIR=/home/s/.cache/kodex-pwa-browser-tmp`. Прежний полный отказ
27 PASS / 4 FAIL / 1 interrupted / 19 NOT RUN остаётся историческим результатом.

Просмотрены контрольные снимки Home для 1280×900, 1440×900, 1920×1080,
2560×1440, 2900×1600, 390×844, 768×1024 и дополнительного 900×1080;
Automation и provider usage на 390/2900, заголовок Workflow с причиной
недоступного запуска. Новых дефектов геометрии на этих снимках не обнаружено.
Снимки находятся в `test-results/synthetic` соответствующего запуска.

Итог применимых локальных проверок: полный unit 1197/1197, ESLint, format,
production build/typecheck, публичный E2Echeck и полный synthetic — PASS.
Полный unit привязан к 8c7207aa42fa9db0512ca31baf49bf55b8913344;
после него изменялись только две browser fixtures и документация.
Canonical OpenAPI/SDK взят из bfb336f7efa3d4abf85d3a72523e76a0ea89348b.
Общий review нового repair package, executable owner acceptance и deployed/live
со стороны PWA — NOT RUN. Synthetic не вызывает настоящие OIDC/provider/STT.

# Подготовка optional API-key приёмки удаления

Существующий `integration-path.spec.ts` использует тот же единственный optional
API key account: авторизация, свежий каталог выбранной модели с точными pins,
реальный Run с проверкой affinity, затем DELETE и защищённый список зависимостей.
Для снятия собственного AGENT blocker выполняется существующий ARCHIVE с fresh
nextActions/OCC; immutable runtime policy не переписывается пустым списком и
другая учётная запись не создаётся. Архив не снимает чужие active/warm blockers.

Завершение проверяется через protected GET состояния DELETED, шесть нулевых
счётчиков и pendingCleanup=0, полный обход default catalog без удалённого ref,
read-only verifier полного cleanup graph и immutable Secret fences. Неизвестный DELETE
не повторяется автоматически; ошибка обязательного readback не считается
успешной очисткой. До согласованного executable owner checkpoint этот сценарий
не доказывает готовность backend. Deployed/live запуск этой редакции — NOT RUN;
понадобятся прежнее отдельное разрешение и optional fixture environment.

Verifier сверяет final owner CP `4e1339f` и broker `1df3ec330`: account/deletion
DELETED, отсутствие current credential и blockers, завершение всех tasks без
leases, cleanup каждой immutable credential revision. Metadata tasks требуют
завершённого successor по наблюдению; superseded/dead-letter и no-effect CAS
допускаются только с точной завершённой цепочкой. ProducedCredential проверяется
отдельно: её cleanup root не обязан иметь predecessor от producer task.
Kubernetes readback извлекает только metadata/type/immutable, названия data keys
и candidate `fence.json`, который проверяется внутри verifier без вывода значения.
Обычный NotFound не заменяет canonical fence, запрещающий позднюю материализацию.
У credential fence отсутствуют все прежние material UID; authorization fence
может сохранять исходный UID после CAS, но не credential/user-code data.
Verifier выполняется и после неизвестного authorize ACK: отсутствие ответа 200
не доказывает отсутствие материала. Пустой граф допустим только при отсутствии
всех immutable revisions и authorization attempts в terminal owner snapshot.

Pure fixtures запускаются из каталога PWA публичной командой
`npm run test:unit -- e2e/provider-cleanup-proof.test.ts`; они также включены
в обычный `npm run test:unit` через `e2e/**/*.test.ts`.
`npm run test:e2e:check` проверяет их TypeScript и подключённый live-сценарий
без обращения к стенду. Pure fixtures не объявляют SQL/Kubernetes или live
приёмку успешной; окончательная проверка этих границ выполняется прежним
авторизованным optional API-key сценарием.
Один cleanup readback ограничен 60 секундами; общий polling — 180 секундами.
Context7 `/kubernetes/website`: проверены `kubectl` machine-oriented
`go-template` output и immutable Secret semantics.

Для deletion.FAILED предусмотрен один отдельный этап восстановления: защищённое
чтение подтверждает FAILED и server DELETE, затем новая команда использует новый
key и свежий OCC. В polling нет мутаций; повторная ошибка не запускает следующий
новый intent. Неизвестный ответ любой Delete имеет отдельный ограниченный этап
exact replay прежних key/OCC после protected GET; один GET не выдаётся за receipt.
Ошибки 4xx не запускают replay. UI также закрывает новый Delete без server action,
а неразрешённый старый UNKNOWN нельзя заменить новым intent.

Эта ветка восстановления проверена локально: provider unit 56/56 в 9 файлах,
E2E TypeScript и форматирование — PASS. Первый scoped ESLint выявил три ошибки
типизации mock.calls в новых тестах; после типизированного mock scoped ESLint
и focused lifecycle 8/8 — PASS. Полный synthetic и live этой редакции — NOT RUN;
следующий общий browser запуск выполняется после окончательного CP/SDK.

Подготовка проверена без подключения к стенду: E2E TypeScript, scoped ESLint,
Prettier и публичный `test:e2e:check` (discovery всех профилей) — PASS.
После неё потреблён canonical OpenAPI/SDK
593117587f6d1a72889811046d6c307371b478f9: существующий UPDATE_PROJECT включён
в оба generated enum. Добавлены два согласованных safe summary для запроса
проверки credential и обработки выбранной очереди, без обещания отмены всех Run.
Canonical generation и точное сравнение с source SDK — PASS; context/catalog
unit 17/17, vue-tsc, scoped ESLint/format — PASS. Полный synthetic 51/51 выше
остаётся результатом точного 680dc57, а не новым запуском этой редакции.

# Финальный baseline после owner repair

Принят executable CP 4e1339f373ca2739ff5c33d88578e45ab377eb87 через проверенный
HTTP f0c4124b6c3f1381c6c25372e23807aaab58bc40. OpenAPI и generated SDK побайтово
совпадают с уже потреблённым 5931175; дополнительных полей или ручных правок
generated code нет. Actual queued summary содержит ACCOUNT и уже переведён.
Все семь context kinds, owner RETRY и раздельные source read/edit guards сохранены.

Добавлены browser сценарии FAILED deletion на 390/2900: без server DELETE кнопки
нет, UNKNOWN блокирует новый intent, явный replay использует исходные key/OCC,
следующее подтверждение — новый key/current OCC до protected DELETED. Targeted
2/2 PASS (3.5 s), E2E TypeScript и build:synthetic PASS.

На момент фиксации этого checkpoint полный новый baseline ещё не заявляется.
Полные lint/format/typecheck/unit/production build/E2Echeck/synthetic выполняются
на его неизменном clean SHA; точный результат и SHA256 логов передаются отдельным
private отчётом manager без последующего docs-only изменения проверенного SHA.
Общий review и deployed/live этой редакции остаются NOT RUN.

## Авторитетные карточки каталогов — #1098

Project использует обязательный `integrationState` и nullable `lastActivityAt`;
дата изменения metadata не подменяет активность. Agent получает eligible
`currentRunRef`: только действительная работа имеет центральный блок со ссылкой
на `/runs/:ref`, READY остаётся одним нижним badge. Workflow использует целиком
серверный `cardSummary`: этапы, уникальные сотрудники, параллельные группы,
решение человека, активные запуски, ожидающие решения и последняя активность.
Счётчики не вычисляются из неполного массива steps или страниц запусков.

Глобальный и проектный каталоги используют одни компоненты карточек и
`listOrganizationAgents`/`listOrganizationWorkflows` с проверяемым сервером
project filter, query и cursor. Карточки Процессов занимают не более двух
колонок; bounded область и modal сохраняют серверную загрузку следующих страниц.
Действия открывают точный проектный контекст. Запуск требует version/revision-
bound `launchReadiness`, редактирование — `EDIT`. Ссылка на запуски открывает
`/projects/:projectRef/runs`: существующий target filter разрешён только в
resumable режиме и не выдаётся за полный каталог запусков Процесса.

Platform invalidation перечитывает проекции с первого cursor. Обычный RUN
сохраняет карточку до свежего ответа; смена authority очищает старые страницы.
Отменённая загрузка и старый completion не восстанавливают прежний owner scope.
PWA закрытый реестр включает новый safe token `RUNTIME_LEASE_EXPIRED` в ru/en.

Synthetic `cards` проверяет реальные компоненты, разрешённые/запрещённые
действия, nullable activity и изменение runRef на390/2900. Основная browser
оснастка отдельно проверяет production routes global/project, server query,
cursor, переход к редактированию и обновление агрегатов. DTO-компонентная
оснастка не заменяет этот маршрут или live acceptance. Документация Vue,
CodeMirror и Playwright проверена через Context7. Новый полный baseline и
общий review привязываются к exact SHA отдельно; staged/live здесь не заявляются.
