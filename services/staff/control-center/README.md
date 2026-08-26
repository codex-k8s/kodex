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

| Route                                                     | Назначение                                                                |
| --------------------------------------------------------- | ------------------------------------------------------------------------- |
| `/onboarding`                                             | first-run и проверка горячего Системного помощника                        |
| `/assistant`                                              | полноразмерный workspace Системного помощника                             |
| `/`                                                       | глобальная сводка активной работы и ожидающих решений                     |
| `/projects`                                               | каталог и создание Проектов                                               |
| `/projects/:projectRef`                                   | обзор выбранного Проекта                                                  |
| `/projects/:projectRef/agents`                            | ИИ-сотрудники Проекта                                                     |
| `/projects/:projectRef/agents/:agentRef`                  | профиль, инструкции, capabilities, образ роли и запуск сотрудника         |
| `/projects/:projectRef/workflows`                         | Процессы Проекта                                                          |
| `/projects/:projectRef/workflows/:workflowRef`            | настройка, публикация и запуск Процесса                                   |
| `/projects/:projectRef/runs/new`                          | прямой запуск сотрудника или Процесса                                     |
| `/runs`, `/projects/:projectRef/runs`                     | глобальный и project-scoped список запусков                               |
| `/runs/:runRef`                                           | live graph, timeline, Human Gate, artifacts, cancel, retry и continuation |
| `/projects/:projectRef/files`                             | входные файлы, знания и результаты                                        |
| `/projects/:projectRef/automations`                       | расписания сотрудников и Процессов                                        |
| `/integrations`                                           | необязательные connections, tests, capabilities и grants                  |
| `/decisions`                                              | долговечные Human Gates                                                   |
| `/administration/access`, `/projects/:projectRef/members` | organization и project access                                             |
| `/administration`                                         | platform capabilities и диагностика установки                             |
| `/administration/audit`                                   | аудит действий и ошибок конфигурации                                      |

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

Повторный `npm run codegen` должен оставлять generated diff чистым.

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
