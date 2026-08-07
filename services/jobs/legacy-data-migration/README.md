---
id: SVC-MC-016
title: Legacy data migration
type: service
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-07
---

# Legacy data migration

`legacy-data-migration` — самостоятельно развёртываемая one-shot job для
[Issue #196](https://github.com/codex-k8s/matter-codex/issues/196). Она не
создаёт compatibility facade и не становится вторым источником истины. После
материализации target graph зависимостями #238/#239 job сверяет полный legacy
snapshot, сохраняет его immutable backup и атомарно фиксирует cutover receipts
с source write fence.

Исполнение любого mode запрещено до закрытия prerequisite
[Issue #241](https://github.com/codex-k8s/matter-codex/issues/241). Код всегда
требует PostgreSQL URL с единственным `sslmode=verify-full`, exact DNS
hostname/SNI и отдельным абсолютным путём trusted CA. Plaintext,
`sslmode=disable|require`, IP endpoint, routing override, другой CA и TLS ниже
1.3 отклоняются до чтения business state. После подключения проверяется
фактически обслуживаемая сессия через `pg_stat_ssl`.

## Source и target inventory

Source adapter динамически экспортирует sentinel и строки каждой существующей
`public.matter_codex_%` таблицы. Planner при этом содержит закрытый inventory
всех таблиц свежего `main`: новая или неизвестная таблица блокирует план как
`unsupported_state`, а не только меняет digest. Поэтому забытая legacy-таблица
не исчезает из backup и не получает молчаливую archive-классификацию.
Полный canonical payload потоково входит в source SHA и `pg_dump`, но в памяти
planner остаётся только минимальная безопасная проекция ключей, lifecycle и
lineage; messages, archive bytes, credentials и memory content отбрасываются
сразу после hash. Фактический inventory разделён так:

| Legacy source | Target / disposition |
| --- | --- |
| `projects`, `chats`, `agent_roles`, `mattermost_bot_identities`, `chat_participants`, `chat_repositories`, `project_repositories`, repositories/accounts/profiles/prompts/flows/runtime variables | Сверяются с server-owned `PROJECT → TEAM → CHAT → AGENT → RoleDefinition/InstructionSet/ProviderPool/ProviderReference/RoleImageRecipe/AgentAssignment`; каждая version/digest подтверждается protected history, configured bot — exact provider identity/Team receipt; точные секретные/config payload остаются только в encrypted archive, target configuration остаётся единственным authoritative state |
| `agent_sessions`, `agent_session_turns` | Активные строки обязаны иметь exact `SESSION → TURN` binding по source ID/key/run/version/SHA; закрытая unmapped history допускается только как archive relation |
| `runtime_agent_binding_outbox`, `runtime_agent_binding_discoveries` | `DELIVERED` receipt сверяется с target binding; незавершённая active discovery, stale digest/version или duplicate блокируют план |
| `process_runs`, `process_turns`, `work_claims`, `owner_attention_requests`, `agent_delegations` и callback manifests | Активный `PROCESS_RUN` связывается через его Turns; parent/process/project lineage обязателен. Делегация считается закрытой только с terminal callback Turn, одним manifest и двумя delivered destinations; незавершённые claim/owner attention/callback блокируют cutover |
| `agent_runs`, turn artifacts, thread contexts, session archive | Каждый незавершённый `agent_run` обязан быть связан с materialized Turn; standalone active run и `pending/configured` thread context блокируют cutover. Target `ARTIFACT`, current `TURN_ATTEMPT`, `RUNTIME_REVISION` и `RUNTIME_EXECUTION` проверяются по scope/version/digest |
| memory records/versions/embeddings, automation schedules/occurrences/runs/audit, policy/capability/relationship, cluster-admin security state, credentials и audit | Полностью входят в encrypted immutable backup с counts/digest; разорванная memory version/supersedes/embedding цепочка, неизвестное состояние, outstanding interaction capability, enabled Schedule, nonterminal occurrence/run и незакрытый thread context блокируют cutover; значения не попадают в report и не копируются в target как второй authoritative state |

Target inventory читается из `control_plane.resources`, immutable
`protected_resource_history`, versioned `runtime_derived_resources`,
`turn_attempts` и `runtime_executions` через отдельную
`control_plane_migration` policy. Так RuntimeRevision components проверяются
по exact historical version и owner; для protected history дополнительно
сверяется сохранённый projection digest, а не случайный current resource. Job
не имеет DML к target business tables. Target receipt хранится
отдельно в `control_plane.legacy_data_cutovers`.

## Source → target invariants

- `PROJECT` выбирается единственно по `slug`; `TEAM` — по exact
  `mattermost_team_id/externalTeamRef`; `CHAT` — по channel ref и stable key;
  `AGENT` — по role name/stable key.
- Вся цепочка имеет одинаковые `organization_id`, `project_id` и
  `owner_actor_id`. Скрытый owner mismatch не превращается в `not found`.
- `SESSION` фиксирует source session ID/key/binding version/SHA, exact Agent и
  Chat. `TURN` фиксирует session, source turn ID/run/version/SHA и текущую
  RuntimeRevision.
- Каждая attempt имеет exact immutable input и собственную pinned
  RuntimeRevision; current attempt совпадает с Turn. Active legacy Turn имеет
  ровно одну current RuntimeExecution. Prompt/result/input artifacts находятся
  в той же owner boundary.
- ProcessRun выводится только из server-owned Turn relation. Cross-project
  source link, orphan parent, несколько target process IDs и broken runtime
  lineage блокируют commit.
- Unknown/unsupported state, duplicate source ID, orphan, ambiguous target,
  stale reference, tenant mismatch, unmaterialized active state и broken
  lineage дают ненулевой bounded violation counter. Любой counter блокирует
  `pre-commit` и `commit`.
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
  непустой legacy role prompt сравнивается с InstructionSet только по SHA-256,
  без удержания content в planner; configured legacy bot дополнительно обязан
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
  `targetSha256` — только exact matched target graph;
  `mappingSha256` — source→target/archive relation с owner/version;
  `planSha256` — полный безопасный plan. Report содержит только эти digests и
  агрегированные counts, без identifiers, сообщений, actor names и секретов.

## Один entrypoint и lifecycle

Сценарная карта:

| Граница | Контракт |
| --- | --- |
| Источник требования | #196, epic #185, strategy reset #179, ADR-MC-015 и owner decision о prerequisite #241 |
| Actor / authority | Только owner-approved SRE execution Job; authority приходит из Kubernetes ServiceAccount → Vault role → выделенные PostgreSQL LOGIN, которые являются members фиксированных NOLOGIN migration roles. Env/payload IDs не дают полномочий |
| Entrypoint | Один `/usr/local/bin/legacy-data-migration`; mode и plan ID обязательны и закрыто валидируются |
| Owner resolution | Source project/chat/role IDs никогда не принимаются как target owner: organization/project/owner выводятся из единственного matched `PROJECT`, затем проверяются на каждом target edge |
| Locks / OCC | Source exported repeatable-read snapshot; commit берёт все legacy tables `SHARE`, target resources/attempts/executions `SHARE`; immutable receipt и plan/source/target SHA являются OCC fence |
| Idempotency / one-winner | Plan ID + шесть immutable hashes; per-DB advisory transaction lock, unique source `FROZEN/COMMITTED` и target `COMMITTED` winner |
| State / audit | `PREPARED → FROZEN → COMMITTED` source и `PREPARED → PREPARED + restore_verified_at → COMMITTED` target; до cutover возможен `ABORTED`. Отдельное domain event неприменимо: job не меняет target business aggregate; authoritative result — exact receipts, immutable manifest и safe report |
| Failure / retry / cancel | До target commit повторяет exact plan либо abort; после target commit только forward recovery. SIGTERM отменяет operation и join; по неизвестному outcome сначала читаются receipts |
| Readiness / deploy | Exact TLS readback обеих БД и named SQL до operation; suspended Kubernetes Job, Vault CSI, retained PVC, exact NetworkPolicy, metrics/alerts; запуск принадлежит отдельному owner-approved execution PR |

| Mode | Effects | Повтор / terminal |
| --- | --- | --- |
| `dry-run` | Repeatable-read source snapshot, target readback, безопасный report; source/target business state не меняется | Тот же immutable input даёт тот же plan/digests; violation завершает job ошибкой |
| `pre-commit` | Exported snapshot → encrypted `pg_dump` → HMAC/list verification → exclusive manifest → `PREPARED` receipts | O_EXCL и exact manifest делают повтор идемпотентным; crash после fsync dump до manifest восстанавливает sidecar только после HMAC/list и exact source/count readback; иной drift закрыто отклоняется |
| `commit` | Повторная HMAC/manifest/receipt проверка backup, source/target locks, повтор plan, source `FROZEN` fence, target `COMMITTED`, source `COMMITTED` | Unique winner; crash между БД безопасно продолжается тем же mode/plan. Target business tables не получают частичных записей |
| `rollback` | Только до target `COMMITTED`: target/source receipts переходят в `ABORTED`, source `FROZEN` снимается | После irreversible cutover отклоняется. Для нового запуска после abort нужен новый plan ID |
| `restore-verify` | Аутентифицированный decrypt и `pg_restore --single-transaction` в exact empty isolated DB, затем повторный snapshot/count/SHA readback и durable target proof | Имя DB обязано соответствовать `mattercodex_restore_<12..32 hex>`; непустая DB всегда отклоняется, после crash её пересоздаёт controller |

`pre-commit` хранит durable intent в обеих БД. `commit` сначала фиксирует
source fence, затем target receipt, затем source receipt. Между PostgreSQL
owners нет ложной распределённой транзакции: при crash source остаётся
закрытым, а повтор exact plan безопасно доводит protocol. До target commit
разрешён `rollback`; после него — только forward recovery/readback.

Worker начинает operation только после config, named-SQL, TLS и обеих DB
readiness. SIGTERM отменяет operation, ожидает goroutine, затем отдельным
bounded context останавливает technical HTTP server и только потом закрывает
DB pools. Metrics имеют закрытые `mode/outcome` labels; runtime diagnostics и
Prometheus HELP — на английском.

## Backup boundary

Backup создаётся до любого cutover effect в source-exported snapshot.
`pg_dump --format=custom` шифруется потоком AES-256-CTR с независимым
HMAC-SHA-256 encrypt-then-MAC ключом, производным от 32-byte base64 secret;
nonce случаен, а аутентифицированный header содержит exact source SHA и digest
table counts для безопасного завершения sidecar после crash. Файл и manifest
создаются `O_EXCL`, mode `0600`, синхронизируются
вместе с directory entry на retained PVC и проверяются без загрузки dump в память. Manifest связывает
source SHA, backup SHA/size и table counts. Secret, DSN, CA, row payload и PII
не логируются и не входят в report.

`pg_restore --list` доказывает читаемость архива при `pre-commit`; отдельный
`restore-verify` доказывает фактическое восстановление и exact source
SHA/counts, после чего ставит идемпотентный durable proof в target receipt.
`commit` без этого proof отклоняется. Production restore не является rollback
этого протокола.

## Deploy ownership

Docker image включает только бинарь и pinned PostgreSQL 18 `pg_dump/pg_restore`.
`deploy/k8s/base/legacy-data-migration` содержит suspended Job, отдельный
ServiceAccount, retained PVC, Vault CSI, source/target CA, exact NetworkPolicy,
ServiceMonitor и alerts. Manifest по умолчанию — `suspend: true`, placeholder
digest/plan и `dry-run`; он не является разрешением на запуск. Отдельный
owner-approved execution PR обязан закрепить digest, plan ID, один mode и для
`restore-verify` добавить отдельные restore credential/CA mounts.
После authoritative `COMMITTED` отдельная cleanup wave #197 переводит
выделенные LOGIN в `NOLOGIN`, отзывает membership, завершает оставшиеся
sessions и делает exact `session_user` readback; job сама не расширяет scope до
credential retirement.

Runbook: [legacy-data-migration](../../../docs/runbooks/legacy-data-migration.md).
