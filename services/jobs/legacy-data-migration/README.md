---
id: SVC-MC-016
title: Legacy data migration
type: service
status: approved
owner: developer
version: 1.3.0
updated: 2026-08-09
---

# Legacy data migration

`legacy-data-migration` — самостоятельно развёртываемая one-shot job для
[Issue #196](https://github.com/codex-k8s/matter-codex/issues/196). Она не
создаёт compatibility facade и не становится вторым источником истины. Job
строит из полного legacy snapshot закрытый typed request owner materializer,
поставленного [PR #249](https://github.com/codex-k8s/matter-codex/pull/249),
для всего применимого active
Project/Team/Chat/Agent/configuration/Session/Turn/Attempt/Process/Schedule/
delegation/callback graph. Затем она сохраняет immutable backup и после source
write fence вызывает единственный target authority по внутреннему gRPC. Job не
читает и не пишет target PostgreSQL, не выбирает target IDs и не содержит
generic JSON/DML dispatcher. Organization, project, owner actor, target IDs,
versions, derived fields, audit и provenance receipts назначает и повторно
проверяет control-plane owner. Неоднозначная либо отсутствующая source
authority закрыто блокирует план.

Исполнение любого mode запрещено до закрытия prerequisite
[Issue #241](https://github.com/codex-k8s/matter-codex/issues/241). Код всегда
требует для source/restore PostgreSQL URL с единственным `sslmode=verify-full`, exact DNS
hostname/SNI и отдельным абсолютным путём trusted CA. Plaintext,
`sslmode=disable|require`, IP endpoint, routing override, другой CA и TLS ниже
1.3 отклоняются до чтения business state. После подключения проверяется
фактически обслуживаемая сессия через `pg_stat_ssl`. Target transport использует
exact TLS hostname/CA, workload certificate, signed application grant и
deploy-owned authority policy из общего `controlplaneclient`.

Source credential поступает из отдельного Secret
`legacy-data-migration-source-postgresql-g1`, подготовленного code-first
контуром #241, а не из общего Vault CSI mount. Exact endpoint —
`mattermost-postgres-migration.matter-kodex-prod.svc.cluster.local`; его leaf
имеет единственный совпадающий SAN. LOGIN principal наследует только
`matter_codex_migration`: read-only доступ к закрытому source inventory и
минимальные receipt/snapshot/fence capabilities, необходимые уже утверждённому
протоколу #196. Readiness не вызывает эти функции и не читает source rows.

## Source и target inventory

Source adapter экспортирует sentinel и строки ровно 50 таблиц из единого
закрытого inventory. Catalog preflight требует точного равенства этому набору,
а `pg_dump` получает 50 отдельных `--table=public.<name>` без wildcard.
Новая, лишняя или отсутствующая `public.matter_codex_%` таблица блокирует план
до backup. Поэтому забытая legacy-таблица
не исчезает из backup и не получает молчаливую archive-классификацию.
Полный canonical payload потоково входит в source SHA и `pg_dump`. В памяти
planner удерживает только поля, необходимые typed materialization: ключи,
lifecycle/lineage, prompt/input/result/memory content и secret references без
secret values. Session archive bytes заменяются digest. Эти поля никогда не
попадают в safe report или runtime logs и освобождаются после operation.
Фактический inventory разделён так:

| Legacy source | Target / disposition |
| --- | --- |
| `projects`, `chats`, `agent_roles`, `mattermost_bot_identities`, `chat_participants`, accounts/credentials/prompts/runtime variables | Typed `PROJECT → TEAM → CHAT → ARTIFACT/CREDENTIAL_BINDING → ROLE_DEFINITION → INSTRUCTION_SET → PROVIDER_REFERENCE/POOL → ROLE_IMAGE_RECIPE/IMAGE_BUILD/IMAGE_ARTIFACT → AGENT → AGENT_ASSIGNMENT`; immutable source rows и deploy-owned CLEAN/admission evidence входят в owner plan, а target owner назначает IDs/versions/digests |
| `agent_sessions`, `agent_session_turns`, `agent_runs` | Typed `RUNTIME_REVISION → SESSION → TURN → TURN_ATTEMPT`; exact source IDs, sequence, immutable input, policy и configuration provenance замыкаются внутри одного Project. Orphan либо unsupported active row блокирует plan |
| `process_runs`, `process_turns`, `agent_delegations`, callback manifests/deliveries | Typed `PROCESS_RUN → DELEGATION_EDGE → CALLBACK_MANIFEST/CALLBACK_DELIVERY` с exact root actor, legacy policy digest отдельно от machine authority policy, parent/predecessor, launching Session/Turn/Attempt и callback tuple. Незавершённый callback/claim/owner attention блокирует cutover |
| `automation_schedules` | Каждый Schedule получает typed operation с exact cron/timezone/overlap/coalesce/misfire/retry, Agent/Room/Assignment/Instruction/Provider/RoleImage refs и lifecycle. Owner создаёт successor и audit; неоднозначность блокирует plan, а не отключает автоматизацию |
| configuration, repository, cluster-admin, policy и memory tables без отдельного active aggregate | Каждая строка получает явную owner disposition. Immutable configuration/provenance материализуется как CLEAN version-pinned Artifact и входит в RuntimeRevision components; terminal operational history архивируется. Nonterminal/unknown rows закрыто блокируются |

Target boundary — только четыре typed RPC из SVC-MC-015:
`PrepareLegacyGraphMigration`, `MaterializeLegacyGraphMigration`,
`GetLegacyGraphMigration(verifyCommitted=true)` и
`AbortLegacyGraphMigration`. Их полный method allowlist закреплён в workload
authority; произвольного operation kind, target ID или JSON payload нет.
`Prepare` проверяет closed 50-table dispositions, authority и immutable plan,
назначает deterministic owner-scoped IDs и immutable receipts. `Materialize`
под target locks одной owner transaction повторно разрешает tenant, actor,
references и eligibility, создаёт full graph/audit/provenance и выбирает один
terminal winner. `Get(...verifyCommitted=true)` перечитывает каждую
operation-specific target projection, protected history, runtime components,
audit и provenance evidence; missing/drift не позволяет source перейти в
`COMMITTED`.

## Source → target invariants

- Source plan допускает ровно один однозначный Project boundary. Target owner
  выводит `organization_id`, `project_id`, `owner_actor_id` из проверенного
  transport/authority context; source IDs становятся только immutable
  provenance и local refs внутри typed request.
- Вся цепочка имеет одинаковые `organization_id`, `project_id` и
  `owner_actor_id`. Скрытый owner mismatch не превращается в `not found`.
- `SESSION` фиксирует source session ID/key/binding version/SHA, exact Agent и
  Chat. `TURN` фиксирует session, source turn ID/run/version/SHA и текущую
  RuntimeRevision.
- Каждый Artifact, доступный active Turn, InstructionSet или RuntimeRevision,
  обязан быть `ACTIVE`, иметь положительную immutable version/size, exact
  SHA-256, `CLEAN` scan evidence с policy revision и version-pinned immutable
  storage reference. Missing, stale, ambiguous или
  недостижимая storage reference закрыто блокирует materialization/commit.
- Каждая перенесённая attempt имеет exact immutable input и собственную pinned
  RuntimeRevision; current attempt совпадает с Turn. Незавершённая lease/claim
  закрыто блокирует cutover. Prompt/result/input artifacts находятся в той же
  owner boundary.
- ProcessRun выводится только из source Turn relation, но target owner заново
  разрешает каждое ребро. Root initiator, legacy policy revision/digest и
  отдельный machine authority policy, immutable input, exact versions root
  Session/Turn/attempt/RuntimeRevision,
  каждый `parent_turn_id`, parent/launching Process, launching Turn/attempt,
  delegation и target Session/Turn/attempt обязаны совпасть. Cross-project
  source link, orphan parent, неверный predecessor/successor и broken runtime
  lineage блокируют commit.
- Unknown/unsupported state, duplicate source ID, orphan, ambiguous target,
  stale reference, tenant mismatch, unmaterialized active state и broken
  lineage закрыто останавливают Build до owner effect. Безопасный report
  считается готовым только при нулевом закрытом наборе bounded violation
  counters; `pre-commit` и `commit` повторно требуют тот же exact plan digest.
- Source foreign-key existence недостаточна: job отдельно сверяет project
  boundary у role/chat/session/process/policy/schedule/delegation/memory связей.
  Именованные OpenAI/GitHub/bot links и optional credential IDs проверяются
  только когда присутствуют; configured/authorized account без credential и
  dangling name дают `orphan_reference`. Cross-project relation учитывается
  как `tenant_mismatch`.
- Каждый target `Agent` обязан замыкать current protected configuration graph:
  exact RoleDefinition, опубликованный InstructionSet, ProviderPool и все его
  ProviderReference, RoleImageRecipe и единственный active AgentAssignment для
  enabled Agent. Ссылка проверяется по ID/version/digest из protected history;
  непустой legacy role prompt переносится в typed InstructionSet, но report
  содержит только SHA-256; configured legacy bot дополнительно обязан
  совпасть с exact Team/provider identity и durable receipt target Agent.
- Memory scope обязан соответствовать role boundary, каждая record имеет хотя
  бы одну version с валидным content hash, `supersedes` указывает только на
  меньшую version той же record, а embedding — на существующую version с
  непустой model revision и положительной dimension.
- Cluster-admin frozen subjects/bindings/dependencies разрешаются только через
  существующую source owner boundary либо точную immutable revocation; orphan,
  неизвестный revocation/dependency type и cross-project binding блокируют
  план. Automation audit также сверяется с project/schedule/run lineage.
- `sourceSha256` покрывает имя каждой таблицы и canonical JSON каждой строки;
  `ownerRequestSha256` — deterministic typed request, а `targetSha256` появляется
  только после owner `Prepare` и хранит его semantic digest;
  `mappingSha256` — source→target/archive relation с owner/version;
  `materializationSha256/count` — точный закрытый typed intent;
  `planSha256` — полный безопасный plan. Report содержит только эти digests и
  агрегированные counts, без identifiers, сообщений, actor names и секретов.

## Один entrypoint и lifecycle

Сценарная карта:

| Граница | Контракт |
| --- | --- |
| Источник требования | #196, epic #185, strategy reset #179, ADR-MC-015 и owner decision о prerequisite #241 |
| Actor / authority | Только owner-approved execution Job; source authority приходит из выделенного PostgreSQL principal, target — из workload mTLS, signed application grant, source root reference/digest и exact RPC allowlist. Env/payload IDs не дают target полномочий |
| Entrypoint | Один `/usr/local/bin/legacy-data-migration`; mode и plan ID обязательны и закрыто валидируются |
| Owner resolution | Source project/chat/role IDs никогда не принимаются как target owner: control-plane owner выводит organization/project/actor из authority context и повторно разрешает каждое typed reference |
| Locks / OCC | Source exported repeatable-read snapshot и commit lock закрытого inventory; target locks, deterministic IDs и immutable receipts принадлежат owner RPC. Plan/source/semantic/request SHA являются OCC fence |
| Idempotency / one-winner | Plan ID + immutable hashes; source advisory lock, owner idempotency receipt и единственный owner `COMMITTED` winner |
| State / audit | Source `PREPARED → FROZEN → COMMITTED`, owner plan `PREPARED → COMMITTED`; до owner cutover возможен `ABORTED`. Full graph, provenance, audit и operation receipts фиксируются одной owner transaction; любой повтор `COMMITTED` выполняет полный authoritative readback |
| Failure / retry / cancel | До target commit повторяет exact plan либо abort; после target commit только forward recovery. SIGTERM отменяет operation и join; по неизвестному outcome сначала читаются receipts |
| Readiness / deploy | Exact TLS readback source/restore DB и рабочий owner RPC path до operation; suspended Kubernetes Job, отдельный source PostgreSQL Secret, Vault CSI для остальных credentials, retained PVC, pod-private staging, exact NetworkPolicy, metrics/alerts; запуск принадлежит отдельному owner-approved execution PR |

| Mode | Effects | Повтор / terminal |
| --- | --- | --- |
| `dry-run` | Repeatable-read source snapshot, typed plan и безопасный report; source/target business state не меняется | Тот же immutable input и owner evidence дают тот же request/plan digests; нарушение завершает job ошибкой |
| `pre-commit` | Exported snapshot → encrypted `pg_dump` → HMAC/list verification → exclusive manifest → owner `Prepare` → source `PREPARED` | O_EXCL, owner idempotency и exact manifest делают повтор детерминированным; иной plan/source/semantic digest закрыто отклоняется |
| `commit` | Backup/manifest/source receipt, повторный locked plan, source `FROZEN`, owner `Materialize` + полный verified `Get`, затем source `COMMITTED` | Crash после `FROZEN` повторяет exact owner command; partial target materialization откатывается owner transaction. Target `COMMITTED` replay всегда перечитывает все operation receipts/projections/audit/provenance |
| `rollback` | Только до owner `COMMITTED`: owner `Abort`, затем source `ABORTED` и снятие fence | После irreversible owner cutover отклоняется. Для нового запуска после abort нужен новый plan ID |
| `restore-verify` | Аутентифицированный decrypt и `pg_restore --single-transaction` в exact empty isolated DB, затем повторный snapshot/count/SHA readback и durable source proof | Имя DB обязано соответствовать `mattercodex_restore_<12..32 hex>`; непустая DB всегда отклоняется, после crash её пересоздаёт controller |

`pre-commit` хранит immutable owner plan и source receipt. `commit` сначала
фиксирует source fence, затем owner materialization/receipt, затем source
receipt. Между source PostgreSQL и control-plane owner нет ложной
распределённой транзакции: при crash source остаётся закрытым, а повтор exact
plan безопасно доводит protocol. До owner commit разрешён `rollback`; после
него — только forward recovery/readback.

Worker начинает operation только после config, named-SQL, source TLS и owner RPC
readiness. SIGTERM отменяет operation; каждый `psql`/`pg_dump`/`pg_restore`
получает SIGTERM, bounded kill fallback и обязательный `wait`, а worker
фактически join-ится до закрытия DB/files. Durable receipt/report остаётся
authority; terminal metric удерживается 20 секунд при readiness=false для
очередного 15-секундного scrape. Labels закрыты, runtime diagnostics и
Prometheus HELP — на английском.

## Backup boundary

Backup создаётся до любого cutover effect в source-exported snapshot.
Exact 50-table `pg_dump --format=custom` шифруется потоком AES-256-CTR с независимым
HMAC-SHA-256 encrypt-then-MAC ключом, производным от 32-byte base64 secret;
nonce случаен, а аутентифицированный header содержит exact source SHA и digest
table counts для безопасного завершения sidecar после crash. Файл и manifest
создаются `O_EXCL`, mode `0600`, синхронизируются
вместе с directory entry на retained PVC и проверяются без загрузки dump в память. Manifest связывает
source SHA, backup SHA/size и table counts. Secret, DSN, CA, row payload и PII
не логируются и не входят в report. Каждый CLI получает закрытый env без
унаследованных `PG*` routing/options и без DSN-as-`PGDATABASE`: URL закрыто
раскладывается в exact `PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE`, CA, `verify-full`,
`PGSSLMINPROTOCOLVERSION=TLSv1.3` и `PGSSLMAXPROTOCOLVERSION=TLSv1.3`.
Перед consumer HMAC и file digest проверяются одним проходом по `O_NOFOLLOW`
regular inode (`0600`, uid, link count, size); plaintext публикуется только в
unlinked private staging inode с повторной проверкой `uid/mode/link-count=0` и
exact size. Rename/symlink/hardlink/truncate исходного PVC после проверки не
меняет bytes, читаемые `pg_restore`.
`TMPDIR` указывает на отдельный pod-private `emptyDir` с `sizeLimit`: staging
остаётся writable при `readOnlyRootFilesystem` и не попадает в retained PVC.
До decrypt/write код синхронно отклоняет ciphertext/plaintext выше
`LEGACY_DATA_MIGRATION_MAXIMUM_STAGING_BYTES` (1920 MiB); лимит ниже 2 GiB
volume capacity и оставляет запас для envelope, каталога и deleted-but-open
inode. Асинхронная eviction не используется как security boundary.

`pg_restore --list` доказывает читаемость архива при `pre-commit`; отдельный
`restore-verify` доказывает фактическое восстановление и exact source
SHA/counts, после чего ставит идемпотентный durable proof в source receipt.
`commit` без этого proof отклоняется. Production restore не является rollback
этого протокола.

## Deploy ownership

Docker image включает только бинарь и pinned PostgreSQL 18 `pg_dump/pg_restore`.
`deploy/k8s/base/legacy-data-migration` содержит suspended Job, отдельный
ServiceAccount, retained PVC, отдельный source PostgreSQL Secret, Vault CSI для
остальных credentials, source/internal RPC CA, exact NetworkPolicy,
ServiceMonitor и alerts. Manifest по умолчанию — `suspend: true`, placeholder
digest/plan и `dry-run`; он не является разрешением на запуск. Отдельный
owner-approved execution PR обязан закрепить digest, plan ID, один mode и для
`restore-verify` добавить отдельные restore credential/CA mounts.
После authoritative `COMMITTED` отдельная cleanup wave
[#271](https://github.com/codex-k8s/matter-codex/issues/271) закрывает client
ingress, переводит выделенный LOGIN в `NOLOGIN`, проверяет boolean termination,
доказывает zero sessions, отзывает membership и только затем выводит client
Secret, CA и source TLS resources. Job сама не расширяет scope до credential
retirement. #197 владеет dark deploy и не является владельцем этого cleanup.

Runbook: [legacy-data-migration](../../../docs/runbooks/legacy-data-migration.md).
