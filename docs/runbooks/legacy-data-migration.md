---
id: RUN-MC-014
title: Перенос legacy MatterCodex data
type: runbook
status: approved
owner: developer
version: 1.5.0
updated: 2026-08-19
---

# Перенос legacy MatterCodex data

Runbook относится к `services/jobs/legacy-data-migration` и
`deploy/k8s/base/legacy-data-migration`. Он описывает code-first protocol, но
не разрешает deploy, backup, restore или migration.

## Подготовка legacy PostgreSQL source по #241

Source boundary материализуется только скриптами
`tools/legacy-postgresql-source/apply-schema-000041.sh` и
`tools/legacy-postgresql-source/manage.sh` из exact merged Git revision. Они не
запускают `legacy-data-migration` и не читают строки предметных таблиц. Каждая
изменяющая production команда требует одновременно `--owner-approved`, exact
`--revision`, идентификатор согласованного окна
`--maintenance-window-id` и bounded `--max-outage-seconds`; наличие
cluster-admin доступа само по себе gate не даёт.

### Exact подготовительная migration 000041

До source apply отдельное owner-approved окно применяет ровно
`000041_legacy_data_cutover.sql` штатным startup lifecycle bot-service. Это
создание capability-role, закрытого inventory, snapshot/fence functions и
receipt table, а не data migration #196. Команда закрыто проверяет clean exact
checkout, hash `000041`, inventory без revision после `000041`, текущую goose
version ровно `40` и тем самым отсутствие любой другой pending migration.
Reviewed bot-service image обязан быть закреплён по digest и содержать ту же
merged revision:

```bash
revision="$(git rev-parse HEAD)"
release_lock='/secure/path/release-lock.json'
test "$(jq -r .source_sha "$release_lock")" = "$revision"
bot_image="$(jq -r '.images[] | select(.component == "bot-service") | .pull_ref' "$release_lock")"
migration_image="$(jq -r '.images[] | select(.component == "legacy-data-migration") | .pull_ref' "$release_lock")"
test -n "$bot_image" && test -n "$migration_image"
maintenance_window='owner-window-000041'
tools/legacy-postgresql-source/apply-schema-000041.sh \
  --owner-approved \
  --revision "$revision" \
  --bot-service-image "$bot_image" \
  --maintenance-window-id "$maintenance_window" \
  --max-outage-seconds 300
```

`migration_image` закрепляется в последующем execution manifest без замены
digest и не означает разрешение на запуск Job.

Скрипт сохраняет immutable previous bot-service PodTemplate, Deployment UID,
exact image, Git SHA и migration hash; затем разворачивает только закреплённый
image. Успех требует version `41`, metadata readback таблицы, функций и exact
capability-role, а также functional checks Mattermost и bot-service. При
ошибке прежний PodTemplate восстанавливается только при совпадении UID и
current/candidate digest. Если `000041` уже зафиксирована, schema остаётся на
`41`: `goose down` и DDL rollback запрещены, это явная forward-only boundary.
Повтор допустим только новой reviewed revision и новым owner gate.

### Render, preflight и maintenance window source endpoint

После принятой `000041` из того же checkout выполнить read-only подготовку:

```bash
revision="$(git rev-parse HEAD)"
tools/legacy-postgresql-source/manage.sh render \
  --revision "$revision" \
  --render-dir /tmp/legacy-postgresql-source-render
tools/legacy-postgresql-source/manage.sh preflight --revision "$revision"
```

Владелец явно допускает короткий restart single-node PostgreSQL. Zero downtime
не заявляется. Для initial apply, каждого leaf renewal и rollback окно имеет
один порядок:

1. До изменения проверить `pg_isready`, readiness и функциональные HTTP probes
   Mattermost `/api/v4/system/ping` и bot-service `/readyz`.
2. Закрыть client ingress, перевести principal в bounded `PENDING`, сохранить
   durable attempt и выполнить ожидаемый restart PostgreSQL.
3. Уложиться в `--max-outage-seconds` для каждой restart стадии; превышение,
   TLS/ACL readback failure или post-check failure является rollback trigger.
