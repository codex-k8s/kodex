---
id: ARCH-MC-006
title: Логическая модель данных web-first платформы
type: architecture
status: approved
owner: architect
version: 1.3.0
updated: 2026-08-29
---

# Логическая модель данных web-first платформы

Нормативная fresh schema находится в
`services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql`.
Документ показывает aggregates и ownership, но не заменяет SQL contract.

## Installation, Organization и доступ

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `installation` | stable installation identity и bootstrap revision |
| `organizations` | tenant boundary, slug, locale и lifecycle |
| `subjects` | server-resolved user/service identity |
| `owner_claim_contracts` | initial owner bootstrap contract без статического owner UUID |
| `permission_registry` | закрытый registry application permissions и допустимых scope/resource kind |
| `oidc_groups`, `oidc_group_memberships` | bounded token-derived group read model без policy authority |
| `application_roles`, `application_role_versions` | system/custom role и immutable version с permission set |
| `access_bindings` | user/group/service binding, pinned role version, scope, UTC conditions, OCC state |
| `memberships` | read-only SQL view старых endpoints, вычисляемый из canonical `access_bindings`; не policy authority и не отдельное хранилище |
| `projects` | единственный пользовательский контейнер, version/OCC и lifecycle |

Role version не изменяется после создания. Binding scope хранит server-resolved
UUID ресурса, но наружу возвращает только opaque ref. Effective access не
материализуется как доверенный кеш: решение строится из актуальных binding,
OIDC group membership и точного server-resolved target в одной snapshot.
Legacy membership-команды атомарно создают либо изменяют custom role version и
binding с server-owned presentation marker. Binding уровня `RESOURCE_KIND` и
`RESOURCE_INSTANCE` в широкую project membership projection не попадает.

## Agents, instructions и role images

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `platform_capabilities` | built-in closed capability catalog |
| `runtime_profiles` | provider/model/resource defaults без secret values |
| `role_definitions` | переиспользуемое назначение и role image policy |
| `agents` | Project Agent либо единственный system assistant по stable key |
| `instruction_versions` | immutable content/digest, draft/validated/published lifecycle |
| `role_image_recipes` | canonical source/build/toolchain spec и SHA-256 |
| `image_builds` | fenced build attempt и безопасный progress/verdict |
| `image_artifacts` | promoted image digest, runtime ABI, SBOM/provenance/signature receipts |

System assistant constraints запрещают delete/archive/disable и смену system
purpose. RuntimeRevision ссылается только на admitted promoted role image.

## Workflows, sessions и graph

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `workflows` | Project aggregate и current published version |
| `workflow_versions` | immutable coordinator/agents/input/result/gate specification |
| `workflow_input_snapshots` | exact finalized AttachmentSet и параметрический input, материализованные для конкретного invocation |
| `sessions` | Agent-owned durable FIFO context; каждый delegated child получает отдельную Session |
| `session_turns` | ordered tasks with source, attempt, finalized input AttachmentSet и lifecycle |
| `runs` | root/child execution, pinned WorkflowVersion, source, target, result, graph revision/sequence |
| `run_nodes` | root process, Agent, Human Gate или bounded external action |
| `run_edges` | delegation, callback, retry, continuation и waiting semantics |
| `run_events` | immutable ordered deltas в пределах root Run |
| `runtime_revisions` | exact immutable versions/digests/grants, finalized AttachmentSet refs и ArtifactRevision input для attempt |
| `runtime_leases` | workload/method/attempt/input/fence-bound claim lifecycle |
| `callback_receipts` | exactly-once child-to-parent continuation effect |

Root lineage, parent/child route и actor назначает control-plane. Payload и
external locator не доказывают происхождение. Exactly-once callback создаёт
completed Turn в родительской Session, а после всех ожидаемых результатов —
новый coordinator continuation Turn и `CONTINUES` edge.

## Gates, attachments, artifacts и schedules

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `owner_gates` | server-owned recipient policy, safe context, version и one-winner resolution |
| `owner_gate_messages` | сообщения и решения точного Gate; пользовательское вложение связывается через finalized AttachmentSet |
| `artifacts` | стабильный organization/project aggregate, current lifecycle, delete/restore/purge state и OCC version |
| `artifact_revisions` | immutable server-numbered source/provenance/display metadata/media type/size/SHA-256/scan state |
| `artifact_uploads` | resumable upload reservation, idempotency scope, quota snapshot, ожидаемый размер/digest и terminal state |
| `artifact_upload_parts` | bounded chunk ordinal/offset/size/digest receipt; иной payload того же ordinal является conflict |
| `artifact_content` | exact S3 bucket/key/version_id/ETag/checksum/size receipt конкретной ArtifactRevision; тело в PostgreSQL отсутствует |
| `attachment_sets` | immutable envelope одного действия: organization/project/actor/source/purpose/finalization/manifest digest |
| `attachment_set_items` | stable ordered refs finalized AttachmentSet на exact ArtifactRevision с display name и purpose |
| `attachment_bindings` | server-owned binding finalized AttachmentSet ровно к одному assistant message, Session Turn, Run input, Workflow input или owner Gate message |
| `artifact_bindings` | direct semantic output/result/knowledge/avatar relation exact ArtifactRevision к Run/node/turn/attempt либо другому закрытому owner kind; как input не используется |
| `artifact_purge_tombstones` | минимальный audit receipt необратимого удаления без filename, content metadata, S3 locator и secret values |
| `schedules` | Agent/Workflow target, server-normalized preset, timezone, next due, input/session/notification policy |
| `schedule_occurrences` | immutable due time, schedule version, target/input snapshot и digest, attempt/fence и materialized Run |

