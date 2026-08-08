---
id: SVC-MC-015
title: Контракт owner-materializer исторического графа control-plane
type: service-contract
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-08
---

# Контракт owner-materializer исторического графа control-plane

Документ фиксирует принимающую сторону Issue
[#247](https://github.com/codex-k8s/matter-codex/issues/247) и prerequisite
для повторной проверки frozen PR
[#242](https://github.com/codex-k8s/matter-codex/pull/242). Producer в
`legacy-data-migration`, его bounded staging и удаление прежнего SQL dispatcher
остаются scope PR #242 после merge #247. Здесь нет compatibility facade,
универсального JSON/DML endpoint, прямого доступа Job к target PostgreSQL или
двойной записи.

## Сквозной authority contract

```text
owner-approved legacy-data-migration Job
  -> exact mTLS workload + LEGACY_DATA_MIGRATION_GRANT
  -> authority resolver: producer/purpose/workload/SPIFFE/audience/operation/full method
  -> short-lived verified authorization context with replay reservation
  -> generated ControlPlaneService client
  -> PrepareLegacyGraphMigration | MaterializeLegacyGraphMigration
     | GetLegacyGraphMigration | AbortLegacyGraphMigration
  -> authorization.Principal
  -> typed caster with unknown-field rejection
  -> legacy migration domain service
  -> LegacyGraphMigrationTransaction repository port
  -> named SQL with pgx.StrictNamedArgs
  -> one serializable owner transaction
  -> target graph + immutable plan/operation receipts + audit + required outbox
  -> version-pinned full-plan readback
```

Профиль `control-plane.legacy-data-migration` отделён от OIDC owner и других
workload. Он принимает только `LEGACY_DATA_MIGRATION_GRANT`, выданный точному
`legacy-data-migration` SPIFFE workload, и четыре full methods выше. mTLS не
заменяет прикладной grant. Actor и organization поступают только из подписанного
grant и verified authorization context. Профиль organization-scoped, потому что
target Project ещё не существует. Подписанный grant связывает target actor с
`source_root_reference` и `source_root_sha256`; RPC повторяет только эти source
provenance поля, а transport требует точного совпадения. Target actor ID,
organization ID, Project ID, owner, версии, transaction/audit/event timestamps,
lineage и event sequence никогда не принимаются из RPC. Исторические timestamps
допускаются только в закрытых typed source evidence и проверяются owner service.

Readiness использует тот же producer, grant verifier, authority policy и
`CheckReadiness` full method (`control.legacy-data-migration.readiness`). Публичный
ключ grant доставляется control-plane через отдельный Vault CSI object name;
значения credential и job-side delivery в этом unit отсутствуют.

## Границы плана

- Один план содержит ровно один Project и не более 2 000 typed operations,
  50 source dispositions, 20 000 ссылок и 8 MiB canonical protobuf bytes.
- `source_snapshot_sha256` является canonical JSON SHA-256 отсортированного по
  `sourceTable` JSON-массива `{sourceTable, disposition, rowCount,
  sourceSha256, terminalStateSha256?}` для всех 50 dispositions; для каждой
  `MATERIALIZE` table `row_count` обязан совпасть с
  числом уникальных `(source_table, source_ref)` proofs в typed operations.
- `plan_id`, idempotency key, local refs и source refs имеют строгие пределы;
  UUID, SHA-256 и UTC timestamps каноничны. Неизвестные Proto fields,
  `UNSPECIFIED`, дубликаты local refs, противоречивые duplicate source proofs,
  повторные зависимости и нетопологический порядок отклоняются до записи
  PREPARED.
- Local ref — только scoped имя внутри immutable plan. Target UUID назначается
  сервером при PREPARE, сохраняется в intent и переиспользуется при replay.
- Каждый внешний target resolver выполняется после owner/project lock. Hidden
  foreign row даёт закрытый отказ до business mutation.
- Canonical `authorityPolicySha256` всегда берётся из реально обслуживаемого
  control-plane policy snapshot. Legacy process/capability/relationship digest
  хранится только как `legacyPolicySha256` и не сравнивается с machine policy.

## Закрытый реестр операций и зависимости

Каждая операция — отдельная ветвь Proto `oneof`; `Struct`, `map`, raw JSON и
`kind + payload` отсутствуют.

| Порядок | Typed operation | Авторитетный resolver и создаваемое состояние |
| --- | --- | --- |
| A | `Project` | подписанные organization/root actor; server-owned Project/Workspace ID, owner, `ACTIVE`, version 1 |
| C | `Team` | Project; typed members только через server-mapped root actor local ref; Team version 1 |
| C | `Chat` | Project и optional Agent local ref; Room type closed; Chat version 1 |
| D | `Artifact` | immutable storage ref/version, digest, size/media type и CLEAN admission evidence; Artifact version 1 |
| D | `CredentialBinding` | immutable secret reference и masked provider metadata без credential value; server owner/version |
| D | `RepositoryWorkspace` | Project и immutable repository source revision/digest; no secret; version 1 |
| E | `RoleDefinition` | Project; RoleDefinition + первая `protected_resource_history` row атомарно |
| E | `InstructionSet` | exact Artifact; published immutable content/version + первая history row |
| E | `ProviderConnectionReference` | masked reference/evidence only + первая history row |
| E | `ProviderPool` | exact Provider refs/version/digest; immutable eligibility snapshot + history |
| E | `RoleImageRecipe` | actual `ROLE_IMAGE_RECIPE` resource shape and specialized validation; audit, без фиктивной protected history |
| E | `ImageBuild` | exact Recipe generation/spec, terminal `SUCCEEDED`, staging digest и provenance evidence |
| E | `ImageArtifact` | exact Build/Recipe, admission/SBOM/signature/promotion readback и promoted digest |
| F | `Agent` | RoleDefinition, InstructionSet, ProviderPool, RoleImageRecipe; Agent + первая history row |
| F | `AgentAssignment` | Agent, Project/Workspace, optional Chat; server root actor + первая history row |
| G | `Schedule` | Agent/assignment/config/RoleImageRecipe local refs; runtime profile URI выводится сервером; Schedule version 1, polling read path |
| H | `RuntimeRevision` | exact active components/artifacts and served machine policy digest; immutable version 1 |
| I | `Session` | Agent/pool/assignment/Chat; immutable source session provenance |
| J | `Turn` | Session, exact predecessor/parent Turn, RuntimeRevision, prompt Artifact |
| K | `TurnAttempt` | Turn + attempt number + immutable input + exact RuntimeRevision ID/version/digest |
| L | `ProcessRun` | root actor, Session/Turn/Attempt, parent ProcessRun, launching Turn/Attempt, separate policy domains |
| M | `DelegationEdge` | parent process/session/turn/attempt -> child Agent/session/turn/attempt; server ID/generation и exact target Session Agent |
| M | `CallbackManifest` | exact delegation, callback run, destination cardinality and immutable manifest digest |
| M | `CallbackDelivery` | manifest destination + terminal delivery receipt; no payload authority |
| N | `MemoryRecord` | Project/source version/content digest; eligibility does not broaden owner boundary |

`RuntimeExecution`, `WorkClaim`, `OwnerGate`, `ScheduleOccurrence`,
`ScheduledRun`, live continuation and other transient authority are never
recreated from a historical caller snapshot. Their source table disposition is
closed and typed: zero rows or terminal archive with digest is accepted;
nonterminal/nonempty authority is rejected at PREPARE. Retry later создаёт новую
attempt/event/grant обычным owner path, а не переносит старую lease/claim.

## Матрица 50 source tables

Ровно одна `LegacySourceDisposition` обязана присутствовать для каждой строки
таблицы. `MATERIALIZE` требует перечисленные typed operations и совпадение
aggregate count/digest; `ARCHIVE_TERMINAL` требует только terminal rows и
включает count/digest в plan summary; `REJECT_NONEMPTY` допускает только ноль.

| Source table | Disposition и typed owner path |
| --- | --- |
| `matter_codex_agent_delegation_callback_deliveries` | `MATERIALIZE` -> CallbackDelivery либо `ARCHIVE_TERMINAL` |
| `matter_codex_agent_delegation_callback_manifests` | `MATERIALIZE` -> CallbackManifest |
| `matter_codex_agent_delegations` | `MATERIALIZE` -> DelegationEdge |
| `matter_codex_agent_flows` | `ARCHIVE_TERMINAL`; active flow запрещён |
| `matter_codex_agent_profiles` | `MATERIALIZE` -> RoleDefinition/Agent |
| `matter_codex_agent_prompt_templates` | `MATERIALIZE` -> Artifact/InstructionSet |
| `matter_codex_agent_role_runtime_variables` | `MATERIALIZE` -> Artifact/Agent provenance |
| `matter_codex_agent_roles` | `MATERIALIZE` -> RoleDefinition/Agent/RoleImageRecipe |
| `matter_codex_agent_runs` | `MATERIALIZE` -> TurnAttempt/ProcessRun либо terminal archive |
| `matter_codex_agent_session_turns` | `MATERIALIZE` -> Turn/TurnAttempt |
| `matter_codex_agent_sessions` | `MATERIALIZE` -> Session |
| `matter_codex_audit_events` | `ARCHIVE_TERMINAL`; target audit генерируется owner transaction |
| `matter_codex_automation_audit_events` | `ARCHIVE_TERMINAL` |
| `matter_codex_automation_schedules` | `MATERIALIZE` -> Schedule |
| `matter_codex_chat_participants` | `MATERIALIZE` -> Team membership/root actor mapping |
| `matter_codex_chat_repositories` | `MATERIALIZE` -> RepositoryWorkspace/Artifact |
| `matter_codex_chats` | `MATERIALIZE` -> Chat |
| `matter_codex_cluster_admin_bindings` | `REJECT_NONEMPTY`; authority не переносится |
| `matter_codex_cluster_bot_bindings` | `MATERIALIZE` -> masked Agent provenance либо terminal archive |
| `matter_codex_cluster_delivery_fences` | `ARCHIVE_TERMINAL` |
| `matter_codex_cluster_dependencies` | `MATERIALIZE` -> immutable provenance digest |
| `matter_codex_cluster_prompt_templates` | `MATERIALIZE` -> Artifact/InstructionSet |
| `matter_codex_cluster_revocations` | `ARCHIVE_TERMINAL`; старые grants остаются revoked |
| `matter_codex_cluster_runtime_variable_bindings` | `MATERIALIZE` -> Artifact/Agent provenance |
| `matter_codex_cluster_session_bindings` | `MATERIALIZE` -> Session provenance |
| `matter_codex_cluster_subjects` | `MATERIALIZE` -> signed root Actor mapping only |
| `matter_codex_credentials` | `MATERIALIZE` -> CredentialBinding/ProviderConnectionReference masked metadata; values запрещены |
| `matter_codex_github_accounts` | `MATERIALIZE` -> ProviderConnectionReference/RepositoryWorkspace |
| `matter_codex_interaction_capabilities` | `MATERIALIZE` -> Team/Chat policy refs |
| `matter_codex_mattermost_bot_identities` | `MATERIALIZE` -> masked Agent provenance |
| `matter_codex_memory_embeddings` | `ARCHIVE_TERMINAL`; projection пересоздаётся из authoritative content |
| `matter_codex_memory_record_versions` | `MATERIALIZE` -> MemoryRecord source version/digest |
| `matter_codex_memory_records` | `MATERIALIZE` -> MemoryRecord |
| `matter_codex_openai_accounts` | `MATERIALIZE` -> ProviderConnectionReference masked metadata |
| `matter_codex_owner_attention_requests` | `ARCHIVE_TERMINAL`; live OwnerGate запрещён |
| `matter_codex_policy_revisions` | `MATERIALIZE` -> separate legacy policy provenance |
| `matter_codex_process_runs` | `MATERIALIZE` -> ProcessRun |
| `matter_codex_process_turns` | `MATERIALIZE` -> predecessor/launch/callback provenance |
| `matter_codex_project_repositories` | `MATERIALIZE` -> RepositoryWorkspace/Artifact |
| `matter_codex_project_runtime_variables` | `MATERIALIZE` -> Artifact/Agent provenance |
| `matter_codex_projects` | `MATERIALIZE` -> Project/Team |
| `matter_codex_repositories` | `MATERIALIZE` -> RepositoryWorkspace |
| `matter_codex_role_capabilities` | `MATERIALIZE` -> RoleDefinition + legacy policy provenance |
| `matter_codex_role_relationship_policies` | `MATERIALIZE` -> RoleDefinition + legacy policy provenance |
| `matter_codex_runtime_agent_binding_discoveries` | `ARCHIVE_TERMINAL`; discovery пересчитывается |
| `matter_codex_runtime_agent_binding_outbox` | `ARCHIVE_TERMINAL`; target outbox не копируется |
| `matter_codex_schedule_occurrences` | `ARCHIVE_TERMINAL`; nonterminal запрещён |
| `matter_codex_scheduled_runs` | `ARCHIVE_TERMINAL`; nonterminal запрещён |
| `matter_codex_thread_contexts` | `MATERIALIZE` -> Session/Turn provenance либо terminal archive |
| `matter_codex_work_claims` | `ARCHIVE_TERMINAL`; active claim запрещён |

## Operation invariant matrix

Все target IDs, version 1, owner/root actor, `created_at`, `updated_at`, audit ID
и event ID назначает сервер и сохраняет в operation intent/receipt. Для каждой
typed operation receipt содержит `plan_id`, ordinal, kind, input SHA-256,
target kind/ID/version/state/projection SHA-256, immutable intent provenance
SHA-256, фактический provenance evidence SHA-256, audit IDs и count, event
IDs/sequences/count. OCC scope —
`organization + source-root + plan id + idempotency key`; request hash —
canonical full typed plan. Same key/different hash даёт `Aborted`.

| Operations | Initial state | Audit | Event или protected read path | Terminal predicates |
| --- | --- | --- | --- | --- |
| Project, Team, Chat, CredentialBinding, RepositoryWorkspace, RuntimeRevision, Session, Turn | typed state (`ACTIVE`, для Turn допустим terminal source state) | ровно 1 | `control_plane.runtime_configuration_changed`, ровно 1, runtime-controller; тот же commit | exact resource/version/spec digest + audit + outbox envelope/sequence |
| Artifact | `ACTIVE`, CLEAN | ровно 1 | события нет; exact `GetResource(id,version)` | storage version/digest/size/media/evidence неизменны |
| RoleDefinition, InstructionSet, ProviderReference, ProviderPool, Agent, AgentAssignment | `ACTIVE`/published | ровно 1 | события нет; typed get/history version-pinned | resource + первая history snapshot/digest + audit |
| RoleImageRecipe | `ACTIVE` | ровно 1 | события и protected history нет; `GetRoleImageRecipe(id,version)` | actual recipe spec/policy/runtime-contract digest + audit |
| ImageBuild, ImageArtifact | `SUCCEEDED`/`ACTIVE` | ровно 1 | события нет; version-pinned specialized read path | exact recipe/build/admission/signature/promotion evidence + audit |
| Schedule | `ACTIVE`/`PAUSED`/terminal source state | ровно 1 | события нет; scheduler polling/read path | exact binding/version/digest + audit |
| TurnAttempt | source terminal либо `QUEUED` без lease; `CLAIMED/RUNNING` запрещены | ровно 1 | события нет; Turn/runtime read path | turn/attempt/input/runtime/state/outcome/time tuple + audit |
| ProcessRun | typed source state | ровно 1 | события нет; run detail/lineage | root/parent/launch tuple + policy domains + audit |
| Delegation/Callback/Memory | terminal or immutable typed state | ровно 1 | события нет; protected lineage/read path | exact edge/cardinality/receipt/content provenance + audit; callback delivery receipt нормализует `DELIVERED/FAILED/CANCELLED` в `SUCCEEDED/FAILED/CANCELLED` lifecycle state |

RuntimeRevision input содержит ожидаемые `authority_policy_revision` и
`authority_policy_sha256`, но materializer требует точного совпадения с
`Service.authorityPolicy*`, реально загруженными при startup. Legacy policy
revision/digest хранится отдельно в ProcessRun provenance и никогда не влияет
на этот predicate.

## Exact provenance matrix

| Узел | Обязательное доказательство |
| --- | --- |
| root Actor | signed grant subject + `source_root_reference/source_root_sha256`; target actor ID только из verified principal |
| RuntimeRevision | served machine policy revision/digest + exact component IDs/versions/projection digests |
| Turn | Session, source turn ref/digest/version, exact predecessor и optional parent Turn |
| TurnAttempt | Turn, attempt number, immutable input digest, RuntimeRevision ID/version/digest |
| ProcessRun | root Actor, унаследованные root Session/Turn/Attempt и policy у child process, parent ProcessRun, launching Turn/Attempt, отдельный current target runtime/input, immutable process input, machine/legacy policy domains |
| Delegation | parent process/session/turn/attempt -> child Agent/session/turn/attempt, server-owned ID/generation и exact Session Agent |
| Callback | exact delegation, callback run, closed destination set и каждая terminal delivery receipt |

Missing, ambiguous, stale, foreign, reordered либо digest-mismatched edge
блокирует PREPARED или COMMITTED. Непустое поле само по себе не является
доказательством.

## Lifecycle, replay и crash matrix

| Сценарий | Авторитетный результат |
| --- | --- |
| PREPARE new | валидирует весь typed plan, 50 dispositions, refs/order/limits; одной transaction сохраняет immutable PREPARED plan, source/semantic/full digests, assigned IDs и receipt skeletons |
| PREPARE exact replay | тот же scope/key/hash возвращает тот же PREPARED либо COMMITTED result |
| key/hash/plan collision или stale OCC | `Aborted`, эффектов нет |
| MATERIALIZE PREPARED | lock plan, Project fence и graph в deterministic order; повторная валидация; одна transaction создаёт весь graph, final receipts/audit/outbox, same-transaction readback и COMMITTED winner |
| ABORT PREPARED | row/fence single winner переводит план в ABORTED; business effects отсутствуют |
| ABORT COMMITTED / MATERIALIZE ABORTED | `FailedPrecondition` |
| cancel против commit | один `FOR UPDATE` fence; ровно один terminal winner |
| retryable DB/crash before commit | transaction rollback; durable PREPARED остаётся; exact retry продолжает |
| response lost after commit | Prepare/Materialize/Get exact replay читает immutable COMMITTED без второго эффекта |
| COMMITTED replay | read-only проверяет каждую operation, immutable payload/intent, source dispositions, target state/version/digest/history, полный снимок Attempt/delegation/callback и provenance, audit/outbox cardinality и full-plan count/digest |
| missing/drifted/foreign row | bounded `DRIFTED` result с predicates; source completion запрещён; generic repair отсутствует |
| unknown source/system state | typed rejection до PREPARED и до mutation |

## Error matrix

| Причина | gRPC code |
| --- | --- |
| malformed/unknown enum/field/oversize/duplicate | `InvalidArgument` |
| missing/invalid application credential | `Unauthenticated` |
| wrong workload/purpose/full method/permission/replay | `PermissionDenied` или `Unauthenticated` по общей границе |
| hidden foreign owner/reference | `NotFound` без утечки identifier |
| OCC/idempotency/request hash conflict | `Aborted` |
| dependency/lifecycle/provenance mismatch | `FailedPrecondition` |
| dependency outage или retry exhaustion | `Unavailable` |
| corrupt persisted receipt/target/audit/event | typed `DRIFTED` либо `DataLoss`, никогда success |
| unexpected | `Internal` |

## Проверка и rollback

Миграция schema forward-only: после появления immutable PREPARED/COMMITTED
receipts откат схемы запрещён. Rollback до merge — откат PR; после merge до
использования — не выдавать grant producer. После первого PREPARED применяется
только новая forward migration. Deploy, credential values, live migration,
backup/restore и source completion не входят в #247.