4. После acceptance повторить database readiness и оба функциональных probe.
   При rollback сначала закрыть LOGIN и доказать zero sessions, затем вернуть
   exact predecessor PodTemplate и повторить те же post-checks.

После отдельного owner OK source применяется тем же кодом и окном:

```bash
tools/legacy-postgresql-source/manage.sh apply \
  --owner-approved \
  --revision "$revision" \
  --maintenance-window-id 'owner-window-source-initial' \
  --max-outage-seconds 300
```

Apply один раз создаёт versioned immutable CA generation `g1`, а cert-manager
использует её только как CA Issuer для candidate leaf с единственным SAN
`mattermost-postgres-migration.matter-kodex-prod.svc.cluster.local`, отдельный
Service и exact NetworkPolicy. Mutable cert-manager Secret никогда не
монтируется PostgreSQL: скрипт проверяет CA, SAN и key match, затем создаёт
versioned immutable runtime Secret и trust ConfigMap. PostgreSQL получает key
из этого snapshot через initContainer в `emptyDir` с owner UID/GID `999` и mode
`0600`; server разрешает только `TLSv1.3`. Отдельный immutable activation
marker держит candidate pod NotReady до независимого served-state readback.
Существующие Mattermost и legacy bot-service используют прежний Service и
сохраняют внутренний plaintext path, но на каждом restart в согласованном окне
возможен короткий перерыв.

Credential generation `g1` создаётся один раз без вывода значения в immutable Secret
`legacy-data-migration-source-postgresql-g1`. LOGIN
`matter_codex_migration_g1` сначала создаётся как `NOLOGIN`, получает ровно одну
membership в `matter_codex_migration`. PostgreSQL catalog comment хранит
durable `PENDING|CURRENT|RETIRED`, Secret UID/resourceVersion и rollout attempt.
`PENDING` получает LOGIN только на пять минут и bounded statement/lock/idle
timeouts. Только после двух TLS/served-certificate/ACL readback и post-checks
состояние атомарно становится `CURRENT` с unlimited validity. Crash/retry
обнаруживает non-CURRENT, выполняет `NOLOGIN`, revoke membership, bounded
session termination и exact rollback. Если initial generation не была принята
и её Secret отсутствует в client namespace, rollback после доказательства zero
sessions удаляет этот principal и source Secret; следующий owner-approved
attempt создаёт новый пароль и новый Secret UID. Принятая либо опубликованная
`RETIRED` generation `g1` не воскрешается.

Capability-role даёт `SELECT` только на закрытый source inventory,
`SELECT|INSERT|UPDATE` на
`public.matter_codex_legacy_data_cutovers` и `EXECUTE` только на утверждённые
snapshot/fence functions. Superuser, owner, database/schema/role creation,
replication, RLS bypass, business DML, receipt `DELETE` и дополнительная
membership запрещены и проверяются фактически под LOGIN principal.

Readback Job устанавливает соединение только с `sslmode=verify-full`, exact
hostname/SNI, доверенной `g1` CA и min/max `TLSv1.3`. Она сравнивает фактически
обслуживаемый DER certificate с immutable candidate/accepted snapshot, а не с
mutable cert-manager Secret, требует единственный exact SAN, проверяет
`pg_stat_ssl` и запускает канонический
`principal__readback.sql`. Probe читает только certificate, transport,
catalog/ACL metadata; snapshot/fence functions и receipt/business rows не
вызываются и не читаются.

Namespace `mattercodex-system` и migration Job принадлежат отдельной wave.
После их появления credential, публичная CA и bounded readback публикуются
явной командой, которая сразу проверяет путь из client namespace:

```bash
tools/legacy-postgresql-source/manage.sh publish-client \
  --owner-approved \
  --revision "$revision" \
  --maintenance-window-id 'owner-window-source-publish'
```

Повторный served-state readback без запуска миграции:

```bash
tools/legacy-postgresql-source/manage.sh readback \
  --owner-approved \
  --revision "$revision" \
  --maintenance-window-id 'owner-window-source-readback' \
  --scope source
```

