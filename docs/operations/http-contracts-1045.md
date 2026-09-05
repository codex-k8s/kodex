---
id: OPS-HTTP-1045
title: HTTP-контракты завершения MVP
type: operations
status: approved
owner: developer
version: 0.2.0
updated: 2026-09-05
---

# HTTP-контракты завершения MVP

Источники: Issues #1045, #1046, epic #1018; `GUIDE-DOC-004`,
`GUIDE-DOC-006`, `GOV-DOC-003`. Это карта интеграции, не заявление полного
acceptance. CP proto и authority policy принадлежат #1046; HTTP потребляет
зафиксированные producer checkpoints. PWA использует сгенерированный SDK.

## Identity и environment impact

Producer: `f8814bfec551afae5e72c5fafa1948fbbdc2e7bc` и его предшественники.
Для всех операций actor/organization происходят из проверенной browser session,
OIDC bearer и подписанного authority context generated CP client. Payload,
path ref и `X-Kodex-Project-ID` не выдают полномочий. Ниже paths имеют префикс
`/api/v1`; организация не требует выбранного проекта.

| HTTP / SDK | CP RPC | Authority / OCC | Результат и lifecycle |
|---|---|---|---|
| GET `integration-connections/{connectionRef}/interaction-identities`, `listInteractionIdentities` | `ListInteractionIdentities` | `access.manage` организации; cursor CP привязан к actor/tenant/connection | `items`, `nextPageToken`; только авторитетное чтение, без события |
| POST тот же path, `bindInteractionIdentity` | `BindInteractionIdentity` | `integration.manage`; `If-Match` = версия **connection**, idempotency + CSRF | 201 identity ACTIVE, ETag = версия identity; CP назначает ref, проверяет subject и активное Mattermost connection, атомарно сохраняет audit/idempotency |
| DELETE `interaction-identities/{identityRef}`, `revokeInteractionIdentity` | `RevokeInteractionIdentity` | `integration.manage`; `If-Match` = версия **identity**, idempotency + CSRF | 200 identity REVOKED и ETag; CP разрешает owner до OCC; последующие dispatch eligibility проверяют актуальное состояние |
| GET `runtime-environments/{environmentRef}/versions/{versionRef}/impact`, `getRuntimeEnvironmentImpact` | `GetRuntimeEnvironmentImpact` | `project.manage` владельца environment; CP фильтрует consumers | target digest, environmentVersion/ETag, consumers/total/cursor; чтение без события |
| POST `runtime-environments/{environmentRef}/versions/{versionRef}/consumer-bindings`, `rebindRuntimeEnvironment` | `RebindRuntimeEnvironment` | `project.manage` и `agent.manage` каждого выбранного consumer; `If-Match` = **environmentVersion**, idempotency + CSRF | 200 точные новые bindings; CP атомарно меняет выбранные bindings/agent versions и фиксирует соответствующие platform events; текущие исполнения неизменны |

Bind body: `externalTeamRef`, `externalChannelRef`, `externalUserDigest`
(lowercase SHA256), `subjectRef`. Actor/organization/project поля запрещены.
Rebind body: `consumers` (1..100), каждый содержит `agentRef`, `agentVersion`,
`bindingRef`, `bindingVersion`, `versionRef`, `projectRef`. В body `versionRef`
означает **прежнюю** версию binding; целевая версия передаётся в path.
`projectRef` потребителя описывает снимок, не authority. Дубликаты agentRef
отклоняются до RPC. После конфликта клиент перечитывает impact; слепой retry
с новой idempotency key запрещён.

Пагинация: `pageSize` 1..100 (default 50), `pageToken` <=512; непрозрачный cursor
возвращается без преобразования. HTTP не вычисляет eligibility самостоятельно.
Неизвестный state, перепутанные refs/target, небезопасные JSON integers,
неполный rebind response закрыто дают 502. Ошибки CP проходят общий безопасный
Problem mapping без upstream detail. Изменения session/CSRF/readiness policy
для этих операций не требуются.

## Secret revision impact и rebind

Producer: `b3375dfa64e6f404df83ce7b05904a5143e2e6e3`.

- GET `/api/v1/runtime-secrets/{secretRef}/revisions/{revision}/impact`,
  SDK `getRuntimeSecretImpact` -> CP `GetRuntimeSecretImpact`.
  `revision` — точное положительное число, не implicit latest. Authority:
  `secret.rotate` и CP eligibility окружений/agents. Ответ `secretVersion`
  и ETag используются для последующей мутации; pagination 1..100/512.
  Read path не создаёт события. Строка без agent binding сохраняется:
  `consumer` отсутствует, environment/version/project/secretRevisions остаются.
- POST того же префикса `/consumer-bindings`, SDK `rebindRuntimeSecret` ->
  CP `RebindRuntimeSecret`. `If-Match` = secretVersion, CSRF и idempotency
  обязательны. Body `selections` содержит 1..32 уникальных environments с
  `environmentRef`, `expectedEnvironmentVersion`, `sourceVersionRef`,
  `consumers` (обязательный массив, может быть пустым). В сумме <=100 уникальных
  agents. Их старый `versionRef` должен совпадать с sourceVersionRef.
  CP проверяет secret/project/agent permissions, owner и OCC, атомарно
  публикует новые environment revisions с нужной secret revision, связывает
  только выбранных consumers и фиксирует platform events публикации/bindings.
  Старые immutable execution snapshots не меняются.