`attachment_bindings` не хранит универсальную пару, которой доверяет
application. Физическая схема содержит nullable foreign keys на допустимые
авторитетные targets и constraint ровно одного target. Binding создаёт только
owner command после разрешения actor, Organization, Project и target. Для
initial Run, continuation, delegated child, Workflow invocation и Human Gate
input одна transaction фиксирует command/message/Turn, finalized AttachmentSet
binding, audit, idempotency receipt и обязательный outbox event.

Прямой `artifact_bindings` применим к сгенерированному result и другим
семантическим выходам. Чтобы использовать такой result как новый input,
control-plane повторно проверяет eligibility и создаёт новый immutable
`AttachmentSet`; mutable переиспользование output binding запрещено.

## Integrations

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `integration_definitions` | built-in/catalog definition и typed capability schema |
| `integration_connections` | metadata, masked credential state и lifecycle |
| `integration_connection_tests` | asynchronous typed readiness test receipt |
| `integration_grants` | Agent/Workflow capability grant and policy revision |
| `integration_invocations` | fenced typed effect, gate relation и safe result/error |

Secret material не хранится в этих таблицах и не возвращается frontend.

## System assistant

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `assistant_runtime` | desired/observed warm revision, heartbeat и readiness |
| `assistant_conversations` | durable system Session presentation per User/Project context |
| `assistant_messages` | immutable ordered user/assistant/platform messages; входные файлы связаны finalized AttachmentSet |
| `assistant_plans` | safe typed configuration preview и apply receipt |

Каждая assistant operation сохраняет initiator User и assistant attribution.

## Сквозные таблицы

`idempotency_receipts` связывает organization, actor, operation, key и intent
digest. Один key с тем же intent возвращает receipt, а с другим — conflict.
`audit_events` хранит actor/assistant attribution и safe before/after metadata.
После purge `artifact_purge_tombstones` и audit содержат только opaque refs,
scope, actor, timestamps, reason category, число revisions и digest deletion
receipt. Filename, media type, content digest, S3 locator, prompt fragment и
body удаляются.
`outbox_events` публикует обязательные domain events после commit.
`worker_grant_high_watermarks` обеспечивает durable replay/rollback protection.

## Инварианты

- все owner data содержат `organization_id`; Project-scoped data разрешается
  через server-owned relation;
- version/OCC проверяется после owner resolution;
- published Instruction/Workflow и terminal attempt immutable;
- finalized `AttachmentSet`, его порядок items и exact ArtifactRevision refs
  immutable; draft set не может быть связан с message/Turn/Run/Workflow/Gate;
- каждая не purged `ArtifactRevision` immutable и имеет ровно один exact
  content receipt; после подтверждённого purge content metadata заменяется
  минимальным tombstone, а не пустым или новым receipt;
- число files в AttachmentSet не ограничивается продуктовым contract, но upload
  materialize-ится bounded batches/chunks с installation quotas по bytes и
  storage; transport batch size не становится `max_files_per_*`;
- только `CLEAN` active ArtifactRevision включается в новый AttachmentSet,
  download grant или RuntimeRevision;
- `RuntimeRevision` pin-ит exact AttachmentSet refs, manifest digests и
  ArtifactRevision refs; materialized input directory read-only и не меняется
  после старта attempt;
- soft delete переводит Artifact в `DELETED`, задаёт `purge_after` через 30
  дней и закрывает новые bindings/grants/materializations, но не меняет уже
  работающий immutable workspace;
- restore до purge влияет только на будущие owner commands и RuntimeRevision;
  terminal и активные snapshots задним числом не переписываются;
- purge удаляет каждую exact S3 version и становится `PURGED` только после
  deletion readback; ошибка сохраняет retryable `PURGE_FAILED`, а не успешный
  статус;
- continuation с новым AttachmentSet всегда имеет platform-owned typed notice с
  count, read-only directory и manifest path независимо от пользовательского
  instruction template;
- prompt file descriptors происходят только из текущей RuntimeRevision,
  содержат safe metadata/local path и никогда не содержат S3 locator,
  credentials или secret values;
- event sequence монотонен в пределах root Run;
- retry создаёт новую attempt/revision/lease и `RETRY_OF` edge;
- Human Gate и callback имеют одного доменного winner;
- external IDs и display values не являются authority;
- legacy authority tables, backfill и dual read/write отсутствуют; допустима
  только read-only compatibility view, вычисляемая из canonical RBAC state.
