---
id: RUN-MC-014
title: Перенос legacy MatterCodex data
type: runbook
status: approved
owner: developer
version: 1.2.0
updated: 2026-08-09
---

# Перенос legacy MatterCodex data

Runbook относится к `services/jobs/legacy-data-migration` и
`deploy/k8s/base/legacy-data-migration`. Он описывает code-first protocol, но
не разрешает deploy, backup, restore или migration.

## Подготовка legacy PostgreSQL source по #241

Source boundary материализуется только скриптом
`tools/legacy-postgresql-source/manage.sh` из exact merged Git revision. Скрипт
не запускает `legacy-data-migration` и не читает строки предметных таблиц.
Команды `apply`, `publish-client`, `readback` и `rollback` требуют явного
`--owner-approved`; наличие cluster-admin доступа само по себе gate не даёт.

До source apply штатный lifecycle bot-service должен отдельно применить schema
migration `000041_legacy_data_cutover.sql`. Это подготовка capability-role,
закрытого inventory, snapshot/fence functions и receipt table, а не запуск
переноса #196. `manage.sh preflight` проверяет только Kubernetes readiness,
закреплённый image, PostgreSQL metadata и наличие этих объектов. Отсутствие
`000041` закрыто останавливает apply до любых изменений.

Из checkout принятой revision выполнить:

```bash
revision="$(git rev-parse HEAD)"
tools/legacy-postgresql-source/manage.sh render \
  --revision "$revision" \
  --render-dir /tmp/legacy-postgresql-source-render
tools/legacy-postgresql-source/manage.sh preflight --revision "$revision"
```

После отдельного owner OK source применяется тем же кодом:

```bash
tools/legacy-postgresql-source/manage.sh apply \
  --owner-approved \
  --revision "$revision"
```

Apply создаёт namespaced cert-manager CA generation `g1`, leaf Certificate с
единственным SAN
`mattermost-postgres-migration.matter-kodex-prod.svc.cluster.local`, отдельный
Service и exact NetworkPolicy. PostgreSQL получает key через initContainer в
`emptyDir` с owner UID/GID `999` и mode `0600`; server разрешает только
`TLSv1.3`. Существующие Mattermost и legacy bot-service продолжают использовать
прежний Service и могут сохранить внутренний plaintext path.

Credential создаётся один раз без вывода значения в Secret
`legacy-data-migration-source-postgresql-g1`. LOGIN
`matter_codex_migration_g1` сначала создаётся как `NOLOGIN`, получает ровно одну
membership в `matter_codex_migration` и включается только после успешного TLS
rollout. Capability-role даёт `SELECT` только на закрытый source inventory,
`SELECT|INSERT|UPDATE` на
`public.matter_codex_legacy_data_cutovers` и `EXECUTE` только на утверждённые
snapshot/fence functions. Superuser, owner, database/schema/role creation,
replication, RLS bypass, business DML, receipt `DELETE` и дополнительная
membership запрещены и проверяются фактически под LOGIN principal.

Readback Job устанавливает соединение только с `sslmode=verify-full`, exact
hostname/SNI, доверенной `g1` CA и min/max `TLSv1.3`. Она сравнивает фактически
обслуживаемый DER certificate с текущим cert-manager leaf, требует единственный
exact SAN, проверяет `pg_stat_ssl` и запускает канонический
`principal__readback.sql`. Probe читает только certificate, transport,
catalog/ACL metadata; snapshot/fence functions и receipt/business rows не
вызываются и не читаются.

Namespace `mattercodex-system` и migration Job принадлежат отдельной wave.
После их появления credential, публичная CA и bounded readback публикуются
явной командой, которая сразу проверяет путь из client namespace:

```bash
tools/legacy-postgresql-source/manage.sh publish-client \
  --owner-approved \
  --revision "$revision"
```

Повторный served-state readback без запуска миграции:

```bash
tools/legacy-postgresql-source/manage.sh readback \
  --owner-approved \
  --revision "$revision" \
  --scope source
```

### Ротация и rollback source endpoint

Leaf Certificate имеет `rotationPolicy: Always`, срок 90 дней и окно renewal
30 дней. После изменения cert-manager Secret повтор той же команды `apply` с
merged revision пересчитывает certificate fingerprint, перезапускает
StatefulSet и выполняет served-state readback; пропущенное обновление не
считается принятым, пока этот readback не успешен. CA `g1` не ротируется на
месте. Переход на `g2` требует отдельного reviewed PR с новым именем CA/Secret,
явным overlap trust, новым leaf, client publication, served-state readback и
только затем retirement `g1`; перезапись trust root на месте запрещена.

Перед каждым source rollout скрипт сохраняет immutable ConfigMap
`mattermost-postgres-migration-rollout-<git-sha-12>` с exact previous
ControllerRevision number/name, Git SHA и certificate fingerprint. До cutover
#196 owner-approved rollback выполняется кодом:

```bash
tools/legacy-postgresql-source/manage.sh rollback \
  --owner-approved \
  --revision "$revision"
```

Rollback сначала переводит LOGIN в `NOLOGIN` и завершает его sessions, затем
возвращает exact previous StatefulSet revision, удаляет migration Service/HBA
и source NetworkPolicy текущего render и подтверждает readiness Mattermost и
bot-service. Credential, PKI и immutable rollout record сохраняются для
расследования и воспроизводимого повторного apply. После необратимой owner
границы #196 этот transport rollback не является rollback данных и не
разрешён вместо forward recovery протокола #196.

## Обязательный внешний gate

Перед **любым исполнением** `dry-run`, `pre-commit`, `commit`, `rollback` или
`restore-verify` Issue
[#241](https://github.com/codex-k8s/matter-codex/issues/241) должен быть закрыт
и принят владельцем. Это обязательный prerequisite и для cutover #196, и для
cleanup #197. До него запрещены Job unsuspend, source connection probe, backup,
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
возврат legacy writer запрещены. Переключение consumers и #197 выполняются
отдельными owner-approved действиями, не этой job.

Crash recovery:

| Source | Owner plan | Действие |
| --- | --- | --- |
| `PREPARED` | `PREPARED` | До owner gate допустим `rollback`; иначе повтор exact `commit` |
| `FROZEN` | `PREPARED` | Legacy writes уже закрыты; выбрать повтор `commit` либо до cutover `rollback` по owner decision |
| `FROZEN` | `COMMITTED` | Только повтор exact `commit`, rollback запрещён |
| `COMMITTED` | `COMMITTED` | Полный authoritative readback каждой operation/audit/provenance receipt; дальнейшие действия только #197 |
| любой mismatch/missing receipt | любой | Fail closed, не создавать receipt вручную |

## Rollback и restore boundary

`rollback` разрешён только пока owner plan не `COMMITTED`. Он сначала вызывает
typed owner `Abort`, затем переводит source intent в `ABORTED` и снимает fence. Backup и
manifest не удаляются. После abort тот же plan ID не переиспользуется.

`restore-verify` восстанавливает только disposable verification DB и не
является production rollback. После owner `COMMITTED` job никогда не обещает
возврат к legacy source: разрешены только forward recovery, authoritative
readback и отдельная cleanup wave #197. Production restore, если когда-либо
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
