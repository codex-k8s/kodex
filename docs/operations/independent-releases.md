---
id: OPS-DOC-INDEPENDENT-RELEASES-001
title: Независимые релизы и переход worker grants
type: operations
status: approved
owner: manager
version: 1.3.0
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
готовность завершённого rollout, чистый source mount каждого reader и source writer,
наличие совместимого изменения `29652a817cc548282f03747da3ea98717f9e32af`.
В этом профиле проверка source/Pod не является криптографической аттестацией
бинаря. Отдельно нужны фактические защищённые RPC и restart recovery.
Для image-only reader этот инструмент закрыто отказывает: требуется отдельный
проверенный capability manifest, а не предположение по общему Git SHA.

Исключение #1304 относится только к image writer `platform-worker-grant-agent`
в `role-image-builder`. Закрытый repo-owned
`tools/release/role-image-builder-writer-capability.json` фиксирует единственный
image digest, canonical source revision и Git trees семи модулей, Go build recipe
и SHA256 executable. Нет CLI для подстановки произвольного manifest или доверия
аннотации. Reader CP остаётся на прежнем source workflow; image-only CP отклоняется.

SRE запускает существующий CLI на host с root-доступом к CRI и `/proc`.
`k3s crictl inspect` читается через pipe без вывода raw JSON. Проверяются exact
Pod UID/name/namespace, container name/ID/imageID, CRI state/restart attempt/PID
и единственный ожидаемый executable argument. `/proc/PID/exe` проверяется по
пути и SHA256; start ticks, inode/device и повторный CRI readback защищают от
замены процесса во время чтения. В evidence попадает только закрытый набор
identity/digest, без env/cmdline/секретов.

Каждый inspect/plan/apply заново проверяет actual writers. План закрепляет
capability digest и process identity; apply закрыто отклоняет drift. После
95s observation непосредственно перед единственным PATCH выполняется повторное
чтение. После rollout/drain все новые writer Pods снова проверяются перед PASS.
Чужой target, tag, неизвестный image/hash/command и restart требуют readback и
нового плана; автоматического fallback или повторного PATCH нет. Схема,
generation floor, lifecycle и порядок четырёх фаз сохраняются.

Источник canonical binary — `6fe0412154b09d37ec654f1dc394e5ca6de48a20`;
прикладные исходники authority и шести локальных зависимостей идентичны checked
main на момент #1304. Локальная Go1.26.6 linux/amd64/GOAMD64=v1 pure-Go сборка
по manifest recipe дала `39d06542b3dce25b294498e8b428d3969ee6de170e39e0dd9f4d2e0527149570`.
Root зафиксировал такой же hash actual `/proc/PID/exe` для image из manifest.
Это доказательство совпадения writer bytes с воспроизводимой сборкой, а не
новая общая OCI attestation или проверка всех binaries этого image.
Live activation/две durable streams/outage/rotation остаются NOT RUN для
исполнителя #1304 и проверяются root после exact-head merge.

`worker-grant-readback.sql` — фиксированное чтение в `READ ONLY` transaction
с `statement_timeout=10s` через существующий SRE local PostgreSQL entrypoint.
Один SELECT читает generation floors, instance watermarks и количества активных
runs/claimed runtime leases из одного snapshot.
Результат содержит только workload, Pod UID, числовые revisions/generations и
timestamps; не содержит токены, подписи, payload или пользовательские данные.
SQL запускается только для этого Issue/runbook; произвольные SQL не добавляются
в параметры. Локальный disposable PostgreSQL harness выполняет тот же файл.

| Переход | Инициатор и полномочия | Предусловие / эффект | Результат и восстановление |
| --- | --- | --- | --- |
| inspect / plan | SRE, выданный exact kubecontext | CP readers совместимы, additive schema доступна, writer source известен | Только private metadata, без domain event; authoritative read — Deployment + PostgreSQL |
| activate | SRE → Kubernetes JSON Patch, UID/resourceVersion CAS | Один здоровый v1 Pod, Recreate; каждому writer назначается Downward API Pod UID | Новый Pod выпускает v2; одна временная Recreate-пауза должна быть отражена в HTTP/task evidence |
| overlap | Тот же SRE CAS | v2 уже включён; один здоровый Pod → две реплики без изменения template | Проверить рабочие RPC обеих реплик и durable registration; Ready сам по себе недостаточен |
| rolling | Тот же SRE CAS | Два неизменных Pod UID: 95s продвижения обоих durable watermark либо отдельное свежее A→B→A доказательство для idle runtime-controller; поколение неизменно, grants не истекли | Только стратегия RollingUpdate/maxUnavailable0/maxSurge1; отсутствие доказательства закрыто блокирует переход |
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

