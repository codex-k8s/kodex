---
id: OPS-DOC-RUNTIME-WORKSPACE-1953
title: Устойчивое рабочее состояние сотрудника
type: operation-contract
status: approved
owner: developer
version: 1.0.5
updated: 2026-10-27
---

# Граница профиля

[#1953](https://github.com/codex-k8s/kodex/issues/1953) задаёт сохранение
неопубликованного рабочего дерева при остановке сотрудника. История модели,
устойчивый work и принятые CLEAN-артефакты имеют разные контракты. Сохранение
work не принимает бизнес-результат и не выдаёт новых полномочий.

Выбранный профиль `SESSION_WORKSPACE_V2` повторно использует существующий
session PVC20GiB. Только exact writable generation монтируется как
`/workspace/work`; private parent, другая generation и другие сессии не доступны
через альтернативный путь `.kodex/state`. Кэши и временные результаты сборки
отделены от сохраняемого дерева. Прежние квоты work, количества файлов,
памяти, выполнения и archive tasks не увеличиваются.

Этот документ фиксирует согласованный контракт. Наличие типов, миграции или
локального теста само по себе не подтверждает доставку либо полную готовность.

## Authority и жизненный цикл

| Переход | Авторитетное действие |
| --- | --- |
| Create | CP создаёт пустую work binding exact organization/project/session/agent. Совпадение имён либо latest-by-agent не выбирает прежние файлы |
| Claim/renew | Один current writer через owner CAS и точные execution pins. RWO и наличие Pod не являются single-writer proof |
| Materialize | Только owner-selected generation и exact PVC UID; role видит только свой tree, не корень хранилища |
| Complete | Сначала process Wait, reader drain и отсутствие process group; затем immutable checkpoint и отдельный owner ACK. Business receipt не переписывается |
| Cancel/timeout | Немедленное закрытие execution grants; work остаётся. Graceful Pod deletion сохраняет положительный owned quiescence proof до исчезновения Pod |
| Неизвестный writer | BLOCKED: нет archive/delete/rejoin по одному lease expiry, force-delete либо PodNotFound |
| Technical continuation | Exact server-owned CONTINUES, свежий lease и inputs; без повторения внешнего эффекта |
| Child retry | Actual retry lineage и fresh actor/project/agent eligibility. Новый самостоятельный root не наследует полномочия старого родителя |
| Workflow retry/rework | Exact predecessor/version/step/agent и approved route; новые Sessions получают копию собственного frozen checkpoint, не общий writable tree |
| Replay/late ACK | Прежний immutable owner outcome. Старый writer не меняет current generation и не становится вновь разрешённым |
| Archive/restore | Проверка всех exact-version parts и digest до owner ACK; model не запускается по частично восстановленному дереву |
| Delete/purge | Durable destructive intent исключает новый writer; exact UID/versions, отсутствие consumers и authoritative readback. Unknown блокирует удаление |

Для нового профиля порядок обязателен уже в первом `workspace-prepare`:
read-only decode/validation входа и создание callback transport → exact owned
Pod/PVC readback и owner writer CAS (`BIND`) → любые prepare/reset/copy/write,
включая trusted init, egress trust, справочник и quota canary. Read-only inode
proof из отдельного file subPath разрешает только переход в exact `work`
mount; наличие этого файла не заменяет BIND и не является authority.
Вложенные mounts и альтернативный доступ через `.kodex/state/workspaces`
закрыты; старый профиль не получает нового callback-метода.

`QUIESCENT` после terminal сохраняет только past fact уже известного exact
writer. Storage-only ACK не создаёт lease, grant, RuntimeRevision, новую
generation и не меняет исход business run. Неизвестный либо force-deleted
writer остаётся BLOCKED с сохранённой PVC.

Persistent preparation сохраняет draft и уже изменённые INPUT copies.
Текущие read-only originals всё равно проверяются по metadata и digest;
изменённая копия не входит в proof текущего INPUT. Новый finalized attachment
set получает собственный существующий namespace. Квота заранее учитывает
оставшиеся bytes, все writable directories, новые INPUT parents и временный
справочник; отказ не удаляет сохранённые данные.

## Архив и ограничение ресурсов

Legacy archive v1 сохраняет прежние bytes и reader. Закрытый v2 work manifest
содержит только scope, generation, writer commitments, digest/размер дерева и
ограниченный список immutable data parts. Secret lease fence, provider auth,
input/context и чужие файлы в архив не входят. Отсутствующий Codex session ID
не выдумывается для work, появившегося до запуска модели.

Canonical stream ограничен прежними work byte/file quotas. Частей не больше32,
каждая не больше64MiB, manifest/task не больше64KiB. Путь и overhead одной записи
ограничены; превышение либо неподдержанное дерево закрыто отклоняется с
сохранением PVC. Decode/restore отклоняет неизвестные и повторные поля, лишние
bytes, traversal, links, special files и несовпадение exact object commitments.
Восстановленный work остаётся недоверенным внутри прежнего sandbox и не
обходит quarantine или artifact/VFS eligibility.

V2 snapshot consumer использует только exact read-only work subPath. Main
UID10001 читает собственные mode0600 files, paired read-only reader UID10002
передаёт только exact owned10002 regular-file descriptors через private Unix
socket с kernel peer UID и повторным inode/owner check. Ни root/DAC capability,
ни chmod исходника, ни общий cross-session FD broker не допускаются. Reader не
получает object-storage credentials, API token, provider auth или session root;
прежний общий CPU/RAM budget разделяется между consumers: основной архиватор
224MiB/900m, минимальный `workspace-reader`32MiB/100m. Reader executable
не импортирует Kubernetes controller. Подготовка socket directory выполняется
UID10001 с primary GID29000 и проверкой mode2770, без CAP_FSETID; reader
работает UID10002 без capabilities. Неизвестное владение,
недоступный private directory или изменённый descriptor оставляют PVC в BLOCKED.
Read-only checkpoint verifier проверяет exact root receipt, scope/generation,
все последовательно прочитанные part versions и их digest/размеры. Canonical TAR
повторно кодируется в hash sink: неизвестные владельцы, links, traversal,
метаданные PAX, timestamps, права и лишние bytes отклоняются независимо от
корректности object hashes. Он не создаёт файлов и не даёт restore authority.

Canonical stream сохраняет закрытый logical owner ROLE/PROVIDER в Uname,
а числовые UID/GID обнуляет. Restore сопоставляет ROLE с UID10001, PROVIDER
с UID10002 при общем GID29000 и создаёт файлы только в новой собственной
generation. Произвольные usernames и UID не разрешают смену владельца.
Private generation parents0700 не монтируются snapshot consumers. Legacy v1
consumer сохраняет UID10002. Renderer/native permissions и реальная двух-UID
проверка обязательны; pure codec/scanner unit этого не доказывает.

Private Proto task использует optional field22; legacy fields1..21 и прежние
Task bytes без workspace binding сохраняются. V2 task имеет strict canonical
JSON без duplicate/unknown/null/alias downgrade. Snapshot attempts после первой
имеют только reconciliation credit: неизвестный PUT и последующий NotFound не
разрешают повторную запись или cleanup. Точное существующее object version
может быть подтверждено read-only; unresolved custody сохраняет PVC.

V1 receipt при наличии work не разрешает удаление всей PVC. Перед activation
нужно завершить либо отозвать старые destructive archive jobs и подтвердить
их exact UID readback. После v2 записей восстановление требует совместимого
forward reader; удаление metadata ради старого бинарного rollback запрещено.

### Граница прежнего архиватора

| Операция | Проверка owner | Переход/событие |
| --- | --- | --- |
| Первое создание workspace | Exact session lock и отсутствие READY/CLAIMED SNAPSHOT, RESTORE, DELETE_PVC | Snapshot тоже исключён: его completion назначает DELETE_PVC |
| Выбор legacy task | Workspace отсутствует до LIMIT; task и session блокируются SKIP LOCKED | Lock совпадает с первым созданием workspace; busy session пропускается |
| Legacy claim | Следующий SQL statement с fresh READ COMMITTED snapshot повторяет отсутствие workspace | Отказ не меняет task/storage, lease/grant и event не создаются |
| Renew/complete | Existing lease/fence/generation плюс отсутствие workspace | Чужой профиль не продлевает grant и не подтверждает PVC deletion |
| Expiry/retry/cleanup | Legacy materializer исключает любую session с workspace | Неизвестная прежняя custody сохраняется; expiry не является join proof |
| Legacy object GC | Только format1 и session без workspace | V2 manifest/parts и ссылки не удаляются прежним consumer |

Проверка использует два настоящих PostgreSQL соединения для обоих порядков
session lock. Отдельная смешанная fixture с workspace и legacy CLAIMED task
существует только в откатываемой disposable транзакции: положительный renew
до workspace, отказ после, запрет completion/нового claim, сохранение CLAIMED
после expiry и блокировка project purge. Полный прежний snapshot/restore/GC
проходит в том же сценарии. Проверка не подтверждает физическое завершение
старого Kubernetes Job: для activation нужен отдельный drain/readback.

Private config `SESSION_WORKSPACE_STORAGE_PROFILE` по умолчанию отсутствует
либо пуст: это прежний профиль без назначения work binding. Единственный
opt-in — `SESSION_WORKSPACE_V2`; unknown значение закрыто останавливает startup.
До включения оператор подтверждает dual readers CP/RC/runner/archive worker,
точный допустимый runtime image contract, readiness, schema proof и drain
старых destructive jobs. После назначения V2 ошибки не дают silent fallback
к legacy и не сбрасывают work. Отзыв opt-in не разрешает старому reader
обрабатывать уже созданные V2 metadata либо удалять соответствующую PVC.

Backup фиксирует точное поколение подтверждённого checkpoint со всеми parts.
Активное неподтверждённое дерево не выдаётся за полный backup исходников.
Restore не возвращает старые leases, credentials или RuntimeRevision authority.
Старый work на уже удалённом emptyDir без receipt остаётся отсутствующим либо
неизвестным, а rollout не переименовывается в восстановленный исходник.

Forward migration `20261026001500` располагается после полного workflow014;
применённые миграции не меняются. Catalog/purge proof и rollout относятся к
точному отдельному checkpoint, без allow-missing и без обхода остановленных
native-путей других задач. Проверки operator, native, backup/restore и реальная
приёмка сотрудником обязательны до заявления готовности.

Поставка приложения требует предварительно подтверждённой schema016 даже
при выключенном opt-in: claim query читает owner storage eligibility до LIMIT.
Default legacy означает отсутствие новых bindings, а не разрешение запуска
нового CP на старой схеме. Первый V2 init использует отдельный protected mode
`runtime-bind-workspace`; отсутствующий input не включает legacy prepare.
Только этот init получает private PVC root, остальные containers — exact
work subPath и отдельные read-only inode/ACK records. Другой live consumer
той же PVC закрывает BIND; один RWO access mode не доказывает exclusivity.


## Граница Job и подтверждения архива

Новый snapshot worker использует точный UID PVC и закрыто отклоняет любого
другого Pod-потребителя тома, включая terminal Pod. Owner claim и сохранённый
positive join обязательны независимо от результата Kubernetes List. Consumer
подключает только read-only subPath текущего поколения; UID10001 архивирует,
UID10002 читает только свои файлы. Actual Pod повторно сверяется с producer:
mounts, identities, image, environment, resources и отсутствие API token.

Pod не назначает `fsGroup`: kubelet не должен рекурсивно менять режимы исходных
файлов. Socket directory создаётся непривилегированным init в отдельном
`emptyDir`, с primary GID29000 и проверкой mode2770. Применимость подтверждена
исходниками Kubernetes: [emptyDir](https://github.com/kubernetes/kubernetes/blob/v1.32.8/pkg/volume/emptydir/empty_dir.go),
[volume ownership](https://github.com/kubernetes/kubernetes/blob/v1.32.8/pkg/volume/volume_linux.go);
Context7 использован для проверки FSGroup semantics. Native Job-проверка
конкретной версии кластера остаётся обязательной перед activation.

У V2 Job отсутствует TTL. Terminal worker outcome, cancel и transport failure
не удаляют Job/input до успешного terminal RPC владельцу. Cleanup использует
только полученные UID/resourceVersion; неизвестный Create outcome сохраняет
ресурс. Новая owner-attempt завершает прежний workload, сохраняя его input
до собственного ACK. Reconcile-only не меняет root object key и не разрешает
новый PUT. Потеря ACK либо crash после owner commit пока оставляют evidence
на месте: автоматический terminal reconciliation ещё требует реализации
и проверки; наличие сохранённого Job не означает повтор business action.

До завершения отдельных V2 restore/delete consumers эти виды закрыто
отклоняются перед Kubernetes и storage effects, без перехода в legacy.
Это промежуточная поставка полного unit #1953: opt-in остаётся выключенным.

## Потоковое восстановление

Restore consumer сначала read-only проверяет manifest и каждый part. Второй
проход повторяет проверку exact versions/digest и canonical TAR одновременно
с записью в неопубликованное поколение. Scope/target/source назначает owner;
архив выбирает только закрытый logical owner ROLE/PROVIDER, не числовой UID.

Файловый adapter одной идентичности работает через root descriptor с
`RESOLVE_BENEATH|NO_SYMLINKS|NO_XDEV`. Создание использует exclusive open,
существующие entries не перезаписываются. Close проверяет inode, owner/group,
link count, размер и итоговые права, затем синхронизирует файл и parent.
Каталоги временно доступны общей группе для двух writer и получают исходный
mode в глобальном обратном порядке. В памяти остаётся только стек предков,
а не перечень всех файлов или полное дерево.

Ошибка первого чтения не создаёт source files. Ошибка второго чтения,
короткая запись либо неизвестный commit сохраняет неопубликованное дерево;
нет PUT/DELETE-компенсации и нет положительного restore result. Reconcile-only
задача не получает write credit этого consumer. Отдельные source.json,
atomic publication, peer authentication и owner ACK остаются обязательны.
Пока writable peer, generation publisher и owner coordinator не подключены,
controller по-прежнему закрыто отклоняет V2 RESTORE до запуска effects.
Наличие stream/filesystem adapter не является готовым восстановлением.

Внутренний publisher получает уже открытый private generation descriptor и
проверяет sealed `restore-intent.json`: logical task, actual PVC UID,
RuntimeRevision, policy и точные target/source. Attempt и lease туда не входят;
запись не является источником полномочий. Общий `securefile` создаёт записи
исключительно один раз с exact mode0440/UID/GID, fsync(file) и fsync(parent).
Runner использует тот же примитив для чтения generation records.

После закрытия writers publisher связывает `restore-prepared.json` с digest
intent и фактическими device/inode проверенного `.restore-tree`, сохраняет
точный checkpoint в `source.json`, затем выполняет `renameat2(RENAME_NOREPLACE)`
в `tree` и fsync(parent). Он повторно сверяет полную связку перед успехом.
Существующий destination, даже пустой, никогда не заменяется.

Reconcile-only открывает прежний logical intent и только читает matching
records/inode с повторением durability barriers. Потеря ACK после rename
допускает rejoin. Сбой до rename оставляет unpublished tree; partial record,
чужой inode, лишний staging alias или metadata drift не получают positive
result и не удаляются. Специализированное продолжение незавершённой подготовки
ещё требует реализации; generic retry не даёт права на новый rename.
Эти adapter-проверки не доказывают trusted initializer, отдельный writable
provider peer, physical Pod/PVC binding или owner RESTORED/RuntimeRevision ACK.

`PrepareWorkspaceRestore` запускается только под UID10001/GID29000 и открывает
fixed full-PVC mount, назначенный initializer. Controller обязан до запуска
доказать текущий owner grant, exact observed PVC UID и join всех прежних
writers/consumer workloads. Непустой PVC UID в immutable task обязателен,
но сам по себе не доказывает физическое владение томом.

Initializer создаёт только private0700 `workspaces/<workspace>/gN`, sealed
logical intent и empty shared2770 `.restore-tree`. Общий `securefile`
обслуживает exact directory ownership/mode/no-link/no-mount-crossing и
durability; runner использует тот же примитив. Parent root остаётся у caller,
готовый `tree`, source/prepared records и writer ACK initializer не создаёт.

| Состояние подготовки | Допуск и результат | Событие/read path |
| --- | --- | --- |
| Fresh generation, нет intent и entries | Только новый разрешённый target; sealed intent перед созданием empty stage | Domain event нет; exact protected intent/stage readback |
| Restart init, matching intent и empty stage | Те же logical input/PVC; без перезаписи intent или смены inode | Domain event нет; те же records и directory metadata |
| Matching intent, stage ещё отсутствует | Завершить только подготовку до начала data writers | Domain event нет; empty stage и durable parent |
| Reconcile-only, missing/wrong PVC, cancel | Отказ до создания ancestors | Domain event нет; исходный owner task и filesystem readback |
| Partial/nonempty stage, unknown entry, foreign intent/mode/link или served tree | Закрытый отказ, данные и metadata сохраняются | Domain event нет; отдельный owner repair/recovery ещё требуется |

Native restore начинает с отдельного capability-free UID10001 initializer и
только затем монтирует созданный exact subpath. Это проверка исполняемой
файловой подготовки; production controller/worker registration, owner CAS и
post-restore continuation пока остаются незавершёнными частями unit.

Отдельный внутренний writable peer использует `KWW1`, а snapshot wire остаётся
read-only. До создания файлов обе стороны проверяют kernel `SO_PEERCRED`,
canonical task digest и фактические root device/inode. Caller trusted wrapper
назначает identity; wire не содержит UID, absolute path, descriptor number или
credentials. Reconcile-only task не получает writer handshake digest.

Команды закрыты: HELLO, DIRECTORY, FILE, SEAL, COMPLETE, FINISH. При FILE peer
создаёт единственный exclusive provider-owned file и передаёт только
write-only CLOEXEC FD. Main проверяет independent path inode и bounded bytes,
синхронизирует и закрывает свою копию до COMPLETE. Provider проверяет shared
kernel offset и actual inode/size, применяет original mode и fsync. Local byte
counter при этом не подменяется размером. Обычный Close exported FD означает
отказ, даже если размер совпал; другое действие при pending file запрещено.

FINISH закрывает adapter и допускает публикацию только после явного peer join
у caller. Ошибка, cancellation, лишний FD, чужой task/root или потерянный ACK
закрывают попытку без повторной команды и сохраняют partial data. Внутренний
Unixpacket-контур под одним текущим UID проверяет transport/stream/publication,
но не заменяет обязательный distinct UID10001/10002 native proof и production
worker/controller wiring. Эти последние части ещё не активированы.

## Метаданные подтверждения checkpoint

Forward016 расширяет существующие archive/task ledgers закрытым discriminator,
сохраняя прежний publication key `(session_id,content_generation,object_key)`.
Только format2 допускает отсутствующие Codex ID/path; v1 сохраняет прежние
обязательность и bounds. Workspace scope/generation/checkpoint/quiescence
и task body неизменны. Применённые015 и более ранние миграции не меняются.

| Переход016 | Authority и immutable input | Устойчивый эффект/наблюдение |
| --- | --- | --- |
| Create+claim | Existing signed session-archive claim RPC; org из transport; session lock → workspace lock → task lock; frozen ABI4 RR/policy и released positive join | Один SNAPSHOT intent с derived root key; audit/run-changed в той же transaction; первая attempt может писать |
| Content generation | Под общим session lock выбирается max session_storage/archives/tasks +1; workspace generation отдельна | Сохраняется общий high-watermark без повторного использования content generation |
| Retry/expired claim | Тот же task/body/root, fresh monotonic generation/lease/fence; typed input digest включает attempt и reconciliation flag | После первой попытки только read-only reconciliation; нет cleanup неопределённых objects |
| Renew | Exact stored task/lease/fence/generation, fresh current workspace и source pins | Тот же grant и продлённый expiry; события нет, authoritative path — owner task/renew response |
| Complete snapshot | Exact current task и frozen source; format2 root receipt, SHA/bytes/entries и quota bounds | Archive receipt + CHECKPOINT_READY/current_archive + SUCCEEDED без lease атомарны с command receipt/audit/run-changed |
| Fail/final expiry | Closed safe error либо clock owner; не более5 attempts | READY с сохранённым intent либо DEAD_LETTER и отзыв grant; PVC сохраняется; run outcome не меняется |
| Replay/late old grant | Immutable command receipt либо exact current generation | Старый lease не действует; matching повтор возвращает прежний archive ref без нового snapshot |
| Cancel/project deletion | Execution grants закрываются прежним owner; storage past facts остаются | Новый worker не создаётся по неизвестному writer; external purge заблокирован до PURGED |

016 producer использует существующие private Claim/Renew/CompleteSessionSnapshot/
FailSessionArchiveTask RPC и того же consumer Job/permissions. Request format
не выбирает task profile: owner разрешает persisted workspace discriminator
в trusted organization. Default legacy reader не обслуживает такие задачи.
После выключения opt-in compatible reader сохраняет storage-only processing
ранее назначенных bindings; новых execution bindings opt-in не создаёт.

Принятие work-checkpoint не создаёт DELETE_PVC: сначала требуется отдельный
proof сохранности Codex history той же PVC. Snapshot/owner metadata не дают
CLEAN, нового RuntimeRevision, model run либо прав на восстановление. Release
readiness требует полного016 schema/operator proof и compatible archive image;
наличие migration или producer кода само по себе не подтверждает rollout.

Owner выдаёт новую задачу как typed `WorkspaceArchiveWork`; transport переносит
её в существующий Proto field22. Отсутствующий legacy discriminator сохраняет
прежнюю проекцию. Явный workspace discriminator с nil/неверным типом либо
невалидными metadata закрывает весь claim response с прежним безопасным
Unavailable; задача не понижается до legacy. Lease остаётся отдельным
назначенным owner transport field. CP и worker используют один общий union
validator из runtimecontract; storage authority и lifecycle ему не принадлежат.

`make test-session-workspace-archive-wire` передаёт exact Proto bytes настоящего
CP caster независимому worker module через private bounded fixture. Проверяются
четыре вида и snapshot/restore reconciliation; неизвестный enum, чужой scope
и удалённый field22 отклоняются. Источник требований и actor — owner task
contract; внешний endpoint не добавляется. Это proof доставки metadata;
сохранённый coordinator, owner format2 completion и физические consumers
проверяются отдельно до activation.

| Граница | Привязка и поведение |
| --- | --- |
| Worker → termination result | Format2 добавляет digest canonical stream и число записей; exact root receipt остаётся ограниченным. Это не CLEAN-артефакт |
| Controller → CompleteSessionSnapshot | Existing workload identity/permission и task/lease/fence/generation сохраняются; новые Proto fields13/14 несут только digest/entries |
| Transport → SessionArchiveTaskInput | Поля явно отображаются в snapshot metadata; они не назначают scope, writer, generation или authority |
| Legacy owner | Непустые новые поля отклоняются. Отсутствующие поля исключены из JSON, поэтому прежние idempotency bytes сохраняются |
| V2 owner | Stored V2 discriminator выбирает coordinator с exact task/workspace locks; format2 ACK атомарно создаёт receipt и CHECKPOINT_READY, закрывает task без DELETE_PVC; activation требует полного016 proof |

Task/result validation связывает root key, scope, generation, checkpoint ref,
полный digest и прежние byte/file limits до отправки owner command. Частичный
worker result, неизвестное дерево и переполнение не подтверждаются. Нет
нового RPC или публичного endpoint; повторные команды сохраняют прежний
lease-bound idempotency key. Event/consumer wiring нового committed checkpoint
не объявляется существующим до реализации owner transaction.

## Неизменяемый outcome и текущая storage tuple

V2 требует точный role runtime contract revision4; opt-in не повышает старый
образ revision3. Nil binding сохраняет legacy профиль. Перед назначением V2
оператор отдельно подтверждает actual image/contract SHA и все readers.
Owner-to-Proto adapter передаёт в workspace validator уже назначенную revision
из того же snapshot: нулевая техническая копия поля не должна отклонять валидный
V2. Полный adapter проверяется положительным V2 и protobuf roundtrip, отказами
для0/3/unknown, чужого scope, assistant/null/policy и сохранением legacy без
binding. Это проверка доставки metadata, не подтверждение готовности image.

| Наблюдение RC | Условие владельца | Изменение storage | Полномочия выполнения |
| --- | --- | --- | --- |
| BIND | Active exact lease/RR, current generation и pinned contract4; проверка до receipt replay | WRITER_PENDING → WRITING, immutable ACK | Новые run/grant не создаются |
| BLOCKED | Exact frozen lease/current writer; причина из закрытого списка, ACK уже записан | WRITING/QUIESCING → BLOCKED; первая причина сохраняется | UNKNOWN не становится resolved |
| QUIESCENT | Exact уже известный writer и positive current owned-container proof, в том числе после BLOCKED/terminal | CHECKPOINT_PENDING и immutable join | Не открывает lease, writer или restore |
| RELEASED | Ранее записанный exact positive join и authoritative отсутствие прежнего Pod | Append-only writerReleased | Только после этого доступен checkpoint; отсутствие без join ничего не разрешает |

Закрытые причины BLOCKED: POD_MISSING, POD_UNREACHABLE, PVC_MISSING,
PVC_REPLACED, OWNERSHIP_CHANGED и WRITER_JOIN_UNCONFIRMED. Все они сохраняют
PVC и замораживают продвижение нового writer. Поздний exact QUIESCENT может
закрыть неизвестность только того же writer; данные и старый audit не стираются.

Command receipt хранит immutable ACK/committed phase и exact committed join
для QUIESCENT/RELEASED. `currentTuple`, текущие state/reason/released и текущий
join — отдельный readback под exact owner lock, включая повторную выдачу receipt.
Чужое поколение не заимствует этот readback. Исторический RELEASED outcome
допускает очистку только прежних private ticket/projection/policy с UID
preconditions и новым readback отсутствия Pod; он не разрешает PVC deletion,
restore или новое выполнение. Semantic replay этих storage outcomes не зависит
от общего24h request TTL, но сохраняет все exact caller/lease/payload guards.

RC сохраняет ticket и сеть до owner ACK, сначала снимает только собственный
finalizer по UID/resourceVersion CAS, затем подтверждает исчезновение Pod.
Обход retained tickets выбирает только V2 по отдельной метке и обрабатывает
не более32 tickets за poll. Opaque continuation сохраняется между polls,
включая страницы с некорректными tickets; последняя страница завершает цикл.
Истёкший cursor очищается, следующий poll начинает новый обход. Cursor имеет
только значение планирования: рестарт RC начинает обход заново и не создаёт
новых storage outcomes или полномочий. Legacy tickets не заполняют V2 pages;
annotation, immutable input и owner tuple проверяются независимо от метки.
Клиент модели не может прислать positive QUIESCENT через обычный callback:
этот факт устанавливает RC по terminal statuses и sealed provider join record.
Отдельный init-only случай требует failed deleting Pod, явного завершения
начавшихся init containers и положительных waiting/no-ID/no-restart statuses
всех никогда не стартовавших main containers. Missing Pod этим доказательством
не является. Role/provider process group join дополняется завершением всего
container перед checkpoint, поэтому отдельный detached descendant не обходит
storage barrier.

Создание private ancestor directories и tree включает fsync(child) и
fsync(parent), а records — file fsync и generation-directory fsync. После
crash между полным mount.json и отсутствующим writer.json fresh BIND допускает
дописать ACK только при неизменном protected proof и пустом либо точно
восстановленном tree. Частичные/чужие records и изменённое дерево сохраняются
в закрытом отказе; существующие bytes не удаляются ради повторной попытки.

## Локальная проверка двух UID

`tools/dev/workspace-reader-native-test.py --binary <absolute-test-binary> --sha256 <sha256>`
принимает зафиксированный статический test binary пакета `internal/archive`,
собранный через `CGO_ENABLED=0 go test -c`. Проверка требует локального Linux
Docker и уже загруженного pinned image; загрузки образа и внешней сети нет.
Контейнер подготовки создаёт только private fixture, после чего отдельный
контейнер монтирует source volume read-only. Настоящие consumer UID10001/10002
теряют все capabilities; проверяются private mode0600, EROFS, отказ прямого
чтения provider bytes, точный stream и завершение peer. Контейнеры удаляются
по сохранённым exact IDs после остановки; volume — только после readback
отсутствия контейнеров и проверки собственной метки. Секреты и рабочее дерево
репозитория не монтируются. Это proof файлового механизма, а не rollout,
полный owner lifecycle или приёмка сотрудником платформы.

Тот же entrypoint с `--mode restore` запускает отдельный writable-контур
`TestWorkspaceWriterTwoUIDNative`: pinned binary, без сети, 1CPU/256MiB,
отдельный labelled volume и IPC tmpfs. Основной процесс UID10001 и provider
UID10002 проверяют отсутствие effective/permitted capabilities. Provider
восстанавливает private mode0700/0600, main — собственный mode0600; прямое
чтение private данных другого UID должно получить EACCES. Coordinator fixture
с только SETUID/SETGID ждёт exact provider process через Wait и передаёт main
один join marker по унаследованному pipe до atomic publication. Затем отдельный
provider verifier сверяет bytes/owner/mode после завершения main.

Generation parent0700 и точное `.restore-tree` монтируются из одной disposable
volume, для tree используется штатный `volume-subpath`. Root fixture готовит
только корень volume, а private ancestors/intent/stage создаёт отдельный
UID10001 initializer без capabilities через настоящий PrepareWorkspaceRestore.
Это проверка реальных
файловых механизмов и SCM_RIGHTS между UID, не production Kubernetes render,
S3 custody, owner RESTORED или full backup/restore. Default snapshot mode и его
read-only mount остаются прежними. Отсутствие opt-in в обычном go test даёт
явный SKIP, отдельный native запуск оценивается по terminal JSON и cleanup.

При `chmod` SGID может молча сбрасываться, если процесс не входит в группу
каталога и не имеет CAP_FSETID. Подготовка использует требуемую группу и
проверяет фактический режим, не расширяя capabilities:
[Linux chmod(2)](https://man7.org/linux/man-pages/man2/chmod.2.html).

## Совместимость миграций и удаления

Workspace вводится отдельной015 после полного014. Первоначальная неприменённая
01150 была перенумерована: вставка между011 и012 нарушала последовательные
predecessor и purge graph proof следующих workflow-миграций. Исходная012
восстановлена дословно;013/014 и их самостоятельные proofs не меняются.
Отдельный успешный переход01150→012 не доказывал совместимость всей цепочки.

| Сценарий | Actor и источник полномочий | Состояние и результат |
| --- | --- | --- |
| Применение015 | Штатный migrator и exact Goose014 | Создание workspace; inverse source proof прежнего purge и точное добавление одной таблицы/семи graph FK |
| Полная проверка015 | Read-only snapshot и системный каталог |19 columns,37 constraints,4 indexes, guard/source/ACL,32 FK triggers; actual graph106/294 и отдельная inverse014 projection |
| Schema drift | Payload не выбирает допустимые metadata | Лишний столбец, изменённый CHECK/guard/ACL, выключенный FK trigger закрыто отклоняются |
| Повтор015 | Goose ledger | No-op; metadata и версия читаются повторно |
| Project purge | Existing owner inventory и terminal transaction | Наличие workspace не в PURGED блокирует выдачу external inventory и подтверждение удаления; row DELETE дополнительно требует owner purge context и PURGED |

make test-runtime-workspace-predecessor-postgres использует отдельную
loopback PostgreSQL: полную014, forward015,11 изменений каталога с rollback
и повтор. Четыре runtime table privileges проверяются отдельно:
has_table_privilege со списком проверяет наличие любого, а не всех.
generate-session-workspace-proof.py --check проверяет воспроизводимость
полного proof. Frozen014 source hash обязателен; predecessorProjection
явно обозначает обратную проекцию, actual версия/граф возвращаются отдельно.
Нет изменения applied migrations, допуска неизвестной схемы или подмены
actual015 старой версией в отчёте.

До реализации полного workspace purge consumer новый профиль остаётся
выключен: блокировка сохраняет custody, но сама по себе не завершает очистку.
Operator rollout, retention/restore, backup и реальный запуск сотрудника
ещё требуют завершения всего unit. Domain event от миграции не создаётся:
авторитетный read path — Goose ledger и системный каталог.

Отдельная точка `make test-runtime-workspace-owner-postgres` проверяет настоящие
owner commands в disposable БД: опубликованный ABI4 и V2 claim, BIND, отказ
чужому workspace, terminal BIND replay, BLOCKED без положительного join,
поздний matching QUIESCENT и RELEASED. Historical receipt остаётся immutable,
текущая storage tuple читается отдельно, отменённый run не открывается снова.
Физические Pod/PVC facts задаются controller fixture; эта проверка не
подменяет обязательный реальный Kubernetes/сотрудник прогон.

Context7: PostgreSQL18 pg_constraint, pg_trigger и ALTER TABLE;
convalidated/conenforced не заменяют проверку включённых FK triggers.