### Ротация и rollback source endpoint

Leaf Certificate имеет `rotationPolicy: Always`, срок 90 дней и окно renewal
30 дней. Изменение cert-manager Secret создаёт только candidate. Оно не меняет
runtime автоматически: owner выполняет `renew` в новом maintenance window, а
accepted snapshot меняется только после independent readback:

```bash
tools/legacy-postgresql-source/manage.sh renew \
  --owner-approved \
  --revision "$revision" \
  --maintenance-window-id 'owner-window-source-leaf-renewal' \
  --max-outage-seconds 300
```

CA `g1` не является cert-manager `Certificate`, не имеет `renewBefore` и не
заменяется in-place. Переход на `g2` требует отдельного reviewed PR с новыми
именами CA/Secret/Issuer, overlap trust обоих поколений, новым leaf, client
publication и served-state readback. Retirement `g1` разрешён только после
доказательства отсутствия клиентов старого trust root; пропущенное поколение
и автоматическая подмена запрещены.

Каждый source rollout, включая тот же Git SHA, retry и leaf renewal, атомарно
получает новый 20-значный monotonic attempt в mutable index. Immutable pending
record связывает Git SHA, maintenance window, Certificate generation,
candidate Secret resourceVersion/fingerprint, CA fingerprint, StatefulSet UID,
current revision, predecessor attempt и digest трёх PodTemplate. Полный exact
predecessor PodTemplate хранится независимо от ControllerRevision retention.
Digest candidate PodTemplate вычисляется через Kubernetes server-side dry-run,
поэтому учитывает defaults и admission конкретного API server. При переходе из
`PENDING` в `CURRENT` управляющий скрипт явно пересоздаёт StatefulSet pod: pod с
закрытой readiness не блокирует OrderedReady rollout. Crash recovery может
принять уже обслуживаемый `CURRENT` только после повторной проверки immutable
attempt, StatefulSet UID/revision, runtime Secret, activation marker, served
certificate, закрытого client ingress, principal state и независимого readback.
Для просроченного bounded `PENDING` тот же exact principal получает новое
пятиминутное окно только после этих проверок; новый credential и новый attempt
при этом не создаются. Failed readback Job останавливает recovery сразу, не
дожидаясь таймаута условия `Complete`.
После успеха отдельный immutable acceptance record сохраняет applied revision,
accepted PodTemplate snapshot/digest и served fingerprint. Crash recovery и
rollback допускают изменение только при совпадении current attempt,
StatefulSet UID, current digest и fingerprint; missing, recreated, stale или
изменённый последующим Mattermost rollout state отклоняется.

До cutover #196 owner-approved rollback выполняется с exact attempt из ledger:

```bash
tools/legacy-postgresql-source/manage.sh rollback \
  --owner-approved \
  --revision "$revision" \
  --attempt '<20-digit-current-attempt>' \
  --maintenance-window-id 'owner-window-source-rollback' \
  --max-outage-seconds 300
```

Rollback с ненулевыми statement/lock timeout проверяет boolean каждого
`pg_terminate_backend`, повторно доказывает zero live sessions и при
недоказанном revoke останавливается. Затем он восстанавливает exact predecessor
PodTemplate из immutable record и подтверждает PostgreSQL, Mattermost и
bot-service. Credential становится `RETIRED`; PKI snapshots и ledger
сохраняются для расследования. После необратимой owner границы #196 этот
transport rollback не является rollback данных и не разрешён вместо forward
recovery протокола #196.

## Bounded import активной конфигурации direct production

Режим `configuration-import` предназначен для owner-approved переноса
настроек в уже развёрнутый новый control-plane без переноса legacy runtime
истории. Он читает всю source inventory в одном repeatable-read snapshot,
создаёт один зашифрованный backup legacy DB, а затем строит отдельный bounded
owner plan для каждого проекта. В target переносятся только активные проекты,
чаты, роли и их bindings, используемые OpenAI/GitHub credentials,
репозитории, runtime variables, policy revisions, bot identities и schedules.
Session, turn, process run, callback, memory, artifact и другая runtime history
не материализуются.

