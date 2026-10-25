---
id: OPS-CP-IMMUTABLE-MIGRATION-1855
title: Адресная immutable-миграция outbox Control Plane
type: operation-plan
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-25
---

# Миграция outbox — #1855

Источник: <https://github.com/codex-k8s/kodex/issues/1855> и проверка #1830.
Это закрытый профиль `outbox-index` существующего
`tools/release/control-plane-migration.mjs`, только для `k3d-kodex`,
`kodex-system`, migration version `20261025000100`. Остальные migration profiles
сохраняют свой прежний hot-reload контракт. Новые ключи, роли, NetworkPolicy,
host mounts, доступ моделей и универсальный deploy framework не создаются.

## Диагностика подключения CLI

`/usr/local/bin/control-plane-cli check` использует тот же явно выбранный
`CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE`, что и `up/status`, но выполняет только
разбор pgx config, открытие database/sql handle и один `PingContext`. Goose,
`StatusContext`, чтение/создание таблицы версий и migration SQL не вызываются.
Успешный stdout: `PostgreSQL connection check completed` и перевод строки.
Это проверка выбранного соединения, а не schema readiness, runtime grants,
права на DDL или разрешение миграции. Штатный migration tool не запускает её
автоматически; отдельный live diagnostic Job требует своего разрешения.

Начальный ping всех трёх команд ограничен десятью секундами и меньшим caller
deadline; отмена до/во время/сразу после ping запрещает успешный исход.
`up/status` затем используют исходный caller context, без нового migration
deadline и без повторов SQL. `status` Goose способен инициализировать таблицу
версий и потому не является read-only заменой `check`.

| Исход | Goose / состояние / событие | Авторитетный результат |
| --- | --- | --- |
| `check` PASS | Никаких Goose/migration SQL и domain events | Только fixed stdout после успешного ping; handle закрывается |
| Config/file/parse FAIL | Goose не вызывается | Exit1; закрытые `stage/error_class`, без DSN/path/error text |
| Ping FAIL/deadline/cancel | Goose не вызывается, повторов нет | Exit1; typed cause остаётся приватной, stdout успеха отсутствует |
| `up/status` после ping | Единственный прежний Goose path; новый event не создаётся | Goose version state/readback прежнего migration lifecycle |
| Goose FAIL/partial | Migration SQL не повторяется | Exit1; закрытая диагностика; прежний authoritative migration readback |

Классификация сначала проверяет типизированные PostgreSQL/TLS/config ошибки,
затем cancellation/deadline и общий stdlib-only network classifier.
SQLSTATE нормализует `errorclass.Postgres`; неизвестная причина остаётся
`unknown`. Если pgx database/sql adapter уже заменил ошибку на
`driver.ErrBadConn`, исход — `bad_connection`: исходные TLS/DNS причины не
угадываются. Raw upstream errors, hostname, user, database, query и DSN не
печатаются. На CLI process boundary та же защита действует для broker path.

## Порядок и граница доступности

Обычный RollingUpdate нового CP перед индексом ненадёжен: bounded claim может
завершить новый процесс до Ready; `maxUnavailable=0` сохранит старые Pod с
дорогими запросами. Сначала требуется согласованное окно недоступности CP.
Отсутствие активных модельных ходов, задач и других поставок проверяет оператор.
API/PWA, PostgreSQL, брокер и соседние приложения этим инструментом не меняются.

| Фаза | Полномочие и эффект | Восстановление |
| --- | --- | --- |
| `plan` | Только read; UID/RV/spec CP, namespace/cluster, SA/Secret/CA metadata, source/build/publication pins | Новый приватный plan, эффектов нет |
| `check` | Один create-only read-only Job, canonical init и CLI `check`; CP остаётся2Ready | Durable CHECK_CREATE_INTENT; UNKNOWN разрешает только readback прежнего имени/UID |
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
До stop нужен свежий check PASS: exact Job/Pod UID, imageID, init/main exit0,
restart0 и фиксированный success stdout CLI. Его completionTime не старше5минут;
повторный inspect не продлевает эту свежесть. Failed, RUNNING, UNKNOWN или stale
check оставляет CP без PATCH. Оператор обязан завершить восстановление CP после terminal Job, в том числе
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
   Этот же образ обязан включать CLI `check` с bounded10секунд Ping и без Goose;
   версия `status` для read-only connection probe не используется.
