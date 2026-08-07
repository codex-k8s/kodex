---
id: FE-MC-CC-001
title: Staff Control Center MatterCodex
type: frontend-guide
status: approved
owner: manager
version: 1.0.0
updated: 2026-08-07
---

# Staff Control Center

`services/staff/control-center` — Vue 3 PWA владельца платформы. Текущий slice
Issue #194 намеренно ограничен полностью материализованными операциями
`control-api-gateway` на base SHA
`46b119db85fe338660bb642cd79e1913b5055f37`. Недостающие owner operations
остаются зависимостями [#236](https://github.com/codex-k8s/matter-codex/issues/236)
и [#237](https://github.com/codex-k8s/matter-codex/issues/237); этот slice не
объявляет Issue #194 завершённой.

## Локальный запуск

```bash
npm ci
npm run codegen
npm run dev
```

Runtime-настройки загружаются из `/config/runtime-config.json` до создания
Vue application. Схема закрыто отклоняет HTTP, credential в URL, неизвестную
форму и timeout вне разрешённого диапазона. OIDC Authorization Code + PKCE
использует `sessionStorage` только для временного protocol state. Полученный
bearer передаётся однократно в `createOwnerSession`, затем удаляется; рабочие
запросы используют только `Secure`/`HttpOnly` server session cookie и CSRF
double-submit boundary. Credential values и server secrets в PWA отсутствуют.
Полученный от сервера session `ETag` сохраняется отдельно как OCC-версия для
достижимого logout после reload/в соседней вкладке; он не заменяет cookie или
CSRF proof и удаляется при logout либо `401`.

PWA и browser API работают на одном origin
`https://control.mattercodex.local`. Static nginx проксирует только `/api/v1/`
к `control-api-gateway` с exact TLS SNI `control-api.mattercodex.local` и
проверкой его публичной CA. Это необходимо, чтобы браузер мог читать host-only
non-HttpOnly CSRF cookie, пока session cookie остаётся HttpOnly; cross-host API
URL сделал бы mutations и WebSocket handshake недостижимыми.
Runtime ConfigMap одновременно задаёт URL и соответствующий exact CSP; overlay
обязан менять их вместе, поэтому endpoint policy не зашита только в build-time
bundle и не расширяется до произвольных `https:`/`wss:` sources.

## Архитектура и generated boundary

- `main/App -> app -> pages -> features -> shared/api|lib|ui -> shared/api/generated`;
- pages только композируют feature stores и формы;
- stores защищены монотонным request generation: старый response/problem не
  перезаписывает новый, mutation всегда делает authoritative reload;
- adapters вызывают только сгенерированные OpenAPI SDK operations, добавляют
  timeout, CSRF, `Idempotency-Key` и `If-Match`, затем нормализуют единый
  `Problem`;
- AsyncAPI модели генерируются Modelina, а typed WebSocket adapter проверяет
  complete snapshot, channel, sequence и payload shape. Несовпадающие
  wire-reserved имена (`type`, `name`) преобразуются только в adapter, generated
  files не редактируются. Codegen semantic canonicalizer даёт устойчивые имена
  inline schedule/OwnerGate schemas вместо недетерминированных номеров
  `AnonymousSchema_*` Modelina;
- Git-owned конфигурация не изменяется общим update: доступны только
  специализированные `detachAccessResource` и `copyAccessResource` с reload;
- PWA не содержит fake data, production mocks, fixtures, ручных API URL или
  форм ввода внутренних UUID.

Генерация воспроизводится командами:

```bash
npm run generate:openapi
npm run generate:asyncapi
```

Source contracts:

- `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`.

## Исполняемые маршруты current contracts

| Маршрут                     | Авторитетные operations и состояние                                                                                                                                                                                                                             |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/`                         | `listProjects`, `listRuns`, `listResources(OWNER_GATE)`, `listIncidents`, `listBackups`, `getDiagnostics`; сводка только из readbacks                                                                                                                           |
| `/workspaces`               | `listProjects`, `createProject`; имя, slug и locale задаёт пользователь, owner/id/version — сервер                                                                                                                                                              |
| `/workspaces/:projectId`    | `listProjects`, `listResources`, `updateProject`, `deleteProject`, `createResource` для текущих mutable kinds, `detachAccessResource`, `copyAccessResource`; credentials выбираются из `CREDENTIAL_BINDING` catalog                                             |
| `/role-images`              | `listResources`, `getRoleImageRecipe`, `manageRoleImageRecipe`, `getRoleImageBuild`, `manageImageBuild`; секретов в форме/readback нет                                                                                                                          |
| `/automations`              | `listResources` catalogs, `createSchedule`, `updateSchedule`, `deleteSchedule`, `runScheduleNow`, `listScheduleOccurrences`, `resolveScheduleRecovery`; opaque refs выбираются по отображаемому имени, recovery evidence приходит только из occurrence readback |
| `/runs`                     | `listRuns`, `listResources(OWNER_GATE)`, `resolveOwnerGate`; форма решения показывается только при authoritative `resolvable=true` и `nextAction=RESOLVE`                                                                                                       |
| `/operations/incidents`     | `listIncidents`; текущий контракт read-only                                                                                                                                                                                                                     |
| `/operations/backups`       | `listBackups`, `listRestoreOperations`, `restoreBackup`; action показывается только при `restorable=true`                                                                                                                                                       |
| `/operations/audit`         | `listAuditEvents`; redacted owner metadata                                                                                                                                                                                                                      |
| `/operations/configuration` | `listConfigurationChanges`; авторитетная read projection                                                                                                                                                                                                        |
| `/operations/diagnostics`   | `getDiagnostics`; bounded counters без private refs                                                                                                                                                                                                             |
| `/search`                   | `searchResources` одного закрытого `ResourceKind`                                                                                                                                                                                                               |

Каждая collection/detail surface обрабатывает `loading`, `empty`, `error`,
`forbidden`, `ready` и OCC `conflict`; retry повторно читает авторитетное
состояние. HTTP и WebSocket old responses защищены от stale overwrite.

## Минимальная operation-level gap matrix #236/#237

Ниже отсутствуют именно browser paths на текущем OpenAPI. Эти действия не
включены в production navigation и не подменены generic Resource CRUD.

| Экран и пользовательское действие                                                                                    | Отсутствующий browser endpoint / operationId / schema                                                                                                                                                                  | Ожидаемый handler и generated adapter `control-api-gateway`                                                   | Exact downstream RPC / авторитетный владелец                                                                                                                                                                                                                                                                                                                                                                                                           | OCC, idempotency, authority, readback и realtime                                                                                                                                                                                | Dependency                                    |
| -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| Workspace: выбрать Mattermost Team, создать Team, link/read/relink/unlink                                            | `listMattermostTeams`, `createMattermostTeam`, `linkWorkspaceMattermostTeam`, `getWorkspaceMattermostMapping`, `relinkWorkspaceMattermostTeam`, `unlinkWorkspaceMattermostTeam`; safe Team/Mapping/Operation readbacks | специализированные owner handlers + generated interaction/control-plane clients; без generic resource handler | `interactiongateway.v1`: `ListMattermostTeams`, `CreateMattermostTeam`, `LinkMattermostTeam`, `GetMattermostTeamBinding`, `RelinkMattermostTeam`, `UnlinkMattermostTeam`, `GetMattermostTeamMappingOperation`, `GetMattermostTeamProviderReadback`; `controlplane.v1`: `ManageWorkspaceMattermostMapping`, `GetWorkspaceMattermostMapping`, `ListWorkspaceMattermostMappings`; эффекты Mattermost — interaction-gateway, mapping state — control-plane | project/actor только из owner session; Team повторно read back provider-side; mutation CSRF + semantic idempotency + current mapping `If-Match`; operation status и complete configuration snapshot без secret/provider payload | #237, downstream уже материализован #235/#234 |
| ИИ-сотрудники и роли: list/detail/create/update/archive/history                                                      | typed RoleDefinition/Agent catalogs, detail, command и history schemas/operations                                                                                                                                      | отдельные `RoleDefinition` и `Agent` handlers/adapters; универсальный protected CRUD запрещён                 | `ManageRoleDefinition`, `GetRoleDefinition`, `ListRoleDefinitions`, `ListRoleDefinitionHistory`; `ManageAgent`, `GetAgent`, `ListAgents`, `ListAgentHistory`; control-plane                                                                                                                                                                                                                                                                            | server assigns owner/ID; aggregate resolved in owner boundary before OCC/idempotency; typed versioned readback, config event и complete snapshot                                                                                | #237, downstream #234                         |
| Назначения: выбрать agent и role, assign/unassign/read history                                                       | AgentAssignment catalog/detail/command/history operations; selectors projection                                                                                                                                        | отдельный assignment handler + generated control-plane adapter                                                | `ManageAgentAssignment`, `GetAgentAssignment`, `ListAgentAssignments`, `ListAgentAssignmentHistory`; control-plane                                                                                                                                                                                                                                                                                                                                     | обе selected сущности повторно разрешаются сервером в одном tenant; `If-Match`/idempotency; assignment version/history/readback и configuration snapshot                                                                        | #237, downstream #234                         |
| Инструкции: content editor, validate/publish, history/compare/rollback, Git detach/copy                              | InstructionSet content/version/validation/diff schemas и специализированные operations                                                                                                                                 | InstructionSet handlers + generated control-plane adapter; Git update закрыт, detach/copy отдельны            | `ManageInstructionSet`, `GetInstructionSet`, `ListInstructionSets`, `ListInstructionSetHistory`, `CompareInstructionSetVersions`; control-plane                                                                                                                                                                                                                                                                                                        | `managed_by/source/revision/content digest` server-owned; OCC current version, semantic receipt; immutable history/diff/current effective snapshot; rollback создаёт новую version                                              | #237, downstream #234                         |
| Provider accounts: catalog, device authorization start/status/new-code, reauthorize/revoke, masked connection status | provider/authorization/connection schemas и operations отсутствуют целиком                                                                                                                                             | owner provider handlers + generated integration-gateway adapter                                               | ожидаемые #236 `ListProviders`, `StartProviderAuthorization`, `GetProviderAuthorization`, `RestartProviderAuthorization`, `ReauthorizeProviderConnection`, `RevokeProviderConnection`; integration-gateway                                                                                                                                                                                                                                             | owner/project из verified context; one active fenced attempt; connection generation + idempotency/OCC; masked terminal readback и authorization progress без token/device secret                                                | #236 → #237                                   |
| Provider pools: catalog/create/update/archive policy/weights                                                         | ProviderPool selections/effective eligibility schemas и commands отсутствуют                                                                                                                                           | pool handlers координируют два generated adapters, не вычисляя eligibility                                    | #236 `ManageProviderPool`/eligibility observation — integration-gateway; `ManageProviderPool`, `GetProviderPool`, `ListProviderPools`, `ListProviderPoolHistory` — control-plane                                                                                                                                                                                                                                                                       | selected connection revisions read back; server resolves membership; pool `If-Match`/idempotency; desired/effective versions and digests + config snapshot                                                                      | #236 → #237, control-plane часть #234         |
| Интеграции: definition catalog/detail/configure/test/capability assignment                                           | IntegrationDefinition/config/test/capability schemas и operations отсутствуют                                                                                                                                          | специализированные integration handlers + generated integration-gateway/control-plane adapters                | #236 `ListIntegrationDefinitions`, `GetIntegrationDefinition`, `ConfigureIntegration`, `TestIntegrationConnection`; definition/effect owner — integration-gateway, project ref metadata — control-plane                                                                                                                                                                                                                                                | immutable definition digest + connection revision + policy version; no caller-created capability; idempotency/OCC; bounded safe test taxonomy и masked effective readback                                                       | #236 → #237                                   |
| Согласования: list/get immutable preview, approve/reject                                                             | owner ApprovalRequest list/get/decision schemas и mapping отсутствуют                                                                                                                                                  | approval handlers + generated integration-gateway adapter; current invocation lifecycle reused                | #236 owner `ListApprovalRequests`, `GetApprovalRequest`, existing specialized decision path in integration-gateway/control-plane continuation                                                                                                                                                                                                                                                                                                          | stored invocation/context proves owner eligibility; exact request hash + version + idempotency, one terminal winner; immutable redacted preview и terminal readback/event                                                       | #236 → #237                                   |

Названия ещё отсутствующих RPC из #236 приведены как требуемый contract delta и
могут быть уточнены только contract-first в этом independently deliverable
gateway unit. PWA не строит предположительный client до появления source
OpenAPI + handler + adapter + authoritative consumer/readback.

## Сборка и deploy ownership

`Dockerfile` собирает production assets и запускает непривилегированный nginx
с read-only root filesystem. `nginx-security-headers.conf` задаёт закрытый CSP,
frame/object запреты и `no-store` для shell/runtime config; asset cache не
распространяется на runtime config. SPA fallback и service worker не кэшируют
owner data.

Kustomize base находится в `deploy/k8s/base/staff-control-center`: Deployment,
Service, Ingress, runtime ConfigMap, PDB, ServiceAccount и default-deny
NetworkPolicy. PWA pod не получает service account token; egress разрешён
только к exact DNS pods и `control-api-gateway:8443`. В volume
монтируется только публичный `ca.crt`, без server certificate/private key. OIDC
вызывается браузером напрямую по TLS. Манифесты не применялись ни к одному
контуру.

## Документация библиотек

Context7 повторно проверен для Vue и `@hey-api/openapi-ts`, но вернул
`Monthly quota exceeded`. Поэтому использованы только официальные документы
Vue, Vite, Pinia, Vue Router, vue-i18n, Lucide и официальные generated-client
настройки `@hey-api/openapi-ts`. Ограничение Context7 должно оставаться явно
указанным до готового PR.

## Ручная проверка

1. Подменить только deploy runtime ConfigMap на URL тестового контура, открыть
   OIDC вход и убедиться, что bearer не появляется в `localStorage`, а cookie
   недоступна JavaScript.
2. На desktop и mobile пройти все маршруты в RU/EN, light/dark; проверить menu,
   focus-visible, таблицы, формы и отсутствие наложений.
3. Для каждого list path проверить loading/empty/error/403/ready, отключение
   WebSocket и восстановление complete snapshot.
4. Для project/resource/schedule/role-image/OwnerGate/restore mutations
   воспроизвести stale `If-Match`, обновить readback и повторить действие с
   новым idempotency key.
5. Проверить, что Git-owned row не имеет общего edit, а detach/copy завершаются
   только после server readback; backup restore и gate resolve показываются
   только по authoritative affordance.

Тяжёлый integration/E2E/deploy/render/lifecycle контур отложен в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).
