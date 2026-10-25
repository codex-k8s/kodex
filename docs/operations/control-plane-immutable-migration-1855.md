---
id: OPS-CP-IMMUTABLE-MIGRATION-1855
title: Адресная immutable-миграция outbox Control Plane
type: operation-plan
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-25
---

# Миграция outbox — #1855

Источник: <https://github.com/codex-k8s/kodex/issues/1855> и проверка #1830.
Это закрытый профиль `outbox-index` существующего
`tools/release/control-plane-migration.mjs`, только для `k3d-kodex`,
`kodex-system`, migration version `20261025000100`. Остальные migration profiles
сохраняют свой прежний hot-reload контракт. Новые ключи, роли, NetworkPolicy,
host mounts, доступ моделей и универсальный deploy framework не создаются.

## Порядок и граница доступности

Обычный RollingUpdate нового CP перед индексом ненадёжен: bounded claim может
завершить новый процесс до Ready; `maxUnavailable=0` сохранит старые Pod с
дорогими запросами. Сначала требуется согласованное окно недоступности CP.
Отсутствие активных модельных ходов, задач и других поставок проверяет оператор.
API/PWA, PostgreSQL, брокер и соседние приложения этим инструментом не меняются.

| Фаза | Полномочие и эффект | Восстановление |
| --- | --- | --- |
| `plan` | Только read; UID/RV/spec CP, namespace/cluster, SA/Secret/CA metadata, source/build/publication pins | Новый приватный plan, эффектов нет |
| `stop` | Durable intent, CAS exact UID/RV, replicas `2→0` и собственный marker | Polling budget120секунд для exact Deployment/ReplicaSet Pod join; UNKNOWN требует inspect |
| `apply` | Только после stop/join; один create-only exact Job | Durable CREATE_INTENT запрещает повторный create, включая отсутствие Job после UNKNOWN |
| `inspect` | Только Kubernetes read; безопасный статус и journal readback | Фактический Job UID, imageID и exact goose version; без повторения effects |
| `resume` | После authoritative Job terminal и прекращения всех его процессов: CAS replicas `0→2` | Возвращает прежний exact CP image независимо от PASS/FAIL миграции; ожидает2 Ready с polling budget120секунд |

После `resume` новый bounded CP устанавливается отдельным свежим штатным
`scoped-release.mjs` plan. Migration tool не меняет CP image. Ни `stop`, ни
`resume`, ни `apply` не запускаются автоматически при ошибке другой фазы.
API reads имеют отдельный timeout30секунд (процесс kubectl —35секунд);
текущий read может завершиться после polling budget, поэтому это не обещание
общей длительности команды120секунд. Все запросы и число effects ограничены.
Оператор обязан завершить восстановление CP после terminal Job, в том числе
после DDL FAIL. Если CREATE_INTENT уже записан, но Job не найден, исход UNKNOWN:
resume и новый create закрыты до отдельного разбора владельцем; отсутствие
объекта не доказывает отсутствие ещё исполняющегося запроса create.

## Предварительные условия

1. Полный clean source SHA содержит канонический файл
   `services/internal/control-plane/cmd/cli/migrations/20261025000100_outbox_unpublished_predecessor_index.sql`.
   Это последняя миграция в source. Build receipt `control-plane` обязан иметь
   **тот же exact SHA**; publication plan/evidence относятся к тому же
   context/cluster/namespace и digest. Ancestor build в этом узком профиле
   не принимается. После интеграции tool/docs образ при необходимости собирается
   заново штатным stable build/publish механизмом.
2. Подготовлены приватные `0700` каталог, `0600` plan/render/evidence. Передаётся
   stable render текущего контура из `tools/release/render-web-only.sh`;
   используются только его `Job/control-plane-migrate` и точные existing refs.
   Отсутствие живого базового Job после TTL допустимо. Нельзя передавать старый
   host-source render вместо immutable base.
3. Existing SA `control-plane-migrator`, Secret
   `control-plane-postgres-migration` и CA ConfigMap
   `control-plane-postgresql-ca` должны принадлежать `kodex`. Tool запрашивает
   только их metadata UID/RV/owner label, без data/stringData и без чтения DSN.
   UID/RV закреплены и проверяются до остановки/создания и при migration readback.
   Existing Secret mutable: на всё окно исключаются сторонние writers/rotation.
   Эти rechecks не являются атомарной блокировкой Kubernetes credential rotation.
4. Прежний CP — healthy immutable replicas2, без hostPath; одновременные
   rollout/config writers приостановлены. Plan фиксирует его spec digest и image.
   Подмена UID, spec или metadata marker закрыто отклоняется.
5. Registry DNS/CA/node trust уже настроены существующим stable delivery.
   Image имеет вид `registry.local.kodex/kodex/control-plane@sha256:...`.
   Publication PASS означает `REGISTRY_PUBLICATION_ONLY`, а не node pull PASS:
   actual imageID впервые проверяется у exact Job-owned Pod.

## Команды оператора

`QA_SOURCE`, `CP_SHA`, `CP_RENDER`, `PUB_PLAN`, `PUB_EVIDENCE`, `MIGRATION_DIR`
ниже содержат только пути/публичный SHA, не значения credentials. `PUB_EVIDENCE`
— свежий двухстрочный `apply.jsonl` либо подтверждённый `readback.jsonl` штатной
publication: INTENT exact plan hash → PASS. Failed/UNKNOWN receipt не подходит.