2. Подготовлены приватные `0700` каталог, `0600` plan/render/evidence. Передаётся
   штатный offline render `deploy/k8s/overlays/staging/control-plane`;
   используются только его `Job/control-plane-migrate` и точные existing refs.
   Отсутствие `app.kubernetes.io/part-of` разрешено только на исходном render;
   если метка присутствует, допускается только `kodex`. Namespace, staging,
   component, CLI, env и mounts проверяются строго; новая Job всегда получает
   `part-of=kodex`. Full release lock и ручная правка labels не нужны.
   Отсутствие живого базового Job после TTL допустимо. Нельзя передавать старый
   host-source render вместо immutable base.
3. Existing SA `control-plane-migrator`, Secret
   `control-plane-postgres-migration` и CA ConfigMap
   `control-plane-postgresql-ca` должны иметь существующую authority. Для SA и
   Secret требуется `app.kubernetes.io/part-of=kodex`; для CA — exact
   `trust.cert-manager.io/bundle` и единственный controller ownerRef на
   `Bundle/control-plane-postgresql-ca`, совпадающий с actual Bundle UID/RV.
   Bundle source — только `kodex-installation-ca/tls.crt`, target — `ca.pem`
   только в `kodex-system`; actual spec должен совпасть с canonical
   `deploy/k8s/base/platform-state/ca-bundles.yaml` принятого source.
   Tool запрашивает metadata и публичные source/target refs Bundle,
   без data/stringData, содержимого сертификата и без чтения DSN.
   UID/RV закреплены и проверяются до остановки/создания и при migration readback.
   Existing Secret mutable: на всё окно исключаются сторонние writers/rotation.
   Эти rechecks не являются атомарной блокировкой Kubernetes credential rotation.
4. Прежний CP — healthy immutable replicas2, без hostPath; одновременные
   rollout/config writers приостановлены. Plan фиксирует его spec digest и image.
   Подмена UID, spec или metadata marker закрыто отклоняется.
5. Registry DNS/CA/node trust уже настроены существующим stable delivery.
   Image имеет вид `registry.local.kodex/kodex/control-plane@sha256:...`.
   Publication PASS означает `REGISTRY_PUBLICATION_ONLY`, а не node pull PASS:
   actual imageID впервые проверяется у exact check Job-owned Pod.
6. V2 plan закрепляет UID Pod/StatefulSet PostgreSQL и hash
   `tools/release/control-plane-migration-precondition.sql` из принятого source.
   Existing `Operator.sql` kubectl/psql transport используется только для
   `BEGIN REPEATABLE READ READ ONLY` с statement5s/lock500ms. Проверяются old goose
   `20261020000200`, отсутствие индекса и нулевые owner-счётчики runs/nodes/turns,
   leases/builds/admissions/promotions/archive/catalog/renewal, включая ожидания
   владельца и UNKNOWN renewal. Secret/DSN/query text/payload в результате нет.
   После stop дополнительно требуется отсутствие всех прежних claim sessions.

## Команды оператора

`QA_SOURCE`, `CP_SHA`, `PUB_PLAN`, `PUB_EVIDENCE`, `MIGRATION_DIR`
ниже содержат только пути/публичный SHA, не значения credentials. `PUB_EVIDENCE`
— свежий двухстрочный `apply.jsonl` либо подтверждённый `readback.jsonl` штатной
publication: INTENT exact plan hash → PASS. Failed/UNKNOWN receipt не подходит.

```bash
umask 077
mkdir -m 700 "$MIGRATION_DIR"
kubectl --context k3d-kodex kustomize \
  "$QA_SOURCE/deploy/k8s/overlays/staging/control-plane" > "$MIGRATION_DIR/base.yaml"
node tools/release/control-plane-migration.mjs plan \
  --context k3d-kodex --profile outbox-index \
  --source "$QA_SOURCE" --revision "$CP_SHA" --base-render "$MIGRATION_DIR/base.yaml" \
  --publication-plan "$PUB_PLAN" --publication-evidence "$PUB_EVIDENCE" \
  --plan "$MIGRATION_DIR/plan.json"
```

Оператор читает plan, подтверждает exact SHA/digest, исходные UID/RV, окно
недоступности и отсутствие моделей. Сначала выполняет check при работающем CP:

```bash
node tools/release/control-plane-migration.mjs check \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

`RUNNING` требует inspect/check того же plan/journal, без повторного create.
`PASS` подтверждает только доступность соединения с точным DSN/TLS внутри
доверенного CLI процесса; credentials не попадают в operator output. Ожидаемый
stdout: `PostgreSQL connection check completed`. Иной вывод не даёт PASS.
Check Job имеет фиксированное имя версии/attempt и сохраняется без TTL: это
server-side fence нового plan после UNKNOWN, а не новый key/name для повтора.
Удаление Job и parent receipts этим инструментом не выполняется.
После PASS и fresh owner-preflight разрешается отдельный stop:

```bash
node tools/release/control-plane-migration.mjs stop \
  --context k3d-kodex --profile outbox-index \
  --plan "$MIGRATION_DIR/plan.json" --evidence "$MIGRATION_DIR/attempt.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

