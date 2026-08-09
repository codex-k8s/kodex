---
id: FE-MC-CC-001
title: Staff Control Center MatterCodex
type: frontend-guide
status: approved
owner: manager
version: 2.0.0
updated: 2026-08-09
---

# Staff Control Center

`services/staff/control-center` — production PWA владельца MatterCodex из
Issue [#194](https://github.com/codex-k8s/matter-codex/issues/194). PWA работает
только через browser-facing API `control-api-gateway`; source contracts
находятся в:

- `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`.

В приложении нет fake data, production mocks, fixtures, ручных HTTP routes или
второй реализации control-plane rules. Pages и components композируют feature
stores; generated SDK вызывают только handwritten adapters. UI показывает
только owner-safe display projections и masked provider status. Internal refs
передаются после выбора авторитетной записи и не запрашиваются у владельца
вручную.

## Локальный запуск

```bash
npm ci
npm run codegen
npm run dev
```

Runtime-настройки загружаются из `/config/runtime-config.json` до создания Vue
application. Parser закрыто отклоняет неизвестные поля, HTTP, URL с query,
credentials или fragment, несогласованные HTTP/WebSocket origins и timeout вне
допустимого диапазона. Build-time secrets отсутствуют.

OIDC Authorization Code + PKCE хранит временный protocol state только в
`sessionStorage`. Bearer используется один раз для `createOwnerSession`, после
чего рабочие запросы используют `Secure`/`HttpOnly` host-only session cookie.
Mutation adapter каждый раз добавляет CSRF double-submit token,
`Idempotency-Key`, а для OCC — exact `If-Match`. Авторитетный `ETag` сохраняется
только как версия session/resource. Safe `Problem` передаёт UI лишь code,
status, retryability и correlation ID; downstream error и private evidence не
показываются.

PWA и API имеют один origin. Nginx проксирует `/api/v1/` к
`control-api-gateway` по TLS с exact SNI и публичной CA. Благодаря этому browser
передаёт session cookie, а JavaScript читает только host-only CSRF cookie.

## Пользовательские маршруты

| Маршрут                                 | Исполняемые сценарии                                                                                                                                                 |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/`                                     | Авторитетная сводка workspaces, runs, OwnerGate, incidents, backups и diagnostics.                                                                                   |
| `/workspaces`, `/workspaces/:projectId` | Workspace CRUD, repositories/chats, Mattermost Team create/list/link/relink/unlink, Git-owned access detach/copy.                                                    |
| `/people`                               | RoleDefinition и Agent create/archive/history, server runtime selections, bot identity bind/rebind/revoke, AgentAssignment assign/unassign.                          |
| `/instructions`                         | InstructionSet create/update, validate, publish, history, compare, rollback, Git-owned detach/copy.                                                                  |
| `/providers`                            | Provider catalog, device authorization/status/new code/cancel, masked connections, reauthorize/revoke, provider pools create/update/archive и effective eligibility. |
| `/integrations`                         | Definition catalog, configure/update, safe connection test receipt, immutable redacted approvals и approve/reject.                                                   |
| `/role-images`                          | RoleImageRecipe и ImageBuild специализированные команды и readbacks.                                                                                                 |
| `/automations`                          | Server-derived selectors, presets/defaults, INLINE либо CLEAN artifact prompt, create/rebind и effective schedule values.                                            |
| `/runs`                                 | Run list/detail, timeline, lineage, artifacts, authoritative next actions и OwnerGate.                                                                               |
| `/operations/incidents`                 | Incident detail/history/runbook и только возвращённые сервером next actions.                                                                                         |
| `/operations/backups`                   | Workspace backup create/cancel/retry и restore create/cancel/retry с membership snapshot.                                                                            |
| `/operations/audit`                     | Bounded audit list и CSV export.                                                                                                                                     |
| `/operations/configuration`             | Configuration changes, safe source detail и redacted version diff.                                                                                                   |
| `/operations/diagnostics`               | Bounded diagnostics и complete health observations от трёх владельцев состояния.                                                                                     |
| `/search`                               | Авторитетный поиск одного закрытого `ResourceKind`.                                                                                                                  |

Каждая read surface имеет `loading`, `empty`, `forbidden`, `error` и `ready`.
Mutation с `409`/`412` переводит применимую поверхность в `conflict`, после чего
нужен свежий readback. Request generation не позволяет старому HTTP response
перезаписать новый.

`managed_by`, `source`, `revision` и drift никогда не вычисляются UI. Для
Git-owned конфигурации общий edit закрыт: `detach` и `copy` являются отдельными
подтверждаемыми командами. Secret values, private locators, tokens, cookies,
credential material и raw provider evidence не выводятся.

## Generated boundary

OpenAPI client генерируется закреплённым `@hey-api/openapi-ts`, AsyncAPI models —
закреплённым `@asyncapi/cli`/Modelina. Generated files не редактируются вручную.

```bash
npm run generate:openapi
npm run generate:asyncapi
npm run codegen
```

`tools/generate-asyncapi.mjs` удаляет прежний output перед генерацией. Для
актуального контракта semantic model names определены source AsyncAPI; fallback
canonicalizer применяется только если generator снова создаст anonymous
schemas. Повторный `npm run codegen` должен оставлять relevant diff чистым.

## Realtime, offline и обновление PWA

WebSocket URL приходит только из runtime config. Handwritten adapter проверяет
closed channel set, complete envelope, channel-specific единственный items key,
типы safe projections и монотонный `sequence`. Snapshot полностью заменяет
локальную проекцию; старый sequence игнорируется. После reconnect sequences
сбрасываются для новой подписки, а UI остаётся в состоянии «обновление снимка»,
пока не получены свежие complete snapshots всех десяти каналов.

При offline owner actions в content блокируются, данные явно отмечаются как
устаревшие. Service worker не кэширует private API, auth, runtime config или
navigation responses и при activation удаляет прежние Cache Storage entries.
Новая версия не активируется скрыто: UI показывает update notice и отправляет
`SKIP_WAITING` только после действия владельца. `/sw.js`, runtime config и SPA
shell обслуживаются с `no-store`; fingerprinted assets — immutable.

## Сборка и deploy ownership

`Dockerfile` выполняет production build и запускает nginx от UID/GID 101 с
read-only root filesystem. Nginx задаёт CSP, `frame-ancestors`, object/base
запреты, Permissions Policy, Referrer Policy и exact upstream TLS validation.

Kustomize base `deploy/k8s/base/staff-control-center` содержит Deployment,
Service, Ingress, ConfigMap, PDB, ServiceAccount и default-deny NetworkPolicy.
Pod не получает service account token. Egress разрешён только exact kube-dns и
`control-api-gateway` pods в namespace `mattercodex-system` на TCP 8443. В pod
монтируется только публичная `ca.crt`, без private key. Readiness проверяет
локальный `/readyz`; никакие deploy, staging или production actions из frontend
волны не выполняются.

## Проверки Prototype-профиля

Публичные быстрые точки входа:

```bash
npm run format:check
npm run lint
npm run typecheck
npm run build
npm run codegen
git diff --check
```

Тяжёлые integration/E2E/contract/deploy/render/lifecycle suites не входят в
Prototype-профиль и отложены в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

## Проверенная документация библиотек

Context7 был вызван для Vue, TypeScript, Vite, Pinia, Vue Router, vue-i18n,
`@hey-api/openapi-ts` и AsyncAPI Modelina, но вернул `Monthly quota exceeded`.
Поэтому использована официальная первичная документация:

- Vue Composition API и TypeScript: <https://vuejs.org/guide/typescript/composition-api>;
- TypeScript `strict`: <https://www.typescriptlang.org/tsconfig/strict>;
- Vite production build: <https://vite.dev/guide/build>;
- Pinia stores: <https://pinia.vuejs.org/core-concepts/>;
- Vue Router: <https://router.vuejs.org/guide/>;
- Vue I18n Composition API: <https://vue-i18n.intlify.dev/guide/advanced/composition>;
- Hey API OpenAPI TypeScript: <https://heyapi.dev/docs/openapi/typescript/get-started>;
- AsyncAPI CLI: <https://github.com/asyncapi/cli/tree/v6.0.2>;
- AsyncAPI Modelina: <https://github.com/asyncapi/modelina/tree/v4.4.3>.

## Ручная проверка владельцем

1. В тестовом runtime ConfigMap заменить только публичные URLs и связанные exact
   CSP sources. Пройти OIDC login/logout; убедиться, что bearer отсутствует в
   `localStorage`, а session cookie недоступна JavaScript.
2. На desktop/mobile и light/dark пройти все маршруты в RU/EN. Проверить
   keyboard navigation, focus-visible, modal focus/escape, таблицы и карточки.
3. Для каждой collection проверить loading/empty/403/error/ready. Отключить сеть,
   убедиться в stale/offline notice и заблокированных actions; восстановить сеть
   и дождаться complete replacement всех realtime channels.
4. Для Workspace Team, Role/Agent/Assignment, InstructionSet, provider pool,
   integration, approval, schedule, Run, Incident и backup/restore воспроизвести
   stale `If-Match`; обновить readback и повторить явное действие.
5. Проверить device authorization/new code/cancel, только masked provider fields,
   immutable approval preview, safe integration test taxonomy и отсутствие
   private IDs/locators/evidence в отображаемом UI.
6. Проверить, что Git-owned InstructionSet/Role не имеет общего edit, а detach и
   copy требуют подтверждения и завершаются authoritative readback.
7. Проверить audit CSV, configuration source/diff, run timeline/lineage/artifacts,
   health observations и workspace backup/restore next actions.
8. Опубликовать новый image в тестовом registry только отдельной SRE-волной и
   убедиться, что update notice появляется до активации нового service worker.
