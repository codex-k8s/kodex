---
id: OPS-DOC-RUNTIME-WORKSPACE-1953
title: Устойчивое рабочее состояние сотрудника
type: operation-contract
status: approved
owner: developer
version: 1.0.1
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
прежний общий CPU/RAM budget разделяется между consumers. Неизвестное владение,
недоступный private directory или изменённый descriptor оставляют PVC в BLOCKED.
Restore создаёт новые файлы под UID10001/GID29000 в собственной generation.
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

Forward migration `20261026001150` располагается после01100 и до01200;
применённые миграции не меняются. Catalog/purge proof и rollout относятся к
точному отдельному checkpoint, без allow-missing и без обхода остановленных
native-путей других задач. Проверки operator, native, backup/restore и реальная
приёмка сотрудником обязательны до заявления готовности.

Поставка приложения требует предварительно подтверждённой schema01150 даже
при выключенном opt-in: claim query читает owner storage eligibility до LIMIT.
Default legacy означает отсутствие новых bindings, а не разрешение запуска
нового CP на старой схеме. Первый V2 init использует отдельный protected mode
`runtime-bind-workspace`; отсутствующий input не включает legacy prepare.
Только этот init получает private PVC root, остальные containers — exact
work subPath и отдельные read-only inode/ACK records. Другой live consumer
той же PVC закрывает BIND; один RWO access mode не доказывает exclusivity.


## Неизменяемый outcome и текущая storage tuple

V2 требует точный role runtime contract revision4; opt-in не повышает старый
образ revision3. Nil binding сохраняет legacy профиль. Перед назначением V2
оператор отдельно подтверждает actual image/contract SHA и все readers.

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