`STOPPED` доказывает отсутствие живых процессов exact Deployment lineage,
включая terminating старые Pod. Перед созданием up Job инструмент повторяет
закрытый readonly owner-preflight и требует0 прежних claim sessions. Root может
дополнительно сохранить расширенный metadata snapshot прежнего ordering key.
Перед `up` требуется exact исходная goose version `20261020000200` и indexAbsent;
уже применённая версия и любые другие pending migrations закрывают новый create,
поскольку embedded CLI `up` применяет все ожидающие версии.

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
DSN file mount и CA mount; source/cache mounts и envFrom отсутствуют.
И check, и up сохраняют canonical `wait-for-postgresql` init: exact pinned
postgres18.3-alpine3.23 image, фиксированные host/port5432, `pg_isready` timeout2s
и sleep2s под общим deadline Job300s. Init не получает auth/DSN/CA mounts, envFrom
или SQL; он проверяет accepting server, а exact TLS/auth проверяет CLI Ping.
Allow rules NetworkPolicy могут примениться после старта Pod, поэтому первый
неудачный Ping не доказывает постоянный конфигурационный запрет. Concrete причина
первого failed Job остаётся UNKNOWN; отсутствие startup barrier доказано.
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
Те же правила применяются отдельно к CHECK_CREATE_INTENT/CHECK_READBACK.

## Явная attempt2 после известной failed попытки

V1 plan первой поставки остаётся доступен для исторического inspect/resume.
Новый check/stop/create требует V2. Attempt2 создаётся только явным plan с
двумя ссылками на существующие приватные **неизменяемые** parent receipts:

```bash
node tools/release/control-plane-migration.mjs plan \
  --context k3d-kodex --profile outbox-index \
  --source "$QA_SOURCE" --revision "$CP_SHA" --base-render "$MIGRATION_DIR/base.yaml" \
  --publication-plan "$PUB_PLAN" --publication-evidence "$PUB_EVIDENCE" \
  --predecessor-plan "$PREVIOUS_PLAN" --predecessor-evidence "$PREVIOUS_JOURNAL" \
  --plan "$MIGRATION_DIR/plan.json"
```

Требуются exact parent plan hash/source/publication proof; actual failed Job UID
совпадает с durable create/readback receipts, Job spec/marker не изменились,
все её Pod-процессы завершились. Parent journal должен содержать FAIL readback
и RESUME_ACK, не иметь active writer lock; current old CP — тот же UID/spec,
image и2Ready. Readonly old-goose/indexAbsent/zero-owner gate проходит заново.
UNKNOWN без доказанного failed terminal, отсутствующая Job, чужой UID, незавершённый
Pod или утраченный parent receipt закрывают retry. Child закрепляет parent bytes
hash; последующее изменение parent journal блокирует check/stop/apply.
Для этого допускается отдельная приватная копия metadata-only parent plan/journal
с теми же exact bytes: дальнейший readback исходного журнала не меняет snapshot.
Копии — owned regular0600, без symlink/hardlink; credentials туда не входят.

Child использует новые фиксированные имена `...-attempt2`, сохраняет исходные
Jobs и receipts; его stop меняет marker только CAS `parentPlanHash→childPlanHash`.
Child после этого проходит весь check/stop/apply/resume lifecycle заново на
точном свежем source/image. Повторная attempt2 из attempt2 и automatic attempt3
не поддерживаются. Новый child plan не является разрешением на живой запуск:
нужны review итогового SHA и отдельное решение root на exact plan/window.

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
UNKNOWN/CAS fences, чужие UID, timeout/join и возврат прежнего image после FAIL,
check/init identity/expiry и known-failed attempt2 с неизменяемыми parent receipts.
Живые stop/DDL/resume/node pull и browser acceptance — отдельные NOT RUN до
явного запуска root. Context7 недоступен (`Invalid API key`); сверены официальные
[Kubernetes Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
и [object identity](https://kubernetes.io/docs/concepts/overview/working-with-objects/).
Проверен также официальный [NetworkPolicy Pod lifecycle](https://kubernetes.io/docs/concepts/services-networking/network-policies/#pod-lifecycle).
