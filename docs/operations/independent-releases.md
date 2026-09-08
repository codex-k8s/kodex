---
id: OPS-DOC-INDEPENDENT-RELEASES-001
title: Независимые релизы и переход worker grants
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-09-08
---

# Независимые релизы

Источник: решение владельца, #1204 и #1031. Реализация выполняется поэтапно;
описанный конечный режим не считается уже проверенным на staging/production.

Обычный релиз меняет только выбранный deployable. Security bootstrap, смена
ключей, общая policy и authority runtime имеют отдельные входы и revisions.
Совместимость относится к протоколу и возможностям, не к общему Git SHA.
Batch состоит из независимых обновлений с ограничением параллелизма и
поэлементным результатом. Ошибка одного участника не откатывает соседей.

## Подтверждённая причина и порядок внедрения

В cdefb3d3 hot-reload renderer назначает девяти grant workloads `Recreate`
и одну реплику. Это workaround для общего watermark: timestamp revision
одного Pod делает действующий grant другого Pod устаревшим. Production base
с двумя репликами сам по себе эту гонку не устраняет.

1. #1220: additive schema и control-plane reader принимают v1 и v2.
2. После готовности всех CP consumers включается v2 signer с Pod UID из
   Kubernetes Downward API. Instance не приходит из пользовательского request.
3. После проверки совместного обслуживания включаются RollingUpdate и scoped
   application release. Общий `up` остаётся bootstrap/reconcile операцией.
4. Проверяются повторные одиночные релизы, mixed versions, batch, отказ участника,
   откат, restart sidecar и outage/rotation authority под нагрузкой.
5. Завершаются прежние STT/runtime/workspace/QA64 и итоговый baseline #1031.

## Grant lifecycle и совместимость

| Переход | Проверка и результат |
| --- | --- |
| v1 issuance | Прежняя подпись и workload-wide revision без изменения wire |
| v2 issuance | Подписанный `v=2`, canonical nonzero UUID `instance_id`, revision=iat, exact workload/SPIFFE/key generation и срок |
| Первый instance | В одной PostgreSQL transaction блокируется общий generation floor и создаётся instance watermark |
| Refresh | Revision растёт только внутри instance; exact envelope digest при повторе обязателен |
| Соседний Pod | Другой подписанный instance того же поколения не двигает revision прежнего |
| Новое поколение | Общий floor исключает прежнее поколение также для неизвестного instance и старого v1 consumer |
| Истечение/повреждение | Закрытый отказ до proof; полномочия не продлеваются |
| Частичный сбой | Transaction rollback сохраняет оба watermark; restart читает устойчивое состояние |
| Откат приложения | Схема и watermarks сохраняются; старый CP нельзя вернуть при активных v2 writers |

Trust origin: отдельный workload signer утверждает instance; после проверки
подписи CP передаёт его typed owner port. Instance только разделяет поток
обновлений, не назначает actor/project/permission. Owner resolver отдельно
проверяет действующий граф, claim/lease и полномочия перед каждым proof.
Grant admission не публикует domain event: authoritative read path —
синхронный PostgreSQL AcceptWorkerGrant. Бизнесовые события не меняются.

Таблица v1 сохраняется как общий generation floor и прежний v1 revision.
V2 не двигает v1 revision внутри поколения. Первое v2 поколение резервирует
revision=1; реальные timestamp revisions v1 остаются выше. SQL блокирует
строку до commit, поэтому concurrent устаревший запрос не использует прежний
statement snapshot. Instance watermarks не очищаются при истечении токена.

Миграция forward-only; откат бинаря не выполняет goose down. Перед v2 rollout
проверяются все CP consumers, а не только один endpoint балансировщика.
Произвольный старый бинарь не обязан понимать новый контракт: независимость
сохраняется в явно поддерживаемом диапазоне контрактов.

## Оставшиеся части общего плана

### Управляемая активация disposable hot-reload

Для #1221/#1222 используется `tools/release/worker-grant-transition.mjs`.
Оператор SRE с уже выданным доступом запускает его из точного согласованного
checkout на host. QA и developer credentials этот доступ не получают.
Это отдельная security-фаза, а не обычный application release. Скрипт не
меняет source/images, signer keys, поколения, shared ConfigMaps, schema и
replay history. Он не предназначен для production. Обычная установка продукта
по-прежнему использует immutable application images; ограничение этого
одноразового dev-перехода не изменяет install contract.

Read-only `inspect` проверяет всех CP Pods через владельца Deployment/ReplicaSet,
готовность завершённого rollout, чистый source mount каждого reader и writer,
наличие совместимого изменения `29652a817cc548282f03747da3ea98717f9e32af`.
В этом профиле проверка source/Pod не является криптографической аттестацией
бинаря. Отдельно нужны фактические защищённые RPC и restart recovery.
Для image-only reader этот инструмент закрыто отказывает: требуется отдельный
проверенный capability manifest, а не предположение по общему Git SHA.

