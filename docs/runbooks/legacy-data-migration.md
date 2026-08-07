---
id: RUN-MC-014
title: Перенос legacy MatterCodex data
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-07
---

# Перенос legacy MatterCodex data

Runbook относится к `services/jobs/legacy-data-migration` и
`deploy/k8s/base/legacy-data-migration`. Он описывает code-first protocol, но
не разрешает deploy, backup, restore или migration.

## Обязательный внешний gate

Перед **любым исполнением** `dry-run`, `pre-commit`, `commit`, `rollback` или
`restore-verify` Issue
[#241](https://github.com/codex-k8s/matter-codex/issues/241) должен быть закрыт
и принят владельцем. Это обязательный prerequisite и для cutover #196, и для
cleanup #197. До него запрещены Job unsuspend, source connection probe, backup,
restore и live migration. Наличие реализации в репозитории gate не снимает.

#241 должен материализовать TLS 1.3 endpoint legacy PostgreSQL, exact SAN/SNI,
trusted CA, Vault credential и NetworkPolicy/readback. Job дополнительно
проверяет URL и отклоняет plaintext, `sslmode=disable`, `sslmode=require`, IP,
host override, другой CA и negotiated protocol не `TLSv1.3`. Отключать
проверку для диагностики запрещено.

## Ownership и подготовка execution PR

Каждая фаза запускается только после отдельного owner OK из отдельного
reviewed execution PR. В нём нужно:

1. Закрепить подписанный image digest и новый устойчивый `plan ID`.
2. Оставить ровно один явный mode: `dry-run`, `pre-commit`, `commit`,
   `rollback` или `restore-verify`.
3. Подтвердить наличие exact source/target principals, membership только в
   `matter_codex_migration`/`control_plane_migration`, TLS CA и retained PVC.
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
  0`, PVC имеет `Prune=false`, а NetworkPolicy разрешает только DNS, три exact
  PostgreSQL Pod selectors и Prometheus ingress;
- проверить readiness source/target principals без раскрытия DSN: source имеет
  только `SELECT`, snapshot/lock и cutover receipt; target — только RLS readback,
  target lock и receipt;
- подтвердить штатным migration readback, что bot-service migration `000041`
  и control-plane migration `20260807019600` уже применены; job не применяет
  schema migrations сама и не получает DDL authority;
- убедиться, что другого `FROZEN`/`COMMITTED` winner нет, а выбранный plan ID не
  относится к другому source/target digest;
- подтвердить свободное место retained storage выше ожидаемого `pg_dump` с
  запасом; backup key — 32 bytes в strict base64 и доступен только job role;
- подтвердить, что bot-service продолжает писать source до `commit`, а target
  graph из #238/#239 доступен. Не использовать dry-run как readiness source до
  закрытия #241.

## Фазы

### 1. dry-run

Запустить owner-approved Job с `MODE=dry-run`. Он открывает repeatable-read
source snapshot и read-only target inventory. Успех требует всех violation
counters равными нулю. Сохранить вне публичного канала safe report и его SHA;
повтор с тем же immutable input обязан дать те же `sourceSha256`,
`targetSha256`, `mappingSha256`, counts и `planSha256`.

Ненулевой `unknown_state`, `orphan_reference`, `duplicate_source`,
`tenant_mismatch`, `stale_reference`, `broken_lineage`,
`unmaterialized_active`, `ambiguous_target` или `unsupported_state` — blocker.
Ничего не исправлять прямым SQL и не угадывать owner.

Для каждого source Role отдельно сверить target Agent и весь его current
configuration graph: RoleDefinition, опубликованный InstructionSet,
ProviderPool/ProviderReference, RoleImageRecipe и active AgentAssignment.
ID/version/digest должны иметь exact protected-history evidence; configured
legacy bot — совпадающие provider identity, Team и durable receipt. Любой
missing/archived/stale edge блокирует план до штатного owner reconciliation.

Перед `pre-commit` должны отсутствовать незавершённые legacy work claims,
owner-attention requests, callback deliveries, thread contexts и
Schedule occurrences/runs, а interaction capabilities должны быть `consumed`
либо `revoked`. Enabled legacy Schedule без materialized target disposition
также даёт `unmaterialized_active`; отключение либо target replacement
выполняется только штатным owner path и отдельным решением, не ручным
исправлением данных migration job.

Standalone active `agent_run`, несвязанный active Turn, делегация без terminal
callback Turn, единственного manifest и двух delivered destinations также
блокируют план. Состояние `configured` у thread context является
незавершённым, а не неизвестным; перед cutover оно должно штатно перейти в
`closed`. Orphan memory record/version/embedding, неверный scope, broken
`supersedes` и неизвестное состояние любой архивируемой lifecycle-сущности
исправляются только через её штатного владельца.

### 2. pre-commit и restore-verify

После сверки двух одинаковых dry-run выполнить `pre-commit` тем же plan ID.
Фаза держит exported snapshot, создаёт encrypted immutable dump и manifest,
проверяет HMAC/`pg_restore --list`, затем фиксирует `PREPARED` receipts. До
этого source/target cutover effects отсутствуют.

До `commit` обязательно выполнить `restore-verify` в отдельной пустой DB.
`pg_restore --single-transaction` должен завершиться успешно, а повторный
snapshot — дать exact source SHA и все table counts из manifest. Сохранить
restore verification report и его SHA; target receipt должен получить exact
`restore_verified_at` readback. `commit` без него невозможен. Verification DB
после фиксации evidence удаляет только её controller по отдельному
owner-approved path.

Если crash произошёл после durable fsync dump, но до создания manifest, повтор
того же `pre-commit` завершает sidecar только после HMAC, `pg_restore --list`
и совпадения source SHA/count с аутентифицированным header. Manifest mismatch, неаутентифицируемый dump,
неизолированная или непустая restore DB и отличие digest/count блокируют
plan. Если crash произошёл после `--single-transaction` restore, но до
durable `restore_verified_at`, controller удаляет только disposable verification
DB, создаёт новую пустую DB и повторяет `restore-verify`. Job никогда не
принимает непустую DB как evidence. Не удалять файл и
не создавать manifest вручную; расследовать storage/crash и выбрать новый plan
ID только после owner decision.

### 3. commit

Перед необратимой фазой ещё раз сверить оба `PREPARED` receipt, durable
`restore_verified_at` target readback и получить
отдельный owner gate. `commit` берёт source/target locks, заново строит plan и
сравнивает все digests, повторно аутентифицирует backup stream и сверяет
manifest SHA/counts с durable receipts. Любой concurrent drift или повреждение
backup evidence завершает Job до fence.

После source `FROZEN` legacy writes закрыто отвергаются. Затем один target
receipt становится `COMMITTED`, после чего source receipt становится
`COMMITTED`. Target `COMMITTED` — irreversible cutover boundary: rollback и
возврат legacy writer запрещены. Переключение consumers и #197 выполняются
отдельными owner-approved действиями, не этой job.

Crash recovery:

| Source | Target | Действие |
| --- | --- | --- |
| `PREPARED` | `PREPARED` | До owner gate допустим `rollback`; иначе повтор exact `commit` |
| `FROZEN` | `PREPARED` | Legacy writes уже закрыты; выбрать повтор `commit` либо до cutover `rollback` по owner decision |
| `FROZEN` | `COMMITTED` | Только повтор exact `commit`, rollback запрещён |
| `COMMITTED` | `COMMITTED` | Идемпотентный readback; дальнейшие действия только #197 |
| любой mismatch/missing receipt | любой | Fail closed, не создавать receipt вручную |

## Rollback и restore boundary

`rollback` разрешён только пока target receipt не `COMMITTED`. Он переводит
существующие intents в `ABORTED` и снимает source FROZEN fence. Backup и
manifest не удаляются. После abort тот же plan ID не переиспользуется.

`restore-verify` восстанавливает только disposable verification DB и не
является production rollback. После target `COMMITTED` job никогда не обещает
возврат к legacy source: разрешены только forward recovery, authoritative
readback и отдельная cleanup wave #197. Production restore, если когда-либо
понадобится, требует отдельного Issue, owner protocol и нового code-first PR.

## Observability и инциденты

`/health/ready` становится успешным только после config, named-SQL, TLS и DB
startup barrier. Метрики:

- `mattercodex_legacy_data_migration_ready`;
- `mattercodex_legacy_data_migration_runs_total{mode,outcome}` с закрытыми
  значениями mode/outcome.

Alerts `LegacyDataMigrationFailed` и `LegacyDataMigrationDeadlineNear` ведут
в этот runbook. При SIGTERM operation отменяется и join выполняется до закрытия
DB. Timeout/cancel не означает отсутствие effects: всегда читать оба durable
receipt и manifest, затем применять таблицу recovery выше.

## Короткая ручная проверка владельца

Без доступа к live environment можно проверить только артефакты PR:

1. Выполнить `go test ./...` и `go build ./...` в
   `services/jobs/legacy-data-migration`.
2. Проверить `goose validate` для обеих migration directories.
3. Выполнить `kubectl kustomize deploy/k8s/base/legacy-data-migration` и
   убедиться, что render содержит suspended Job, exact NetworkPolicy, retained
   PVC, Vault CSI, ServiceMonitor и HTTPS `runbook_url`.
4. Проверить JSON syntax четырёх report schemas и `contracts/registry.yaml`.
5. Просмотреть safe report schema: в нём нет row payload, secret/DSN/token,
   actor, message, channel/post или credential values.

Фактические `dry-run`, backup, restore, commit, deploy или доступ к
staging/production не являются частью этой проверки и в PR #196 не
выполняются.