- HTTP возвращает `environments` — безопасные квитанции
  environmentRef/environmentVersion/projectRef/versionRef/digest — и
  `bindings`. Полная конфигурация читается существующим
  `getRuntimeEnvironmentSet`; конфигурационные values в ответ мутации не
  копируются. Возвращённые refs/versions/secret descriptors сверяются с
  selections и target до выдачи. Headers всегда no-store/no-cache.
- Runtime-каталог gateway содержит новые CP message IDs для identity,
  неизвестного delivery outcome, reconciliation и обоих selected rebind.

## Проверки HTTP

Из `services/external/control-api-gateway`:

```sh
env -u GOFLAGS GOENV=off GOWORK=off go test -race ./internal/transport/http ./internal/security/boundary ./internal/app -count=1
env -u GOFLAGS GOENV=off GOWORK=off go vet ./internal/transport/http ./internal/security/boundary ./internal/app
```

`TestIdentityEnvironment*` проверяет exact RPC/body/OCC/cursor, отрицательные
authority ответы, malformed input, session/CSRF/revocation, чужую organization,
upstream mismatch и отсутствие незапрошенных bindings. Это локальная fake
проверка HTTP boundary, не live CP integration или пользовательское E2E.
Go/TS генерируются через `make gen-control-api-gateway-openapi-go gen-openapi-ts`.

Полный `npm run typecheck` на текущем HTTP worktree: FAIL в handwritten
PWA Schedule и IntegrationDefinition fixtures/editor, которые ещё не содержат
обязательные поля принятых ранее контрактов. Это зависимость от PWA unit, не
основание ослаблять схемы или readiness. Сгенерированный SDK проверяется
отдельно; локальный fake test не заменяет этот общий результат.

## Сверка остального scope

| Критерий #1045 | HTTP mapping / локальное evidence |
|---|---|
| Managed lifecycle четырёх kinds | `managed_configuration_endpoints.go`, typed views, `TestManagedConfigurationRoutesCallExactTypedRPC`; 21 операция, source ownership остаётся CP |
| Глобальные каталоги и группировка | `organization_catalog_endpoints.go`, `managed_catalog_endpoints.go`, соответствующие route tests; optional project только filter, не authority |
| VFS/search | `vfs_endpoints.go`, bounded query/cursor; ownership и eligibility проверяет CP |
| Prompt/preview/materialization | `mvp_endpoints.go`: ListPromptTemplateVariables, ValidatePromptTemplate, PreviewPromptTemplate с targetKind/targetRef, template и includeFullMaterialization; preview не заменяет публикацию |
| Continuation | `command_endpoints.go`: LaunchRun передаёт sessionRef, AddSessionTurn — session/run/node/task/attachmentSetRef; origin CONTROL_CENTER назначает HTTP, lineage проверяет CP |
| Model/configuration | `runtime_configuration_endpoints.go`: PublishAgentRuntimeConfiguration и overlay draft/validate/publish; model/provider candidates передаются явно, не выбираются gateway |
| Provider lifecycle | `mvp_endpoints.go`: create, API key/device authorization, observe/refresh/reauthorize, enable/revoke/delete; secret key не возвращается и не логируется |
| STT bootstrap | `stt_availability.go` + tests: eligibility CP и authenticated protected check, stage READY + fresh validUntil <=31s, TTL30s; условие не ослаблялось |
| Environment draft | `environment_draft_endpoints.go` + tests: create/save/validate/publish/discard/get |
| Identity, environment/secret impact/rebind | Новые typed endpoints и `TestIdentityEnvironment*`, `TestSecretImpact*`, `TestSecretRebind*`, exact app authority method tests |

Текущий producer не содержит отдельного поля reasoning в
`PublishAgentRuntimeConfigurationRequest`: расширять HTTP вымышленным полем
нельзя. Настройки исполнения через существующий overlay проходят CP validation.
Новые профильные producer возможности требуют отдельного checkpoint.

## Незавершённые зависимости

- Полные STT parameters потреблены из `a88caf7f2` вместе с предшественниками
  и миграцией системных ролей `9911ddb38`. GET `system-stt-configuration`
  возвращает enabled, parameters и три точных limit поля. POST
  `system-stt-configurations/typed-drafts` принимает name, optional
  configurationRef и specification. Новый configuration не требует If-Match;
  новая revision существующего configuration требует его ETag. Параметры
  проверяет общий modelprofile; далее используется тот же CP draft/validate/
  publish lifecycle. Raw JSON/YAML/TOML редактор сохранён. Это не upload override
  и не подтверждение доступности provider. Focused HTTP race/SDK: PASS.
- HTTP SkillBundle/MemoryRecord потребляет `bd674280e`: все 24 typed
  operations реализованы и покрыты focused fake tests. Точные paths, OCC,
  redaction и ограничения — в `context-http-1045.md`. CP owner SQL для CRUD,
  history и bindings подключён; runtime materialization и scanner deployment
  ещё не готовы. Runtime configuration readback/command responses содержат
  оба массива exact bindings и agentVersion. Full acceptance не заявляется.
- Полный PR body и итоговая матрица criterion/evidence оформляются после
  полного HTTP scope. Live provider, staging и deploy не запускались.