`worker-grant-readback.sql` — фиксированное чтение в `READ ONLY` transaction
с `statement_timeout=10s` через существующий SRE local PostgreSQL entrypoint.
Один SELECT читает generation floors и instance watermarks из одного snapshot.
Результат содержит только workload, Pod UID, числовые revisions/generations и
timestamps; не содержит токены, подписи, payload или пользовательские данные.
SQL запускается только для этого Issue/runbook; произвольные SQL не добавляются
в параметры. Локальный disposable PostgreSQL harness выполняет тот же файл.

| Переход | Инициатор и полномочия | Предусловие / эффект | Результат и восстановление |
| --- | --- | --- | --- |
| inspect / plan | SRE, выданный exact kubecontext | CP readers совместимы, additive schema доступна, writer source известен | Только private metadata, без domain event; authoritative read — Deployment + PostgreSQL |
| activate | SRE → Kubernetes JSON Patch, UID/resourceVersion CAS | Один здоровый v1 Pod, Recreate; каждому writer назначается Downward API Pod UID | Новый Pod выпускает v2; одна временная Recreate-пауза должна быть отражена в HTTP/task evidence |
| overlap | Тот же SRE CAS | v2 уже включён; один здоровый Pod → две реплики без изменения template | Проверить рабочие RPC обеих реплик и durable registration; Ready сам по себе недостаточен |
| rolling | Тот же SRE CAS | Два одинаковых Pod UID в наблюдении 95s; оба durable watermark растут, поколение неизменно, оба grants не истекли | Только стратегия RollingUpdate/maxUnavailable0/maxSurge1; отсутствие активности закрыто блокирует переход |
| settle | Тот же SRE CAS | Две здоровые v2 реплики с RollingUpdate | Возврат к одной реплике; grants/revocation/history не откатываются |
| CAS drift / потеря ACK / timeout | SRE, без автоматического повтора | План или actual readback не совпал / исход PATCH неизвестен | Сохранить INTENT/PATCH_ATTEMPT/APPLIED и terminal evidence; read-only inspect, новый план только после выяснения исхода |

У этих SRE-переходов нет domain event. Kubernetes сохраняет собственный
resourceVersion; private fsync journal сохраняет intent до PATCH и точный
readback после него. Readiness/обычные RPC продолжают использовать
канонические authority checks; запросов с изготовленным grant нет.
Переход не доказывает LKG/outage/rotation/freshness, task loss или отсутствие
duplicate effects: эти сценарии остаются отдельной приёмкой #1222/#1223.
После активации старый CP без v2 reader не является допустимым rollback.

Команды выполняются по одному target. Для каждой фазы — новые файлы, без
перезаписи предыдущих FAIL/UNKNOWN. Значения context/target задаются по preflight.

```bash
node tools/release/worker-grant-transition.mjs inspect \
  --context "$KODEX_RELEASE_CONTEXT" --target runtime-controller \
  --output "$PRIVATE_INSPECTION"
node tools/release/worker-grant-transition.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" --target runtime-controller \
  --phase activate --output "$PRIVATE_PLAN"
node tools/release/worker-grant-transition.mjs apply \
  --context "$KODEX_RELEASE_CONTEXT" --target runtime-controller \
  --plan "$PRIVATE_PLAN" --evidence "$PRIVATE_JOURNAL" \
  --confirm TRANSITION-STAGING-WORKER-GRANTS
```

Затем последовательно `overlap`, `rolling`, `settle` с собственными plan/journal
и подтверждённым исходом предыдущей фазы. Перед Recreate оператор проверяет
активные tasks/leases и выполняет разрешённый lifecycle drain; этот инструмент
не завершает пользовательские задачи и не считает отсутствие Pod отсутствием
фоновой работы. Не запускать глобальный `up`: старый dev renderer всё ещё
назначает Recreate и требует отдельного согласования с активированным v2.

Локальные проверки:

```bash
node --test tools/release/scoped-release.test.mjs tools/release/worker-grant-transition.test.mjs
bash scripts/tests/control-plane-postgres-test.sh '^(TestBootstrapComponent|TestWorkerGrantInstancesComponent)$'
```

Context7: `/kubernetes/website` (Downward API, Deployment, JSON Patch CAS),
`/websites/postgresql_17` (read-only transaction и snapshot одного SELECT).

### Остаток приёмки

- Стабильный sidecar digest и поэтапный trust publish/readback/switch/drain/revoke.
- Last-known-good в пределах срока/отзыва, atomic adoption, bounded reconnect.
- Scoped immutable release plan, application-only apply/rollback, batch budget.
- API/events/schema expand/contract и запрет разрушительного cleanup в обычном rollout.
- Проверки доступности запросов, сохранения задач и отсутствия повторных effects.

Context7: `/kubernetes/website` (Deployment/PDB), `/websites/postgresql_17`
(ON CONFLICT и блокировки транзакций). Значения секретов не документируются.