```bash
umask 077
mkdir -m 700 "$MIGRATION_DIR"
node tools/release/control-plane-migration.mjs plan \
  --context k3d-kodex --profile outbox-index \
  --source "$QA_SOURCE" --revision "$CP_SHA" --base-render "$CP_RENDER" \
  --publication-plan "$PUB_PLAN" --publication-evidence "$PUB_EVIDENCE" \
  --plan "$MIGRATION_DIR/plan.json"
```

Оператор читает plan, подтверждает exact SHA/digest, исходные UID/RV, окно
недоступности и отсутствие моделей. Только затем выполняет отдельные фазы:

```bash
node tools/release/control-plane-migration.mjs stop \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

`STOPPED` доказывает отсутствие живых процессов exact Deployment lineage,
включая terminating старые Pod. Отдельный разрешённый PostgreSQL metadata
readback оператора должен подтвердить отсутствие прежних claim transactions;
при незавершённом запросе миграция не начинается. Запросы выполняются существующим
repo-owned kubectl/psql transport в `BEGIN READ ONLY` с локальными bounded
timeouts: без payload, query text, DSN, env или secret values в выводе.
Перед `up` требуется exact исходная goose version `20261020000200` либо уже
применённая `20261025000100`; другие pending migrations требуют отдельного
разбора, поскольку embedded CLI `up` применяет все ожидающие версии.

```bash
node tools/release/control-plane-migration.mjs apply \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
node tools/release/control-plane-migration.mjs inspect \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl"
```

Job имеет `backoffLimit:0`, deadline300секунд, TTL86400, requests25m/32Mi,
limits250m/128Mi, `Always` pull, один container с
`/usr/local/bin/control-plane-cli up`, uid/gid10001, read-only root, dropALL,
seccompRuntimeDefault, `automountServiceAccountToken:false`. Только миграторский
DSN file mount и CA mount; source/cache mounts, init, envFrom отсутствуют.
Mode CA задаётся явным decimal288, DSN volume292. Schema SQL ограничивает DDL
lock timeout8секундами и statement timeout30секундами внутри goose-транзакции.
При lock timeout нет ручного cancel/skip/drop или бесконечного retry.

`RUNNING` означает лишь наблюдаемую незавершённую Job. `PASS` требует exact
goose version **и** одного exact Job-owned Pod без restart, с успешным exit и
фактическим imageID. `READBACK_UNCONFIRMED` и `FAIL` возвращают exit1.
Ни лог, ни Complete по отдельности не доказывают успешную поставку.

После authoritative terminal Job и прекращения всех её процессов обязательно
восстановить прежний CP, даже если migration readback дал FAIL/UNCONFIRMED:

```bash
node tools/release/control-plane-migration.mjs resume \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

Resume не зависит от доступности исходного checkout и свежести credential
metadata: он восстанавливает exact прежний CP spec. Он запрещён при ещё
исполняемой/неизвестной Job, чужом UID, изменённом CP spec или незавершённых
процессах мигратора. Failed DDL может вернуть прежнюю деградацию outbox;
`RESUMED` подтверждает доступность прежнего бинарника, а не исправление #1855.

## UNKNOWN, restart и возврат

Журнал fsync сохраняет intent до effects. После неизвестного stop/resume
сначала отдельный inspect того же plan/journal. Если desired state уже виден,
повторный вызов лишь ожидает readback. Если PATCH не применился, повторный CAS
допустим только после durable inspect точного прежнего RV; blind retry запрещён.
Если после UNKNOWN stop всё ещё виден прежний spec, resume не объявляет
`UNCHANGED`: запоздавший stop ещё возможен. После inspect сначала завершается
тот же CAS stop/join, затем разрешается resume; старый RV уже не сможет вновь
остановить восстановленный CP.
После CREATE_INTENT apply никогда не создаёт вторую Job. Первая наблюдённая
Job UID закрепляется и после lost ACK; её replacement отклоняется.

При SIGKILL локальный journal lock сохраняется: inspect остаётся доступен
read-only и возвращает `evidenceReadOnly:true`. Этот режим не пишет durable
readback и не открывает mutations. Владелец отдельно проверяет owner/inode
exact lock, PID и отсутствие работающего writer; только после подтверждённого
завершения процесса допускается удаление **этого локального lock**, без
изменения plan/journal. PID reuse/живой процесс запрещает снятие lock. Затем
повторяется обычный inspect и нужная фаза. Автоматического unlink lock нет.
Утрата plan/journal или постоянное отсутствие Job после CREATE_INTENT требует
отдельного owner recovery, а не нового plan/idempotency key.

После PASS дополнительно проверить index definition/validity, goose version,
продвижение прежнего outbox ordering key и browser realtime delta/resync.
Индекс остаётся forward-only; возврат старого приложения его не удаляет.
Полная приёмка восстановления realtime этим operator unit не объявляется.

## Локальная проверка

```bash
make test-control-plane-migration
```

Node tests используют canonical base и реальные subprocess CLI с изолированным
kubectl fixture. Покрываются authority/source/publication drift, отсутствие
secret payload reads, lifecycle, потеря ACK create/stop/resume, SIGKILL,
UNKNOWN/CAS fences, чужие UID, timeout/join и возврат прежнего image после FAIL.
Живые stop/DDL/resume/node pull и browser acceptance — отдельные NOT RUN до
явного запуска root. Context7 недоступен (`Invalid API key`); сверены официальные
[Kubernetes Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
и [object identity](https://kubernetes.io/docs/concepts/overview/working-with-objects/).
