---
id: OPS-DOC-2145-CAPTURE-RETENTION
title: Сохранённый Kubernetes OUTPUT — discovery и восстановление cleanup
type: operation
status: approved
owner: developer
version: 1.0.7
updated: 2026-11-21
---

# Сохранённый Kubernetes OUTPUT — discovery и восстановление cleanup

Источник — [#2145](https://github.com/codex-k8s/kodex/issues/2145),
`DOM-MC-008`, `ARCH-MC-008` и согласованный capture lifecycle в
`GUIDE-DOC-006`. Документ задаёт оставшиеся переходы внутри существующих
capture, stream, prepared content и artifact aggregates. Это не заявление
готовности реализации, portable047, authority108 или установленного окружения.

## Подтверждённые исходные границы

- `GetManagedKubernetesCaptureStatus` разрешает original root actor с текущим
  `run.view` без native credentials и `integration.view`. До первой revision
  history List использует тот же original-root rule независимо от Tool/integration.view.
- INPUT/OUTPUT cleanup после трёх неудач не получает новую claim. Payload
  продолжает учитываться физической staging-квотой; потеря объекта не считается
  доказанным удалением.
- Canonical artifact purge уже переводит head в `PURGED` перед удалением
  revisions. Capture receipt сохраняет opaque refs и commitment; nullable
  `revision_id` снимается через существующий FK только при terminal head.
  Удалять provenance ради освобождения квоты нельзя.
- Новая попытка публикации и новая попытка удаления — разные переходы.
  Восстановление cleanup не делает capture `READY` и не возобновляет native
  execution, input claim, output credit, Run либо owner Gate.

## Сквозная матрица

| Сценарий                     | Инициатор и authority                                                                                                      | Endpoint / owner                                                                                                  | Версия и повтор                                                                                                                 | Переход и потребитель                                                                                                                                                   |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Capture history discovery    | Проверенный исходный root actor; exact run/node/project; текущий `run.view`                                                | Новый paginated List в существующем `ManagedKubernetesCaptureService`, CAG и отдельная проекция RunSessionDetails | Page содержит opaque cursor, не authority; каждый результат проходит правило exact Get                                          | Read-only; outbox event отсутствует, List/Get — авторитетное чтение                                                                                                     |
| Cleanup status               | Тот же original root и текущий `run.view`; graph назначает stream и исходный invocation                                    | CleanupStatus в existing capture Get/List                                                                         | Version считает только explicit cleanup repair cycle, не independent worker progress; без object key/version/ETag/fence/payload | Только safe counters, причина и version; не permission на удаление                                                                                                      |
| Явное восстановление cleanup | Тот же root, свежие `run.view` и canonical `artifact.upload`; первое конечное allowance сохраняется через все waits/COMMIT | Purpose-specific Repair staging cleanup, exact capture и Mutation; stream назначает owner                         | Ownership до OCC/idem; exact actor/operation/key; unknown ACK — один metadata Get, без повторного удаления                      | Новый bounded цикл только exhausted eligible immutable receipts; общий attempt counter и exact object tuple сохраняются; audit + immutable receipt, без execution event |
| Worker claim                 | Существующий artifact-retention SQL consumer, session-user scope и отдельный cleanup fence                                 | `kubernetes_stage_cleanup_claim`                                                                                  | Новое owner generation; bounded lease/batch; нет native authority                                                               | Exact eligible INPUT/OUTPUT/prepared/FILE source, затем существующий prepared deletion processor                                                                        |
| Unknown object write         | Тот же cleanup claim                                                                                                       | Существующий exact-version object-storage Head/readback                                                           | Нет повторного PUT; отсутствие не доказывает успешное удаление                                                                  | Положительный immutable receipt до Delete; неизвестность сохраняет quota                                                                                                |
| Confirmed cleanup            | Текущий cleanup owner/generation и exact version/ETag                                                                      | Существующая owner completion function                                                                            | Late либо foreign completion не меняет row; повтор не создаёт второе действие                                                   | Только подтверждённое удаление ставит payload-purged receipt и освобождает физическую quota; frame/ACK/source commitments сохраняются                                   |
| Restore canonical artifact   | Текущая canonical `artifact.restore` authority и 30-day retention                                                          | Существующий artifact lifecycle                                                                                   | Exact artifact/version/idem                                                                                                     | Старый personal/service provenance сохраняется; native execution и bindings автоматически не возобновляются                                                             |
| Manual/job artifact purge    | Canonical artifact purge либо retention claim; active pinned readers блокируют purge                                       | Существующие manual/job consumers                                                                                 | Exact object versions удаляются до terminal metadata; source receipts не расширяют доступ                                       | `PURGED` до revision cleanup; capture commitment сохраняется; обычные artifact audit/events не заменяются новым engine                                                  |
| Project purge                | Существующий authoritatively resolved project purge context                                                                | Canonical database purge и object inventory                                                                       | Полный graph в одной owner transaction; stale writer/capture tasks предварительно закрыты                                       | Удаление FK-зависимостей в правильном порядке; никакой произвольный global prune; чужие project/actor objects не входят в inventory                                     |

Первое конечное allowance нельзя заменить более поздним бессрочным grant.
Проверка повторяется после блокирующих locks и до/после COMMIT. Истёкший
read/repair не выдаёт ни stale metadata, ни successful receipt.

## Физическая quota и неизменность

Quota считает только ещё не подтверждённо удалённые payload reservations.
Metadata receipts и общие attempt counters не обнуляются при repair.
Ограничение batch означает частичный **явно отражённый** результат: receipt
сообщает число восстановленных и оставшихся exhausted items. Следующий batch
требует нового осознанного действия с новой Mutation.

Cleanup проверяет eligibility заново под существующим stream lock. Публикация
истории не зависит от нового native SSAR. Однако неподтверждённый OUTPUT tail,
ещё нужный capture source и FILE source с возможным INPUT consumer удалять
нельзя. FILE использует тот же cleanup consumer и quota graph, а не отдельную
очередь, transport либо самостоятельный scavenger.

## Файлы и проверка реализации

- Public capture Proto/DTO/service/client/CAG и PWA history/status/repair —
  расширение имеющегося capture service.
- Owner SQL и guards — текущая development047; INPUT/OUTPUT/prepared cleanup
  metadata и existing functions, с согласованным FILE source hook.
- Purge — canonical artifact/manual/job/project inventory/functions; полный
  список FK/retention объектов, включая PF bindings и loss facts.
- Disposable PostgreSQL должен доказать exhausted→repair→exact deletion→quota
  recovery, immutable attempt history, late completion, actor/project denial,
  finite ACL после lock/COMMIT и restore/purge provenance.
- Synthetic API/browser должны доказать history discovery после отказа Tool
  по `integration.view`, один readback после unknown repair ACK и скрытие
  stale metadata после утраты `run.view`.
- Максимальный pipeline case — только отдельный собственный disposable
  AV/S3 fixture, exact256MiB incompressible source, bounded disk/RSS и cleanup.
  Spool-only измерение не является этой проверкой; `NOT RUN` не становится
  `PASS` из-за успешной малой публикации.

`WAITING_OWNER` capture сохраняет ещё нужный prepared intent для явного
восстановления публикации. Cleanup prepared допускается только для
`COMPLETE`, `REJECTED` либо `SUPERSEDED`, после fencing writer и завершения
active attempts. UNKNOWN provider receipt сначала сверяется по прежнему key,
size и digest; новый PUT и отсутствие объекта не подменяют доказанное удаление.

## Доставка обработчика захвата

Защищённый профиль доставляет issuer `artifact-retention` с отдельной
идентичностью SPIFFE, application grant v2 с UID Pod и собственным LOGIN
`ira_artifact_retention_issuer_g1`. Миграция authority
`20261115000200_artifact_retention_issuer.sql` регистрирует поколение 1.
Канонический bootstrap применяет миграции перед сверкой runtime credentials,
доставляет материал через registry revision 9 и ждёт readback issuer.
Control plane проверяет worker grant по выделенному открытому ключу. В профиле
локальной разработки `trusted-cluster` действуют явная регистрация worker RPC
и двусторонняя сетевая граница обработчик → control plane.

Для обновления действующей установки требуется поэтапный release plan,
который сохраняет текущие ключи и БД: подтвердить предшественника authority,
применить прямую миграцию, доставить новое поколение материала и сверить
readiness обоих участников перед запуском capture worker. Проверка render
покрывает маршрут и проекции, проверка PostgreSQL — допуск нового LOGIN к
своему workload и отказы на чужих ресурсах. Установочная приёмка дополнительно
требует реального claim, обработки и подтверждённого результата.

## Project purge: сохранение достижимого reconciliation

До Trash и явного Purge project owner удерживает project lock, блокирует все
исходные roots проекта и отдельным свежим statement проверяет UNKNOWN/незавершённые
physical object intents. При отсутствии exact provider version/ETag переход
отклоняется с `KUBERNETES_STAGING_RECONCILIATION_REQUIRED`: project lifecycle,
run grants, task leases и события не меняются. Это сохраняет original-root
`run.view` и прежний capture/FILE metadata→cleanup repair path. Повторный native
Execute и новый PUT не являются способом снять блокировку.

| Переход                 | Actor/authority и owner                                                    | Intent/version и потребитель                                                                                             | Итог и readback                                                                                                                                |
| ----------------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Trash/Purge при UNKNOWN | Existing organization project owner; project/root mutex, затем owner count | Прежняя project Mutation; отсутствие события при отказе                                                                  | ACTIVE либо TRASHED сохраняется; existing ProjectPurgeRecovery возвращает BLOCKED/reason11/count без locators; original root продолжает repair |
| Automatic due           | Existing purge worker, exact tenant/project                                | Bounded candidates lock → fresh root/predicate → прежний promotion                                                       | UNKNOWN не переводится в PURGE_PENDING, события и purge receipt не создаются                                                                   |
| Поздний prepared intent | Existing capture/FILE purpose snapshot                                     | Exact ACTIVE project FOR SHARE до durable insert; отдельный writer держит project→prepared lock через single PUT/outcome | Trash, победивший первым, запрещает новый PUT; прежний ledger не удаляется                                                                     |
| Known object purge      | Existing canonical project inventory/physical delete proof                 | Exact immutable versions, затем авторизованный FK graph                                                                  | Сохраняются tenant/provenance guards; UNKNOWN нельзя превратить в ABSENT либо generic abandon                                                  |
| Artifact restore/purge  | Existing artifact lifecycle и исходный provenance                          | Exact impact/version/idem; после PURGED terminal head остаётся свидетелем capture                                        | Restore не расширяет actor visibility; удаление revision content не оставляет принятые stage payload навсегда занятыми                         |

Public recovery reason — объяснение отказа, а не новое полномочие на object
cleanup или публикацию. Administrator получает только безопасный count;
исходного пользователя нельзя подменить administrator/Gate resolver.

Эта секция описывает целевой source path текущего purge-среза. Native overlap,
полный FILE/FK inventory и фактический purge/restore ещё требуют отдельной
квалификации; прежний df4 cleanup PASS не является их доказательством.

Known prepared receipt не требует обязательной публикации перед удалением
проекта. Existing canonical inventory включает immutable INPUT/OUTPUT/prepared
versions, а purge consumer сверяет полный owner receipt (`key/version/ETag/digest/size`)
через тот же owner rejoin. Префикс storage key не является authority.
`OBJECTS_CLEARED` + exact inventory digest + существующие project/recovery context
разрешают terminal scrub known, fenced prepared; общий attempt history не меняется.
Generic owner-abandonment SYSTEM/PROJECT не получает KUBE intents.

Trash/explicit Purge/automatic due в той же owner transaction закрывают active
capture attempts и сохраняют непубликовавшуюся capture как `WAITING_OWNER`.
Завершённая история immutable. Late ACK в закрытом проекте сохраняет новую
capture generation также без runnable publication lease. После Restore original
root может явно восстановить capture; ни native execution, ни старый lease
не возобновляются. `PROJECT_CHANGED` и защищённые project/capture reads —
авторитетный event/readback path этого перехода.

FILE physical receipt принадлежит одному normalized source. Его INPUT frames
не являются отдельными physical objects и не должны повторно удаляться либо
считаться в physical quota. Typed FILE origin/adoption release и финальный
FK fingerprint фиксируются на общей development047/048 после frozen FILE schema;
эта зависимость пока не считается закрытой данным source checkpoint.

## Ручной artifact purge и существующая retention custody

Ручной purge больше не обходит generation/owner существующего retention claim.
Перед первым внешним действием owner сохраняет impact/version/idempotency intent
и фактические owner/generation/deadline. Pending receipt не истекает через24часа;
его FK удерживается головой артефакта. В receipt хранится неизменяемый inventory
и monotonic map уже допущенных revision ID. Ключ attempt не содержит нового
claim generation: смена worker не разрешает повторить прежний Delete.

| Сценарий                             | Полномочия и переход                                                                               | Внешнее действие                                        | Авторитетный итог                                                           |
| ------------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------- |
| Новый ручной purge                   | Исходный actor, canonical artifact.purge, exact impact/OCC; first/fresh Until после waits и COMMIT | Durable admission → optional DeleteOnce точной версии   | Head той же версии подтверждает отсутствие перед scrub                      |
| Потерянный DELETE/COMMIT ACK         | Тот же immutable intent и version                                                                  | Повтор только Head; fallback на обычный Delete запрещён | PURGE_PENDING сохраняется до подтверждённого отсутствия                     |
| Истёкший public claim                | Старый owner/generation не продлевается                                                            | Public caller не получает новый Delete                  | Existing retention consumer получает fresh bounded custody того же intent   |
| Worker, версия уже admitted          | SYSTEM cleanup actor, current fenced claim                                                         | Только Head                                             | При наличии объекта история остаётся pending; отсутствие разрешает finalize |
| Worker, следующая NOT_STARTED версия | SYSTEM cleanup actor, новая bounded attempt прежнего intent                                        | Ровно один DeleteOnce после durable admission           | Отдельная версия не наследует право повторить предыдущую                    |
| Terminal purge                       | Все content receipts удалены после absence; actual generation>0 и closed owner/deadline            | Новых действий нет                                      | Prepared scrub и безопасный receipt PURGED + attempt count, без locators    |

Scheduled retention и avatar cleanup сохраняют прежний обычный Delete/retry.
Manual reconciliation записывает отдельную причину аудита; SYSTEM не подменяет
original actor в receipt. Старый worker не может присвоить manual claim прямым
UPDATE. Startup attests optional one-attempt transport и owner claim/complete
functions до потребления новых admissions.

Это source-контракт текущего изменения. SDK local HTTP RED/GREEN подтверждён
на c40e651; CP manual custody, worker SQL и native repair/expiry ещё не прошли
приёмку. Полный project FK graph, FILE release и реальный AV/S3 сценарий256Mi
остаются отдельными открытыми границами.

Manual custody также блокирует Trash/Purge/automatic due проекта до terminal
reconciliation. Reservation берёт project FOR SHARE раньше idempotency/head locks;
project owner держит FOR UPDATE и читает свежий pending count. Это запрещает
canonical project worker повторно вызвать Delete после manual UNKNOWN.
Existing recovery.reason11 возвращает `ARTIFACT_PURGE_RECONCILIATION_REQUIRED`
и безопасный count; прежний project state/eligibility сохраняется, generic
abandonment запрещён. После project purge безопасный terminal receipt получает
обычный24часовой срок вместо удержания удалённой головой артефакта.


## Общий FILE cleanup consumer

FILE проходит существующий Kubernetes cleanup loop и тот же девятипольный claim,
без отдельной очереди. `source_kind=KUBERNETES_FILE_SOURCE` выбирает typed owner
functions. Общий prepared collector допускает только SYSTEM/PROJECT; FILE INPUT
metadata исключены из stage claim/status/repair и physical quota. Единственный
физический объект принадлежит FILE source, а его receipt атомарно закрывает
metadata frames и typed prepared edge.

| Сценарий | Owner admission и внешний эффект | Итог / read path |
| --- | --- | --- |
| Первый claim с известным receipt | Durable admission внутри claim; `uncertain=false`; точный Head и один DeleteOnce | Exact Head absence → source/metadata/prepared receipt |
| Первый неизвестный PUT | `uncertain=true`, версия пуста; положительный Head → Record нового admission; только подтверждённый Record допускает один DeleteOnce | Head404 не доказывает отсутствие; потерянный Record ACK не разрешает Delete |
| Уже допущенный Delete, включая repair | `uncertain=true`, версия известна; только Head, capability Delete не требуется | Точный absence закрывает source; присутствие/неопределённость сохраняет unresolved историю |
| Истечение во время claim/Head | Монотонный deadline от начала claim ограничивает исходный lease; после IO проверяется тот же context | Delete не вызывается; следующий claim не сбрасывает admission |
| Нет object intent | Typed source функция под exact mutex доказывает fenced writer и отсутствие unique prepared intent | NO_OBJECT_INTENT receipt без внешнего Delete |
| Project Trash/Purge | Любой непогашенный FILE source блокирует переход до отмены grants/состояния | Original project остаётся доступен для Stop и cleanup/reconciliation; generic inventory не выдаёт FILE Delete |
| Original artifact revision | Canonical artifact retention учитывает FILE origin FK до внешнего Delete | Release является отдельным terminal owner переходом; этот checkpoint его не реализует |

Startup artifact-retention проверяет общий dispatcher, typed FILE функции и
закрытый allowlist generic collector. Это проверка доступности кода и прав,
а не доказательство native FILE, реального S3/AV или готовности FILE5. Local
processor unit, disposable owner component и итоговый schema capture фиксируются
отдельно на точном SHA. Полный origin release/purge/restore graph остаётся OPEN.


Для нового FILE/manual reconciliation generic `Head` → `ErrNotFound` не является
доказательством отсутствия версии. Optional `VersionAbsenceVerifier` допускается
только для квалифицированной target/authentication boundary; default закрыт.
Локальный VersionedStore проверяет свою полную version map под lock и доказывает
только disposable fixture. S3-адаптер этот optional port пока не реализует:
после неизвестного Delete404/403 остаются UNKNOWN, новый Delete не выдаётся.
Отдельный успешный ответ текущего exact DeleteOnce является положительной
квитанцией самого действия; прежние scheduled consumers не меняют свой контракт.
Это не требует ListBucket, нового S3 permission либо provider query. Проверены
официальные AWS HeadObject (generic error/status semantics) и PostgreSQL18
RETURN QUERY/ROW_COUNT/session_user через Context7. Actual S3 recovery пока NOT RUN.


Public `make test-managed-kubernetes-file-postgres` и
`make test-managed-kubernetes-capture-postgres` используют существующий exact
current-source049 development seed. Bootstrap сверяет весь shipped package set,
включая SSH_TARGET: isolated047 не содержит его adapter constraint и закрыто
отклоняет reconcile definition с SQLSTATE23514 до FILE. Старый
`TestManagedKubernetesOwnerRegressionSeed` сохранён как отдельный047 schema-only
fixture. Подмена seed не ослабляет production validation, не расширяет portable
release selector и не считается доказательством backup/restore всей схемы.


Terminal FILE origin release выполняет прежний common cleanup claim до выбора
новых объектов. Private owner function блокирует invocation, все его sources,
затем origin; только terminal invocation, fenced writers, подтверждённые deletion
receipts и отсутствие живого INPUT допускают обнуление exact revision FK.
SourceComplete эту функцию не вызывает. Opaque refs, digest, catalog/RR pins,
cleanup counters и remote outcome сохраняются; событие не создаётся, authoritative
read path — FILE metadata и canonical artifact impact. Public runtime не может
сам вызвать private dispatcher. Смена claim либо repair не заменяет delete admission.

Canonical project purge размыкает typed prepared FILE edge лишь после source
receipt и origin release, под прежними OBJECTS_CLEARED + SCRUB context. Сохранённый
prepared intent становится CLEANED без project/source FK; его opaque provenance
и deletion receipt не переписываются. Graph guard обновляется отдельно на каждом
реальном047/048/049 переходе по actual normal8bf catalog (134/369 →137/373 →139/376),
проверяя прежний owner function digest. Future graph variants не принимаются.
Сначала удаляются FILE admissions и bindings/loss, затем source/origin; SSH receipt
children также предшествуют своим parent. Это source-контракт: новые guards,
terminal release и весь restore/purge graph ещё требуют actual DDL/component
проверки и повторного normal/backup/table-only capture на итоговом SHA.