Credential values не входят в plan и report. Owner execution script копирует
только используемые source Secrets в versioned immutable Kubernetes Secret
snapshots целевого namespace, вычисляет hash выбранного credential key и
передаёт control-plane exact `Secret UID:resourceVersion`, content hash и
immutable reference. Значения не выводятся. Несовпадение существующего
snapshot, source hash или owner evidence закрыто останавливает запуск.

Для direct-production выполняется только скрипт из exact clean merged
revision и exact release lock:

```bash
tools/legacy-configuration-import/run-direct-production.sh \
  --owner-approved \
  --revision "$(git rev-parse HEAD)" \
  --lock /secure/path/release-lock.json \
  --plan-id '<new-uuid>' \
  --source-root-reference '<new-uuid>' \
  --source-root-sha256 '<approved-64-hex-source-root-digest>' \
  --organization-id '<owner-organization-uuid>' \
  --owner-actor-id '<owner-actor-uuid>' \
  --context '<exact-kubernetes-context>'
```

Скрипт получает live image-policy evidence, создаёт короткоживущий signed
application grant, рендерит scope `migration`, снимает `suspend` только с
этого Job и ждёт terminal outcome. Base manifest остаётся suspended. Повтор с
тем же plan ID допустим только при неизменных source root, credential snapshots
и owner evidence; для изменившегося source используется новый plan ID.

## Обязательный внешний gate