После `APPLIED` и штатного rollout инструмент дополнительно ждёт удаления
прежних terminating Pods до точного healthy inventory (#1297). Это ограниченное
read-only ожидание до 300 секунд с записью `WAITING_FOR_POD_DRAIN`; повторного
PATCH нет. UID/spec drift и ошибки транспорта завершают операцию отказом.
Истечение ожидания сохраняет FAIL и требует authoritative readback: наличие
применённой strategy или одной Ready replica само по себе не заменяет окончание
drain. Старые FAIL не переписываются последующим успешным inspect.

Локальные проверки:

```bash
node --test tools/release/scoped-release.test.mjs tools/release/worker-grant-transition.test.mjs
bash scripts/tests/control-plane-postgres-test.sh '^(TestBootstrapComponent|TestWorkerGrantInstancesComponent)$'
```

Context7: `/kubernetes/website` (Downward API, Deployment, JSON Patch CAS),
`/websites/postgresql_17` (read-only transaction и snapshot одного SELECT).

#### Адресное доказательство CP и передача лидерства

Для CP `rolling` вызывает существующий mTLS
`AuthorityProofResolverService/CheckReadiness` на каждом точном Pod до и после
95s. Этот контракт проверяет собственный подписанный grant reader и durable
owner state; он не является business command и не принимает изготовленный
bearer. Инструмент сопоставляет Kubernetes containerID/Pod UID с CRI PID,
читает CA/certificate/key через mount namespace этого контейнера и проверяет
точный server hostname и SPIFFE client identity. Ключ остаётся в памяти SRE
процесса, очищается после вызова и не передаётся в argv, QA либо artifacts.
Проверка из host network не доказывает DNS/NetworkPolicy рабочего клиента.
Журнал сохраняет попытку RPC до вызова и metadata после; ошибка до PATCH —
FAIL, неизвестный исход PATCH — UNKNOWN. Повтор старого плана запрещён.

Runtime-controller вызывает рабочие RPC только как leader, поэтому обычное
ожидание двух advancing rows не проверяет standby. Для #1254 отдельный
`runtime-leader-handoff.mjs` допускает только idle профиль: нет активных runs,
claimed leases и незавершённых Jobs. Он фиксирует lease holder, Deployment,
два Pod UID, application и native sidecar restart counts и generation floor.
Перед каждым сигналом повторяется preflight; свежая работа закрыто блокирует
продолжение. Штатный SIGTERM получает PID1 Air только текущего leader с
проверкой Downward API UID и пути бинаря. Kubernetes восстанавливает тот же
application container; grant agents, leases и файлы вручную не изменяются.

| Переход | Авторитетный результат | Ошибка / отсутствие события |
| --- | --- | --- |
| A→B | Обычный drain/release Kubernetes Lease; B выполняет защищённый RPC и регистрирует собственный durable instance | Потеря exec ACK не повторяет сигнал; readback того же Pod до 240s |
| B→A | A вновь становится leader, его revision продвигается после второго handoff | Новая работа, замена Pod/reader, sidecar restart или floor drift закрыто останавливают proof |
| Proof→rolling | PASS моложе 300s, тот же cluster/Deployment/spec/Pods/processes и A leader; hash proof закреплён в plan | Новый activity/lease, stale proof или CAS drift не разрешают PATCH |

Отдельного domain event нет: authoritative read — Kubernetes Lease/Pod и
PostgreSQL watermarks. Fsync journal хранит SIGNAL_INTENT, неопределённый ACK
и readback; повторного сигнала после timeout нет. Это доказательство idle
restart recovery, не сохранения активной задачи/внешнего эффекта. Такие
сценарии по-прежнему обязательны в #1223.

```bash
node tools/release/runtime-leader-handoff.mjs \
  --context "$KODEX_RELEASE_CONTEXT" --output "$PRIVATE_HANDOFF_PROOF" \
  --evidence "$PRIVATE_HANDOFF_JOURNAL" \
  --confirm OBSERVE-IDLE-RUNTIME-LEADER-HANDOFF
node tools/release/worker-grant-transition.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" --target runtime-controller --phase rolling \
  --handoff-proof "$PRIVATE_HANDOFF_PROOF" --output "$PRIVATE_PLAN"
node tools/release/worker-grant-transition.mjs apply \
  --context "$KODEX_RELEASE_CONTEXT" --target runtime-controller \
  --handoff-proof "$PRIVATE_HANDOFF_PROOF" --plan "$PRIVATE_PLAN" \
  --evidence "$PRIVATE_JOURNAL" --confirm TRANSITION-STAGING-WORKER-GRANTS
```

Air runtime-controller получает `kill_delay=230s`: 210s application drain и
20s cleanup до 240s Pod grace. Остальные Air процессы сохраняют 90s. Новая
настройка применяется при следующем штатном application rollout; изменение
исходников SRE инструмента само по себе не меняет запущенный supervisor.

HTTP monitor принимает proxy cookie rotation также из HTML/session GET через
общую очередь owner-session-client. Redirect не выполняется, каждый non-200
сохраняется как FAIL; refresh не продлевает absolute expiry и не повторяется
после неопределённого ответа. Это исправление оснастки #1256 не превращает
предыдущие live 503/401 в PASS и требует нового естественного окна.

Дополнительные локальные проверки:

```bash
node --test tools/release/control-plane-reader-probe.test.mjs tools/release/runtime-leader-handoff.test.mjs tools/release/go-shutdown-budget.test.mjs
node --test tools/dev/owner-session-client.test.mjs tools/dev/release-http-acceptance.test.mjs
```

Context7: `/websites/nodejs_latest-v24_x_api` (HTTP2, TLS, trailers),
`/air-verse/air` (interrupt и kill_delay), `/kubernetes/website` (graceful termination).

### Остаток приёмки

- Стабильный sidecar digest и поэтапный trust publish/readback/switch/drain/revoke.
- Last-known-good в пределах срока/отзыва, atomic adoption, bounded reconnect.
- Scoped immutable release plan, application-only apply/rollback, batch budget.
- API/events/schema expand/contract и запрет разрушительного cleanup в обычном rollout.
- Проверки доступности запросов, сохранения задач и отсутствия повторных effects.

Context7: `/kubernetes/website` (Deployment/PDB), `/websites/postgresql_17`
(ON CONFLICT и блокировки транзакций). Значения секретов не документируются.
