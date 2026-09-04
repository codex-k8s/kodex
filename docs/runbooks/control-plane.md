---
id: RUN-MC-007
title: Диагностика control-plane
type: runbook
status: approved
owner: sre
version: 2.2.1
updated: 2026-09-04
---

# Диагностика control-plane

## Локальные probes

`/healthz` подтверждает жизнь процесса. `/readyz` читает локальный snapshot
PostgreSQL, NATS publisher/outbox и workload-local authority. Gateway, runtime,
integration и interaction services не вызываются из probe.

Если сосед недоступен, его рабочий RPC получает `Unavailable`; control-plane не
объявляется неготовым из-за отсутствия optional или downstream consumer.

## Fresh schema

Fresh установка применяет baseline
`services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql`
и следующие versioned forward-only migrations по номеру. Контекстные планы и
Run activity добавляет
`services/internal/control-plane/cmd/cli/migrations/20260828099700_contextual_plans_and_run_activity.sql`.
Managed revisions, custom cron, materialized prompt snapshots и system STT
configuration добавляет
`services/internal/control-plane/cmd/cli/migrations/20260904000100_issue_1019_control_plane.sql`.
Exact scope IntegrationDefinition и durable avatar reservation/cleanup добавляет
`services/internal/control-plane/cmd/cli/migrations/20260904000200_issue_1019_remediation.sql`.

Migration Job вызывает `control-plane-cli up` и читает только
`CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE`. `control-plane-cli status` выполняет
readback. Legacy source DSN, ручной backfill/cutover и schema down в рабочей
среде не используются.

## Bootstrap

Проверить через owner API/audit, не прямым изменением SQL:

- одну Organization и initial owner claim contract;
- один Agent со stable key `system-assistant`;
- protected core prompt и owner supplement;
- built-in capabilities, integration definitions и runtime defaults;
- desired/observed warm assistant revision;
- идемпотентный повтор bootstrap без duplicate.

## Run/event path

Для incident зафиксировать безопасные opaque refs и correlation ID. Проверить:

1. state/attempt/version из authoritative Run detail;
2. graph snapshot sequence;
3. ordered `RunEvent` без gap или conflicting duplicate;
4. outbox publication receipt и NATS subject;
5. runtime claim/fence и terminal/cancel readback;
6. audit/idempotency receipt без raw payload.

Human Gate разрешается специализированной командой с OCC. Повтор exact intent
возвращает receipt, stale version — conflict/winner readback и не создаёт вторую
continuation.

## Prompt materialization и effective authority

При расхождении preview и фактического Run проверить безопасные metadata:

1. target kind: `AGENT`, `WORKFLOW_STAGE`, `AUTOMATION` или
   `SESSION_CONTINUATION`;
2. template ref/digest и materialization digest в preview и `RuntimeRevision`;
3. exact Agent, Workflow, schedule, Session/Turn и environment refs/revisions;
4. закрытое пересечение user, Agent, Workflow, eligible Connection и Human
   Gate capabilities; при наличии gate проверить server-owned layer exact
   node/turn/attempt, причём пустой layer обязан дать пустое пересечение;
5. наличие явных service blocks, включая неиспользованные slots;
6. audit, idempotency receipt и отсутствие нового Run при validation failure.

Prompt content, contextual values и полный materialized prompt в incident log
не выводить. Для `RUN` проверить exact requested run ref, а не latest revision
root-графа. `includeFull` требует одновременно `prompt.full.view` и credential
`auth_time` не старше пяти минут из проверенного ACR/AMR context. Для сравнения
использовать только opaque refs, versions и digests.

## Runtime workspace policy

В каждом новом `RuntimeRevision` проверить `workspace_policy.revision`, root,
quota и digest. Для revision `1` сервер публикует longest-prefix матрицу:

- `/workspace/input` — `READ_ONLY`;
- `/workspace/knowledge` — `READ_ONLY`;
- `/workspace` — `WRITABLE`;
- не более 1 GiB writable content и 10 000 файлов.

Browser или runner payload не может изменить paths, access либо quota. До
filesystem effect consumer обязан проверить policy digest и выбрать самое
длинное совпавшее правило; отсутствие совпадения означает
`PATH_OUTSIDE_WORKSPACE`. Отказ возвращает только один machine-safe reason:
`READ_ONLY`, `QUOTA_EXCEEDED`, `PATH_OUTSIDE_WORKSPACE` или
`RUNTIME_IO_ERROR`. File body, fragment и secret material в audit/provenance
не записываются.

Control-plane подтверждает producer-side snapshot в PostgreSQL и claim RPC.
Фактическое применение mount/path/quota и readback denial принадлежит внешним
units Issues #1025 (`runtime-controller`) и #1026 (`agent-runner`); до их
отдельного обновления этот consumer path отмечать как `NOT RUN`, а не как
readiness control-plane.

## Managed configuration

Для PromptTemplate, RoleImage, IntegrationDefinition и system STT проверить:

1. set version, `managed_by`, source и source revision;
2. единственный mutable draft и переход `DRAFT -> VALID/INVALID -> PUBLISHED`;
3. immutable published content/digest и полную history;
4. impact digest непосредственно перед selective rebind;
5. exact consumer kind/ref/revision/version после rebind;
6. readiness descriptor фактического consumer path.