Перед **любым исполнением** `dry-run`, `pre-commit`, `commit`, `rollback`,
`restore-verify` или `configuration-import` Issue
[#241](https://github.com/codex-k8s/matter-codex/issues/241) должен быть закрыт
и принят владельцем. Это обязательный prerequisite и для cutover #196, и для
post-COMMITTED cleanup [#271](https://github.com/codex-k8s/matter-codex/issues/271).
Issue #197 владеет только своей dark-deploy wave и не владеет TLS/credential
cleanup. До закрытия #241 запрещены Job unsuspend, source connection probe, backup,
restore и live migration. Наличие реализации в репозитории gate не снимает.

#241 должен материализовать TLS 1.3 endpoint legacy PostgreSQL, exact SAN/SNI,
trusted cert-manager CA, отдельный Kubernetes credential и
NetworkPolicy/readback. Job дополнительно
проверяет URL и отклоняет plaintext, `sslmode=disable`, `sslmode=require`, IP,
host override, другой CA и negotiated protocol не `TLSv1.3`. Отключать
проверку для диагностики запрещено.
`pg_dump`, `pg_restore` и их `psql` readback получают только закрытый набор
`PG*`: DSN не передаётся как routing string, а раскладывается на exact host,
port, database, user, password, CA, `verify-full` и min/max TLS 1.3.

## Ownership и подготовка execution PR

Каждая фаза запускается только после отдельного owner OK из отдельного
reviewed execution PR. В нём нужно:

1. Закрепить подписанный image digest и новый устойчивый `plan ID`.
2. Оставить ровно один явный mode: `dry-run`, `pre-commit`, `commit`,
   `rollback` или `restore-verify`.
3. Подтвердить exact source principal с membership только в
   `matter_codex_migration`, workload mTLS, signed application grant, source
   root authority reference/digest, exact control-plane RPC allowlist и
   retained PVC. Target PostgreSQL credential job не получает.
4. Для `restore-verify` добавить к Job отдельные CSI volume
   `legacy-data-migration-restore`, CA `legacy-data-restore-postgresql-ca` и
   env `LEGACY_DATA_MIGRATION_RESTORE_DSN_FILE`,
   `LEGACY_DATA_MIGRATION_RESTORE_TLS_SERVER_NAME`,
   `LEGACY_DATA_MIGRATION_RESTORE_CA_FILE`. Verification DB создаётся
   отдельным controller path, пуста и называется
   `mattercodex_restore_<12..32 lowercase hex>`.
5. Снять `suspend` только в этом execution PR. Не менять env через ручной
   `kubectl set env`; не выполнять SQL вручную.

Не выводить DSN, token, backup key, CA material, private key, cookies, source
rows, messages, actor names или archive. Диагностика использует только Job
status, bounded metrics, safe report и receipts.

## Read-only preflight

После закрытия #241 и owner OK, но перед первой фазой:

- сверить exact image digest и render:
  `kubectl kustomize deploy/k8s/base/legacy-data-migration`;
- проверить, что Job всё ещё `suspend: true` в базовом manifest, `backoffLimit:
  0`, PVC имеет `Prune=false`, pod-private `emptyDir` имеет `sizeLimit: 2Gi`, а
  NetworkPolicy разрешает только exact DNS, source/restore PostgreSQL,
  control-plane, authority issuer и Prometheus paths;
- проверить readiness source и owner RPC без раскрытия credentials: source
  principal имеет `SELECT` только на exact 50-table allowlist,
  snapshot/lock/cutover receipt; workload grant разрешает только
  `PrepareLegacyGraphMigration`, `MaterializeLegacyGraphMigration`,
  `GetLegacyGraphMigration` и `AbortLegacyGraphMigration`. Job не имеет target
  DSN, generic operation API, прямого receipt или business DML;
- подтвердить штатным migration readback, что bot-service migration `000041`
  и owner materializer migration `20260807024700` из #249 уже применены; job не применяет
  schema migrations сама и не получает DDL authority;
- убедиться, что другого `FROZEN`/`COMMITTED` winner нет, а выбранный plan ID не
  относится к другому source/target digest;
- подтвердить свободное место retained storage выше ожидаемого `pg_dump` с
  запасом; backup key — 32 bytes в strict base64 и доступен только job role;
- подтвердить, что bot-service продолжает писать source до `commit`, а typed
  owner materializer из #249 доступен по рабочему RPC path. Не использовать dry-run как readiness source до
  закрытия #241.

## Фазы

### 1. dry-run

Запустить owner-approved Job с `MODE=dry-run`. Он открывает repeatable-read
source snapshot и строит deterministic closed typed owner request без target
effect. Успех требует всех violation
counters равными нулю. Сохранить вне публичного канала safe report и его SHA;
повтор с тем же immutable input обязан дать те же `sourceSha256`,
`mappingSha256`, `ownerRequestSha256`, `materializationSha256/count`, counts и
`planSha256`. Owner semantic `targetSha256` появляется только в `pre-commit`
после authoritative `Prepare` readback.

Ненулевой `unknown_state`, `orphan_reference`, `duplicate_source`,
`tenant_mismatch`, `stale_reference`, `broken_lineage`,
`unmaterialized_active`, `ambiguous_target` или `unsupported_state` — blocker.
Ничего не исправлять прямым SQL и не угадывать owner.

Для каждого source Role проверить, что request содержит весь будущий owner
graph: RoleDefinition, опубликованный InstructionSet, ProviderReference/Pool,
RoleImageRecipe/ImageBuild/ImageArtifact, Agent и AgentAssignment. Target IDs,
versions и derived digests отсутствуют в caller payload и назначаются owner;
deploy-owned artifact/image evidence должно быть exact и immutable. Любой
missing/archived/stale source edge блокирует plan.

Перед `pre-commit` должны отсутствовать незавершённые legacy work claims,
owner-attention requests, callback deliveries, thread contexts и
Schedule occurrences/runs, а interaction capabilities должны быть `consumed`
либо `revoked`. Каждый enabled daily legacy Schedule обязан дать одну
детерминированную typed Schedule operation. Проверить
Project/Chat/Agent, cron/timezone/next run, overlap/coalesce/misfire/retry,
playbook/prompt/callback pins, Session/RuntimeRevision/Process lineage и prompt
Artifact eligibility. Неоднозначный successor либо unsupported source state
блокирует plan и требует owner decision; Schedule нельзя молча отключать или
архивировать.

Каждый Artifact, доступный active Turn, InstructionSet, RuntimeRevision или
Process result, должен быть `ACTIVE`, `CLEAN`, иметь exact scan policy/evidence,
положительные immutable version/size, SHA-256 и единственный version-pinned
`s3://...?...versionId=...` storage ref. Missing, mutable, quarantined,
stale либо cross-tenant Artifact блокирует план до необратимой границы.

Standalone active `agent_run`, несвязанный active Turn, делегация без terminal
callback Turn, единственного manifest и двух delivered destinations также
блокируют план. Состояние `configured` у thread context является
незавершённым, а не неизвестным; перед cutover оно должно штатно перейти в
`closed`. Orphan memory record/version/embedding, неверный scope, broken
`supersedes` и неизвестное состояние любой архивируемой lifecycle-сущности
исправляются только через её штатного владельца.

### 2. pre-commit и restore-verify

После сверки двух одинаковых dry-run выполнить `pre-commit` тем же plan ID.
Фаза держит exported snapshot, создаёт exact 50-table encrypted immutable dump
без чужих `public` tables и без post-data objects, и manifest, проверяет
HMAC/closed `pg_restore --list`, затем фиксирует owner `PREPARED` immutable
plan/operation receipts через #249 и source `PREPARED` receipt. До
этого source/target cutover effects отсутствуют.
Authenticated plaintext staging разрешён только через `TMPDIR` на
pod-private bounded `emptyDir`; файл немедленно unlink-ится и не сохраняется в
backup PVC. До decrypt/write код синхронно проверяет ciphertext/envelope и
plain size против `LEGACY_DATA_MIGRATION_MAXIMUM_STAGING_BYTES=2013265920`.
Это ниже `sizeLimit: 2Gi` и оставляет запас для каталога и deleted-but-open
inode; eviction не считается механизмом ограничения.

До `commit` обязательно выполнить `restore-verify` в отдельной пустой DB.
`pg_restore --single-transaction` должен завершиться успешно, а повторный
snapshot — дать exact source SHA и все table counts из manifest. Сохранить
restore verification report и его SHA; source receipt должен получить exact
`restore_verified=true` readback. `commit` без него невозможен. Verification DB
после фиксации evidence удаляет только её controller по отдельному
owner-approved path.

Если crash произошёл после durable fsync dump, но до создания manifest, повтор
того же `pre-commit` завершает sidecar только после HMAC, `pg_restore --list`
и совпадения source SHA/count с аутентифицированным header. Manifest mismatch,
неаутентифицируемый dump, неизолированная или непустая restore DB и отличие
digest/count блокируют
plan. Если crash произошёл после `--single-transaction` restore, но до
durable `restore_verified`, controller удаляет только disposable verification
DB, создаёт новую пустую DB и повторяет `restore-verify`. Job никогда не
принимает непустую DB как evidence. Не удалять файл и
не создавать manifest вручную; расследовать storage/crash и выбрать новый plan
ID только после owner decision.

### 3. commit

Перед необратимой фазой ещё раз сверить source `PREPARED`, owner plan readback,
durable `restore_verified` source readback и получить отдельный owner gate.
`commit` берёт source lock; target locks принадлежат owner RPC. Job заново строит plan и
сравнивает все digests, повторно аутентифицирует backup stream и сверяет
manifest SHA/counts с durable receipts. Любой concurrent drift или повреждение
backup evidence завершает Job до fence.

После source `FROZEN` legacy writes закрыто отвергаются. Затем target owner
повторно разрешает Project/Chat/Agent/configuration/Artifact authority и одной
transaction материализует весь planned Project/Team/Chat/Agent/configuration/
Session/Turn/Attempt/Process/Schedule/delegation/callback graph. Root actor,
legacy policy digest, отдельный machine authority policy, parent/predecessor,
launching Session/Turn/Attempt, callback route, audit и provenance фиксируются
operation-specific receipts. Job вызывает
`GetLegacyGraphMigration(verifyCommitted=true)`: owner перечитывает каждую
target projection, protected history/runtime components, audit и provenance;
missing/drift блокирует source `COMMITTED`. Receipt tuple неизменяем с первого
`PREPARED`; caller не имеет target IDs, generic JSON/DML или table-wide update,
а terminal winner выбирается одной owner transaction. Owner `COMMITTED` — irreversible cutover boundary: rollback и
возврат legacy writer запрещены. Переключение consumers выполняется отдельным
owner-approved действием. Вывод transport/credential из эксплуатации после
проверенного окна принадлежит только
[#271](https://github.com/codex-k8s/matter-codex/issues/271), не этой job и не #197.

Crash recovery:

| Source | Owner plan | Действие |
| --- | --- | --- |
| `PREPARED` | `PREPARED` | До owner gate допустим `rollback`; иначе повтор exact `commit` |
| `FROZEN` | `PREPARED` | Legacy writes уже закрыты; выбрать повтор `commit` либо до cutover `rollback` по owner decision |
| `FROZEN` | `COMMITTED` | Только повтор exact `commit`, rollback запрещён |
| `COMMITTED` | `COMMITTED` | Полный authoritative readback каждой operation/audit/provenance receipt; затем отдельный consumer cutover и cleanup #271 |
| любой mismatch/missing receipt | любой | Fail closed, не создавать receipt вручную |

## Rollback и restore boundary

`rollback` разрешён только пока owner plan не `COMMITTED`. Он сначала вызывает
typed owner `Abort`, затем переводит source intent в `ABORTED` и снимает fence. Backup и
manifest не удаляются. После abort тот же plan ID не переиспользуется.

`restore-verify` восстанавливает только disposable verification DB и не
является production rollback. После owner `COMMITTED` job никогда не обещает
возврат к legacy source: разрешены только forward recovery, authoritative
readback и отдельная cleanup wave #271. Production restore, если когда-либо
понадобится, требует отдельного Issue, owner protocol и нового code-first PR.

## Observability и инциденты

`/health/ready` становится успешным только после config, named-SQL, source TLS
и owner RPC startup barrier. Метрики:

- `mattercodex_legacy_data_migration_ready`;
- `mattercodex_legacy_data_migration_runs_total{mode,outcome}` с закрытыми
  значениями mode/outcome.

Alerts `LegacyDataMigrationFailed` и `LegacyDataMigrationDeadlineNear` ведут
в этот runbook. При SIGTERM operation отменяется, каждый CLI subprocess получает
terminate/kill/wait, а worker фактически join-ится до закрытия DB/files.
Terminal endpoint остаётся доступным с readiness=false не менее 20 секунд, что
покрывает один 15-секундный scrape; durable receipt/report остаётся authority.
Timeout/cancel не означает отсутствие effects: всегда читать source receipt,
owner plan через `GetLegacyGraphMigration` и manifest, затем применять таблицу
recovery выше.

## Короткая ручная проверка владельца

Без доступа к live environment можно проверить только артефакты PR:

1. Выполнить `go test ./...` и `go build ./...` в
   `services/jobs/legacy-data-migration`.
2. Структурно проверить новые forward-only migration files и named SQL без их
   применения к live DB.
3. Выполнить `tools/legacy-postgresql-source/manage.sh render` с exact SHA,
   разобрать обычные ресурсы через `kubectl apply --dry-run=client`, readback
   Job с `generateName` через `kubectl create --dry-run=client`, а StatefulSet
   patch через `kubectl patch --dry-run=client`. Убедиться, что source endpoint
   содержит exact SAN/FQDN, TLS 1.3 и deny-all/exact NetworkPolicy.
4. Выполнить `kubectl kustomize deploy/k8s/base/legacy-data-migration` и
   убедиться, что render содержит suspended Job, отдельный source PostgreSQL
   Secret, exact cross-namespace NetworkPolicy, retained PVC, Vault CSI для
   остальных credentials, ServiceMonitor и HTTPS `runbook_url`.
5. Проверить JSON syntax четырёх report schemas и `contracts/registry.yaml`.
6. Просмотреть safe report schema: в нём нет row payload, secret/DSN/token,
   actor, message, channel/post или credential values.

Фактические `dry-run`, backup, restore, commit, deploy или доступ к
staging/production не являются частью этой проверки и в PR #196 не
выполняются.
