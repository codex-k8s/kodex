---
id: OPS-HTTP-1045
title: HTTP-контракты завершения MVP
type: operations
status: approved
owner: developer
version: 0.1.0
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

## Локальные проверки

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

## Незавершённые зависимости

- Secret revision impact/rebind потребляет отдельный CP checkpoint.
- Настоящий SkillBundle/MemoryRecord CRUD и полный STT parameters требуют
  producer checkpoint #1046; подмена tools/knowledge artifacts запрещена.
- Полный PR body и итоговая матрица criterion/evidence оформляются после
  полного HTTP scope. Live provider, staging и deploy не запускались.