До selective rebind прежняя binding должна продолжать читать immutable
`SUPERSEDED` revision. После rebind проверить специализированный consumer path:
`GetRuntimeEnvironmentRoleImageConfiguration`,
`GetIntegrationConnectionDefinitionConfiguration`, effective prompt либо
`GetSystemSTTConfiguration`. Несколько active bindings одного configuration
kind на один consumer являются дефектом. Managed lifecycle не имеет отдельного
domain event; восстановление выполняется через exact binding snapshot.

`IntegrationDefinition` всегда organization-wide. Rebind к
`IntegrationConnection` должен пройти `organization.manage` над definition
scope и отдельный `integration.manage` над exact server-resolved Connection;
`project.manage` этого не заменяет. История `PROMPT_TEMPLATE` возвращает
`content` только при `prompt.full.view`, иначе сервер редактирует DTO до выдачи.

Git-owned set нельзя редактировать через UI. Использовать только явный
`detach` или `copy`; прямое изменение SQL, published revision или binding
запрещено. Для environment и runtime secret применять их специализированные
version/revision lifecycle, а не generic managed configuration.

## Search, VFS и cursor

Для пропуска, повтора или межтенантного результата зафиксировать query,
optional project ref, page size и opaque token без декодирования в журнале.
Проверить `total`, immutable ordering `(relevance, created_at, kind, ref)` и
canonical effective access к каждому result. Изменение `updated_at` между
страницами не должно создавать gap или duplicate. Legacy project `VIEW` не
разрешает metadata Agent/Workflow/Run/Artifact без отдельной canonical binding.
Token связан
с исходным фильтром: его повтор как следующего token или применение с другим
query/project/path должно завершаться закрытым отказом.

VFS возвращает только metadata. Появление content, secret value или узла
недоступного Project является security incident. Исправлять cursor или VFS
projection прямым SQL запрещено.

Input node допустим только через exact `Run/Turn -> AttachmentSet -> frozen
item`; result node — только через producer `run_id` и source
`AGENT_RESULT/INTEGRATION_RESULT`. Если output позже приложен к другому Run, он
становится input того Run, но остаётся result исходного producer Run.

## Models, provider accounts и system STT

Model считается доступной только при enabled provider definition и хотя бы
одном подходящем `AUTHORIZED` account с текущей credential revision. Проверить
точные `readinessBlockers`, provider definition key и eligible account refs.

Device flow использует отдельные verify и reauthorize operations. Удаление
разрешено только account, чьей последней успешной authorization method была
`API_KEY`: результат — terminal `REVOKED` tombstone и durable credential
cleanup, а не удаление audit/state rows. Для system STT дополнительно проверить
published configuration revision, permission `platform.stt.use`, model и
provider eligibility; credential material через этот read path не выдаётся.
Поле `safe_status_reason` должно содержать только закрытый lifecycle/failure
code; произвольный provider text через него не выдаётся. Reauthorize до запуска
нового device flow обязан отвязать прежнюю credential revision и поставить её
durable cleanup. Если verify получил materialized credential, а owner
transaction/OCC не commit, проверить `ProviderMaterializationReferenced` по
account/authorization/exact descriptor и удалить только непривязанный material.
Исполнение STT и credential access относятся к Issue #1020.

## Avatar reservation и компенсация

Owner UI должен использовать `UploadAgentAvatar`, а не раздельные
`UploadArtifact` и `SetAgentAvatar`. Для incident проверить reservation ref,
state и безопасный object descriptor без content:

1. `RESERVED` создан до S3 `Put` с server-owned key и expiry;
2. `MATERIALIZED` содержит exact version/ETag/digest;
3. `FINALIZED` имеет один active Artifact, content reference, Agent binding,
   audit, event и idempotency receipt;
4. OCC/commit failure переводит запись в `COMPENSATING`, только если exact
   `artifact_content` reference отсутствует;
5. успешный exact delete завершает `COMPENSATED`, а сбой остаётся доступен для
   fenced expiry retry.

Нельзя вручную удалять object или reservation. Cleanup worker принадлежит
control-plane lifecycle и стартует вместе с остальными workers после startup
barrier.

## System assistant title и plans

Для жалобы на неверный title или план проверить через owner API и audit:

1. `titleSource` и `titleRevision`; `AGENT_PROPOSED` не должен заменять
   `USER_EDITED`;
2. contextual descriptor и server-resolved entity version/allowed operations;
3. plan `version`, `revision`, `validatedRevision`, state и content digest;
4. immutable revision readback до edit и после него;
5. apply/reject receipt, operation audit refs и отсутствие частичных ресурсов
   при `STALE`/`CONFLICT`;
6. idempotency receipt для exact retry и отдельный отказ при intent mismatch.

Не переводить plan state и не исправлять title прямым SQL. Для воспроизведения
schema и plan lifecycle использовать disposable проверку:

```bash
make test-control-plane-postgres
```

## Safe Run activity

В graph snapshot до первого delegate должны присутствовать `PLANNED` workflow
nodes. После materialization тот же node ref получает `QUEUED`; duplicate node
или duplicate `DELEGATED_TO` edge является дефектом.

Для `TOOL_CALL_RECORDED` проверить actor, message kind, tool, safe parameters,
capability/grant ref, state, duration, safe result и audit ref. Отсутствующий
audit ref, неизвестный capability/grant или raw payload требует закрытого
отказа и расследования; вручную дописывать event/outbox запрещено.

## Запрещено

- исправлять lifecycle прямым SQL;
- принимать actor/project/root lineage из request payload;
- вручную переоткрывать terminal Run или published version;
- удалять outbox/event row для разблокировки;
- выводить prompt, artifact content, provider response или secret material.
