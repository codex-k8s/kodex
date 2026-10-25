---
id: GUIDE-DOC-006
title: Защищённые агрегаты и граф фонового выполнения
type: guide
status: approved
owner: architect
version: 1.1.38
updated: 2026-10-25
---

# Защищённые агрегаты и граф фонового выполнения

Продолжение после решения владельца переносит авторитетные Gate ref/version,
решение и точное сохранённое замечание в новый immutable task в транзакции
создания turn и lineage. Замечание владельца отделяется от результата внешнего
провайдера; отрицательное решение не описывается как одобрение. Feedback не
заменяет grant, не разрешает повторный effect и не открывает terminal root.
Публичная проекция callback сохраняет типизированное скрытие внутренней задачи.
Матрица и сквозной путь: [#1863](../operations/integration-gate-intent-1075.md#замечание-владельца-в-продолжении-1863).

Финальное удаление большого owner-графа проверяется на реалистичной истории,
а не только на пустом проекте. Точные relation_id входят в индексируемые
предикаты каждого ребра/таблицы; сравнение сохранённых устойчивых ключей
использует типизированные PK-колонки, не декартово JSON-сравнение. Оптимизация
не заменяет fingerprint графа, tenant boundary, row locks, lifecycle guards,
внешний inventory proof или одну terminal-транзакцию. Истёкший budget не
объявляется DONE и не разрешает ручное удаление receipt.

Readiness snapshot графа проверяет не только RETRY_OF, но и root-to-root
CONTINUES: оба owner-readable root относятся к одной непустой session,
предшественник SUCCEEDED, exact server-owned edge принадлежит новому root.
Внутреннее CONTINUES между attempts одного run не даёт cross-root полномочий.
Отсутствующее/дублирующее ребро, цикл, другая сессия или несовпадающий retry pin
закрыто отклоняются на owner, gateway и frontend boundaries. Это проверка
авторитетного чтения, без нового grant или события.

Корневой узел помощника имеет тип `AGENT_EXECUTION`, поэтому проверка
`RETRY_OF` не может отождествлять корень с `ROOT_PROCESS`. Для такого ребра
оба узла не имеют родителя и принадлежат точным owner-readable root runs
помощника; сотрудник узла совпадает с `AssistantRunPin`, обе привязки целиком
равны, сессия непустая и общая, номер попытки увеличен ровно на один.
Это узкое правило чтения применяется одинаково в control-plane, WebSocket
проекции и PWA; произвольный `AGENT_EXECUTION` не подтверждает lineage.
Оно не расширяет допуск `CONTINUES`, не создаёт grant и не меняет retry policy.

Окончательный PermissionDenied точного CompleteExecution закрывает собственные
runtime Pod/ticket/projection/сеть и tracker независимо от наличия deadline:
cancel/interrupt также отзывают lease. Transient outage не подменяется отзывом,
не даёт ложного terminal receipt и не разрешает удалить чужую attempt.

Организационный audit или audit другого проекта не становится собственностью
удаляемого проекта из-за FK на его сотрудника. В защищённой purge-транзакции
после exact inventory proof снимается только физическая assistant_agent_id
ссылка того же tenant на удаляемого сотрудника; audit/root actor/action/resource
сохраняются. Собственная проектная история удаляется по прежнему owner-графу.
Отвязка не разрешает чужую организацию, обычный runtime UPDATE или обход
immutable guard и откатывается вместе с неуспешным terminal.

Новые NO ACTION FK внутри замкнутого project purge graph должны быть
DEFERRABLE INITIALLY IMMEDIATE. Обычные транзакции сохраняют проверку FK;
только purge откладывает их до удаления полного графа в той же транзакции.
Fingerprint таблиц и действий FK не доказывает deferrability: отдельная
проверка schema и runtime guard обязательны до terminal-перехода.

Диагностика отказа worker различает подтверждённую текущую причину, прошлую
историю и неизвестность. Exit code либо потеря локального потока не доказывают
отказ внешнего провайдера или конкретного ресурса. Закрытое наблюдение до
cleanup не меняет semantic intent повторного completion по тому же ключу;
результат принадлежит прежней owner-транзакции, usage/archive не теряются.
Матрица локального OOM-пути — [OPS-RUNTIME-MEMORY-001](../operations/runtime-memory-failure.md).

`childRunRefs` принадлежит точному узлу, а не всем узлам родительского run.
Ordinary child подтверждается серверными `DELEGATED_TO`/`CALLBACK_TO` и
materialized target внутри одного organization/root. Required Workflow
подтверждается persisted launch с exact origin/proxy/child root и связанными
рёбрами. Parent run сам по себе не доказывает происхождение. `PLANNED` и
`ROOT_PROCESS` не наследуют children; историческая связь materialized узлов
сохраняется после terminal. HTTP graph, полный realtime snapshot и event delta
используют одинаковое правило, не декодируют payload/summary как authority.

Workflow prompt получает компактный полный DAG из exact pinned опубликованной
ревизии: назначения, зависимости, порядок, parallel group и Human Gate.
Эта owner-проекция является данными, не эффективным frontier или разрешением
делегирования. Published version number берётся из authoritative version row,
а не сохранённого draft spec или workflow OCC. Snapshot/materialization digests
связывают новое содержание; исторические snapshots не переписываются.

Aggregate catalog не должен выдавать недопущенную exact ревизию и не должен
позволять ей блокировать соседние допустимые объекты. Пропуск разрешён только
по различимому отказу eligibility/отсутствию ресурса владельца; адресный read
сохраняет отказ. Повреждение найденного package, mismatch current pins и
ошибка чтения не относятся к отсутствию eligibility и закрывают весь read.
Фильтр и cursor применяются после единого authoritative eligibility rule;
пропуск не включает исторический decoder и не создаёт права на mutation.

Каталог собственных прав помощника использует ту же проверенную read-only
проекцию exact published package, что каталог назначенного сотрудника.
Корректный, но несовместимый с текущим executable registry пакет доступен
для диагностики как `PACKAGE_UNAVAILABLE`, `grantable=false`. Enable и
invocation по-прежнему проходят отдельный executable decoder и закрыто
отклоняются. Ошибки owner scope, binding, content digest, parser и чтения не
превращаются в диагностический успех и не скрываются blanket suppression.

Перепривязка consumer между configuration sets проверяет глобальную связь по
организации/kind/consumer, а не только версию нового set. Явное expected absence
разрешает только INSERT; существующая связь меняется только UPDATE с точными
revision/version. Отсутствующая связь не создаётся в режиме MATCH. Impact
commitment учитывает связи других sets того же kind и project/org scope;
список для выбора по-прежнему ограничен target set и read eligibility.
Stale digest/pins возвращают version mismatch без частичных effects, receipt,
audit или events. Успех сохраняет их в одной owner-транзакции; exact replay
возвращает прежний receipt и не перепривязывает consumer повторно.

Initial SYSTEM binding без физического version pin включается в owner impact
по effective текущей ревизии только для канонического организационного помощника.
После публикации выбранный consumer получает явный exact pin: прежний snapshot
проверяется по неизменившимся agent/binding versions и exact parent новой ревизии.
Это исключение принадлежит одной publication-транзакции; обычный rebind, PROJECT
и другие агенты не получают NULL fallback либо обход OCC.

Пустой результат claim не доказывает отсутствие изменений: истечение lease и
terminal непригодного кандидата сохраняют audit и command receipt в той же
транзакции. Только действительно неизменившийся idle poll может их пропустить.
Queued execution с terminal SessionStorage `ERROR|PURGED` сверяется до join
runtime eligibility: отсутствующая credential или конфигурация не скрывает
неисполняемый граф. Server-owned ClaimExecution ограниченно блокирует точные
tenant/session/node/storage, закрывает весь root graph существующим atomic
terminal path и сохраняет audit, receipt и ordered run/node/gate events.
Storage не переводится обратно в `LIVE`, свежий grant/RuntimeRevision не выдаётся.
`SNAPSHOT_READY|SNAPSHOTTING|DELETE_PVC_READY|ARCHIVED|RESTORE_READY|RESTORING`
остаются ожиданием, а не terminal failure.

Terminal storage reconciliation сохраняет различимую закрытую диагностику
до terminal command: stage/class и exact run/node/session refs, без archive
descriptor, исходного содержимого и credentials. Наличие такого наблюдения
не доказывает commit транзакции; replay или rollback не объявляются новым
terminal effect. Отсутствие общего eligibility warning не является
доказательством пригодности сессии.

Адресный Run и workspace RunGraph вычисляют session readiness одним owner
predicate в той же защищённой read-транзакции. Отсутствующая или повреждённая
storage metadata не превращается в готовность. UI блокирует продолжение только
по точной текущей conversation/session/run привязке; историческая ошибка другого
хода не переносится на новую сессию. Read-проекция не выдаёт grant и не заменяет
повторную серверную проверку перед claim.

Перед возобновлением provider thread подтверждённый read-only locator rollout
хранится отдельно от execution binding. Он проверяется по trusted session,
canonical workspace, regular single-link inode, owner/mode и размеру. Отказ
resume/bind не назначает thread/turn или usage. Только после bounded abort/join
writer тот же подтверждённый inode может дать свежий sealed archive descriptor;
старые size/digest не переиспользуются. Замена inode, неизвестный writer либо
невалидный locator закрывают capture. Исторический storage ERROR не чинится
переписыванием tuple или ослаблением admission.

| Переход storage-blocked execution | Результат владельца |
| --- | --- |
| create во время snapshot/restore | Queue сохраняется; claim не запускает provider до `LIVE` |
| claim при `ERROR|PURGED` | Одна owner-транзакция завершает root tree как `FAILED`, закрывает leases/turns/gates/effects; durable receipt, audit и ordered events |
| claim при transit state | Нет terminal-перехода и новых runtime grants; очередь ждёт штатный archive/restore |
| renew/complete после terminal reconcile | Прежняя lease/grant закрыто отклоняется существующим terminal fence |
| owner cancel до reconcile | Штатный OCC cancel закрывает весь граф независимо storage; последующий reconcile не повторяет эффект |
| replay/повторный poll после reconcile | Сохранённый command receipt либо отсутствие открытого кандидата; новых terminal events нет |
| retry/continuation | Новая attempt проходит обычную свежую authority и storage eligibility; terminal storage не восстанавливается автоматически |

Устойчивый cleanup receipt может повторно сообщать прежний produced descriptor
после его отдельной очистки. Владелец принимает доказанное exact terminal
завершение идемпотентно, не создаёт повторный эффект и сохраняет защиту от
current/foreign descriptor и циклов. Receipt upload/delete не заменяет свежую
authority; tombstone сохраняет owner binding для ограниченного разрешённого
replay, не открывая удалённый ресурс обычному каталогу.

`GUIDE-DOC-006` задаёт переносимый способ проектирования control plane,
планировщиков, оркестраторов, фоновых задач и других unit, где одна операция
связывает полномочия, несколько агрегатов, аренду, повтор и внешний результат.
Правила дополняют общую структуру `GO-DOC-001`, PostgreSQL-профиль
`GO-DOC-002`, события `GO-DOC-004` и распределённую безопасность
`GUIDE-DOC-003`.

## Защищённый вид ресурса

Ресурс считается защищённым, если его изменение способно:

- выдать или отозвать permission, роль, членство, привязку credential или
  делегирование;
- запустить, продолжить, повторить либо остановить исполнение;
- подтвердить решение владельца, scan/admission или иное заключение о
  допустимости;
- изменить расписание, аренду, попытку, заявку, доказательство завершения или
  проекцию, используемую для выдачи полномочий.

Для защищённого вида универсальный `create|update|transition|delete` запрещён.
Закрытый реестр перечисляет все такие виды и их специализированные команды.
Каждая команда имеет отдельные разрешение, источник полномочий, поиск
владельца, OCC/идемпотентность, конечный автомат, аудит и событие либо путь
чтения. Если хотя бы одна обязательная операция завершения, отмены, удаления
или повтора не имеет достижимого специализированного пути, вид нельзя открывать
даже для универсального создания.

При создании несущего полномочия ресурса сервер назначает владельца, начальное
состояние и область grant. Назначаемые роли, actors и permissions должны
входить в разрешённое вызывающему множество; включение себя в роль, повышение
собственных полномочий и выдача себе новой capability закрыто отклоняются.
Разрешение уровня проекта не заменяет проверку сохранённого `owner_actor_id`,
корневого инициатора или ребра делегирования. Административное исключение
допустимо только как явная permission и отдельный аудируемый сценарий.

## Идентичность графа

Фоновая работа имеет одну неизменяемую связку:

```text
organization + project + owner/root actor
+ root session/turn/attempt
+ current session/turn/attempt
+ parent process + launching delegation edge
+ immutable input digest + RuntimeRevision version/digest
+ workload + authority generation + grant JTI/fence
```

Идентификатор из запроса не подтверждает ни один компонент этой связки.
Сервер разрешает её из проверенного transport/signed context и заблокированного
состояния владельца. Grant фоновой задаче связывается с точными workload,
audience, полным методом, permission, session, turn, attempt, неизменяемым
хэшем входа и монотонным поколением. Заявка или grant на весь проект без этих
координат запрещены.

Дочерний процесс создаётся только через принадлежащее серверу ребро
делегирования:

- source содержит точные parent process/session/turn/attempt/input;
- target содержит точные role/session/turn/attempt/input и поколение grant;
- корневые actor, policy, playbook, trigger route и исходный неизменяемый снимок
  наследуются от заблокированного parent;
- вызывающая сторона доказывает право запустить именно это ребро;
- межсессионный дочерний процесс разрешён, но чужое либо несовпадающее ребро
  скрыто отклоняется;
- завершение parent либо закрывает весь обязательный дочерний граф, либо
  отклоняется при незавершённом дочернем процессе.

При материализации этапа Workflow CP передаёт в child input исходные значения
exact canonical root и его immutable опубликованной версии. Дополнительный
input делегирования может только дополнять их: совпадающее значение допустимо,
отличающаяся подмена закрыто отклоняется до создания child и его effects.
Общий key/byte budget проверяется после объединения. Повреждённый root/spec
является недоступностью авторитетного источника, не caller validation и не
основанием terminal eligibility. Ordinary delegation без Workflow сохраняет
прежний input; nested Workflow использует собственный canonical root.
История новой Session, callback artifacts и имя Workflow не заменяют эту
передачу. Значения данных не становятся authority, grants либо файловым доступом;
исторические inputs/RuntimeRevision не переписываются.

Подтверждаемое структурное изменение Workflow сохраняет каждое исходное ребро
оставленных этапов и объединяет его с зависимостями нового серверного фронта.
Без изменения порядка и parallel-групп исходный DAG сохраняется точно.
Удалённый либо перемещённый после своего потребителя prerequisite, повторный
key/dependency и явный caller-controlled DAG отклоняются до effects. Новым
этапам ключ назначает сервер. Before/OCC и вычисленный заново After проверяются
в owner boundary; After содержит точный применяемый draft, а не выдаёт authority.
Агрегация ждёт всех parallel peers; публикация создаёт новую immutable версию,
не переписывая historical execution pins, граф и inputs прежних запусков.

Ограничения human-text Workflow согласованы с Unicode `maxLength` схемы;
некорректный UTF-8 отклоняется, общий размер инструкций остаётся байтовым.
Обычная команда и подтверждаемый план используют один predicate. Диагностика
invalid proposal сохраняет прежний отказ и содержит только закрытые stage/field
реального rejecting guard, а не текст payload или ошибку зависимости. Consumer
принимает лишь точный canonical code/domain/detail и закрытый набор metadata;
неизвестные поля не становятся подсказкой, locator или разрешением повторить
эффект. Native ошибка и durable FAILED receipt сообщают согласованный код.

## Авторитетный граф выполнения

Минимальный граф для запуска по расписанию:

```text
Schedule
-> ScheduleOccurrence
-> ScheduledRun(attempt)
-> Session
-> Turn
-> TurnAttempt
-> ProcessRun / delegation tree
-> OwnerGate, если требуется
-> WorkClaim / worker grant
-> immutable result, receipt, audit, event или versioned read
```

Отсутствующий узел фиксируется как неприменимый с причиной. Нельзя хранить
только envelope планировщика, если session/turn/process уже материализованы.
`ScheduledRun` или эквивалентная неизменяемая запись связывает occurrence и
каждую отдельную attempt с точными версиями session, turn, process,
`RuntimeRevision` и итогового входа. Retry создаёт новую attempt и сохраняет
предыдущую, а не перезаписывает привязку.

Для Workflow и опубликованного этапа deadline назначается DB clock при первом
допустимом claim и далее неизменен: очередь до claim исключена, Human Gate,
continuation, ожидание и reclaim входят в wall-clock. Immutable RuntimeRevision
и exact runner ABI связывают все ancestor clocks с version/digest источника;
controller keeper и runner/provider независимо cancel/join по их минимуму.
Lease продление, делегирование, leased read и новый integration effect не
продлевают срок. Owner expiry атомарно закрывает полный граф как
`FAILED/RUNTIME_TIMEOUT`, сохраняя уже начатый WRITE как `UNKNOWN_OUTCOME`.
Late success не меняет terminal verdict; подтверждённые usage/archive pins
сохраняются только после исходной проверки результата и до отзыва lease.
Полная матрица и forward-only cutover приведены в
[ARCH-MC-007](../architecture/runtime-and-sessions.md#устойчивый-срок-workflow-и-этапа).

### Матрица переходов

До реализации developer составляет отдельную строку для каждого применимого
перехода. Объединять разные виды задач в одну строку без одинаковой семантики
полномочий и жизненного цикла запрещено.

| Переход               | Обязательная блокировка и проверка                                                                  | Атомарный результат                                                                                  |
| --------------------- | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| create/materialize    | owner, версии источников, допустимость, отсутствие конфликтующей привязки                           | новые узлы, неизменяемые input/revision, receipt, аудит, события                                     |
| claim/start           | точный предшественник в очереди, FIFO/session fence, workload/grant/attempt                         | одна победившая lease, attempt и claim                                                               |
| renew                 | lease token/fence, незавершённая session→turn→attempt→process/delegation                            | только продление той же attempt; terminal/stale закрыто отклоняется                                  |
| complete              | точный конечный результат worker, проверки открытых дочерних процессов и работ                      | turn/attempt/process/run/result и claims согласованы                                                 |
| cancel                | весь материализованный граф и owner                                                                 | lease/grants/claims/gates отозваны, связанные узлы завершены                                         |
| delete/cleanup        | отсутствие активной работы либо её согласованное закрытие                                           | достижимый `DELETED`, tombstone и авторитетное чтение                                                |
| retry                 | завершённый/просроченный предшественник, лимит, неизменяемый bounded SourceRef и предыдущая attempt | новая attempt/grant/revision и полный current execution tuple после закрытия старых gate/claim/lease |
| lease expiry          | блокировка строки точной lease и полная неизменяемая привязка                                       | старый граф закрыт до возврата в очередь/dead letter; один победитель                                |
| dead letter           | исчерпание лимита и доказательство завершения                                                       | поток заблокирован до ограниченного аудируемого repair/skip                                          |
| owner waiting         | точная привязка root/delivery/schedule                                                              | turn/process/occurrence/run переходят в `WAITING_OWNER`, leases отозваны                             |
| owner decision/expiry | receipt доставки или expiry по времени PostgreSQL, OCC/fence                                        | ровно один decision/expiry закрывает или продолжает весь граф                                        |

Завершение только одного envelope при живом дочернем исполнении запрещено.
Старт второй реплики фонового controller не является authority для общей
очистки его Jobs. Перед retry новый exact owner claim подтверждается renew;
старый writer разрешается только по прежней attempt/input и полной owner
связке, удаляется с UID/RV preconditions и joined до нового writer. Cleanup
своей попытки не удаляет replacement по имени; неизвестный Create не означает
владения. Namespace-wide sweep по одному managed label запрещён. Матрица
архиватора и cutover приведена в [OPS-PUBLIC-STABLE-001](../operations/public-stable-1810.md#архиватор-изоляция-попыток-двух-реплик).
Каждый путь terminal/cancel/delete/retry/expiry повторяет одну проверку owner и
отзывает все grants/leases/claims в той же PostgreSQL-транзакции. Путь чтения
скрывает claim как активный, если его авторитетный граф уже завершён, даже до
фоновой очистки.

Гонка claim/renew/cancel, owner click/expiry и complete/retry разрешается
row/OCC/fence-моделью с одним победителем. Повтор с тем же idempotency scope и
request hash возвращает сохранённый результат; новый эффект не создаётся.

Для Report/Renew/Complete/Fail сборки образа semantic intent исключает только
назначаемый заново transport CorrelationRef. Исходный Principal проходит
свежую проверку без изменений; actor/organization/project/permission/workload,
credential revision/authentication, lease/token/attempt/fence/OCC и payload
остаются в commitment. Новый correlation не создаёт второй effect, а чужая
authority и изменённый intent закрыто отклоняются. Сохранённые receipt hashes
не переписываются и не получают fallback decoder: cutover выполняется при
отсутствии активных сборок, последующие команды используют новые keys.

Полученный batch runtime claims продлевается с момента получения ответа, а
не только после последовательной материализации Pod. Каждый keeper немедленно
и периодически вызывает существующий exact lease/fence/generation renew;
отказ отменяет материализацию. Финальная публикация Pod и warm dispatch
сериализованы с renew и проходят свежий owner fence до эффекта. Передача tracker
сначала отменяет и дожидается keeper, затем продлевает ту же lease: двух
конкурирующих владельцев heartbeat нет. Shutdown отменяет и дожидается всех
keepers до закрытия RPC клиента. TTL, attempt, authority и owner expiry/requeue
при этом не расширяются; transport 404 сам по себе не доказывает expiry.

## Решение владельца

Mailbox Human Gate проверяется независимо от generic integration approval policy.
Для email с generic `NONE` подтверждение exact mailbox gate открывает только
исходную invocation с теми же organization/project/root/input и активной версией
grant. Завершившийся сотрудник не блокирует этот effect, если root ещё находится
в `WAITING_HUMAN` и точный gate одобрен; это не оживляет terminal root, отозванную
lease либо grant. Claim и последующая email authorization используют одинаковое
ограждение. Повторное одобрение не создаёт новую invocation или отправку.

### Ограниченное восстановление доставки runtime heartbeat

Источник — тот же `RuntimeWorkService.RenewExecution` и authoritative
`WorkLease.expires_at`. Контроллер передаёт только прежние exact lease/fence/
generation; actor и workload проверяются обычным transport/owner command.
Нет нового endpoint, grant, attempt, события или локального продления TTL.
Ответ принимается только с совпавшими pins и допустимым ещё не истёкшим expiry.
Предыдущее подтверждённое expiry ограничивает весь новый RPC и его повторы;
ответ после этого срока не восстанавливает исполнение. Наблюдение сетевого
сбоя не доказывает revoke, но и не назначает новый срок полномочий.

| Переход heartbeat | Контроллер и владелец состояния |
| --- | --- |
| Claim → materialization | До первой подтверждённой renewal нет повторов по предполагаемому TTL; свежий exact owner ответ обязателен до публикации |
| Периодическая materialization / публикация | Только `Unavailable/DeadlineExceeded`, ограниченные число/задержки и прежнее expiry; публикация сериализована с renewal и отдельно ограничена expiry |
| Keeper → tracker | Keeper отменён и joined; последний успешный renew передаёт tracker подтверждённый expiry, без второго heartbeat writer |
| Track → drain | Тот же expiry; graceful stop не выдаёт authority, renew ограничен независимым drain budget и потерей leadership |
| Renew success | Owner повторно проверяет exact живую lease/graph; контроллер принимает только matching pins и свежий expiry, без подмены input/revision |
| Permission/NotFound/stale/mismatch | Немедленный закрытый отказ без повторов, Stop/Delete и завершение локального coordinator; owner terminal не переписывается |
| Временная доставка без подтверждённого expiry | Один bounded RPC, затем закрытый отказ; неизвестный commit не считается renewal success |
| Подтверждённое expiry / исчерпание попыток | RPC прекращён, workload закрыт; устаревший success не оживляет terminal/root и не запускает retry attempt |
| Cancel/delete/root terminal/lease expiry | Прежняя owner-транзакция отзывает leases/grants всего графа; свежий renew отвергается, исторические receipts не назначают новый expiry |
| Callback завершился во время recovery | Локальный watcher joined; callback сохраняет terminal result, heartbeat не создаёт второе completion |

Небольшой запас до authoritative expiry используется только для раннего
прекращения RPC и локального cleanup, никогда для расширения срока. Heartbeat
не разрешает новый пользовательский effect при отказе его отдельной authority.
Проверки покрывают три фазы, ошибочный ответ, повторный exact tuple, revoke,
parent cancellation, expiry и отсутствие публикации/воскрешения после отказа.

### Обязательный дочерний Workflow обычного сотрудника

Native `launch_workflow` использует только closed
`RuntimeWorkService.LaunchWorkflowExecution`; runtime-controller не получает
owner `LaunchRun`. CP разрешает organization/project/root actor из текущих
lease/fence/generation и immutable revision, проверяет materialized и current
`platform.run.launch`, а также exact `workflow.launch` корневого пользователя.
Клиент передаёт только locator существующего опубликованного Workflow, task,
title и input. `AGENT_DELEGATION` назначается сервером. Configuration plan и
право SYSTEM/PROJECT помощника для этого пути не требуются и не наследуются.

Workflow сохраняет собственный root, version-pinned steps и canonical claim
scope. Отдельная required relation закрепляет origin root/run/node/session/
turn/attempt/generation/input/revision, дочерний root, опубликованную workflow
version и local callback edge. Parent/root не подменяет workflow step scope.
Graph содержит local proxy node; события и durable callback receipts используют
существующий ordered run-event и continuation путь.

| Переход required Workflow | Owner-транзакция и результат |
| --- | --- |
| launch/materialize | Project-scoped сериализация до row locks; свежая authority; canonical WF root, immutable relation, proxy/edges, receipt/audit/events |
| claim/start | Текущие root actor, origin capability и exact workflow permission всех required ancestors; собственный свежий RuntimeRevision W |
| renew | Exact W lease/fence/attempt; parent cancel атомарно отзывает W lease, поэтому stale renew закрыто отклоняется |
| parent complete | `OPEN` relation не допускает terminal parent; continuation ждёт все required results |
| W complete | Один terminal relation/proxy transition и durable callback P; FAILED/CANCELLED result не допускает parent SUCCEEDED |
| parent/root terminal или cancel | Та же транзакция закрывает все required roots и их leases/turns/gates/effects; terminal parent не получает новый callback turn |
| owner W cancel/reject | Закрывает собственный canonical graph W и required subtree; живой P получает durable CANCELLED/FAILED callback |
| retry | Новый origin/root/attempt и новая relation; прежние coordinates и result не переписываются |
| lease expiry / eligibility failure | Существующий ClaimExecution terminal path и та же required reconciliation; не новый timer |
| owner gate / changes requested | Существующий OCC gate и fresh continuation; required results остаются обязательными; generic gate expiry — NONE/N/A |
| delete / purge | Сначала штатный terminal/trash, затем exact project purge graph и row-targeted protected cleanup; обычный delete relation запрещён |
| replay / unknown response | Exact active authority проверяется до receipt replay; accepted intent не создаёт второй child при новом transport key |

Bounded required graph ограничивает cardinality и глубину; переход terminal
не допускает частично закрытый envelope. Ошибка audit/event/receipt откатывает
все вложенные terminal transitions. Авторитетный readback: canonical Run/Graph,
server-owned launch/callback refs в ответе native operation и callback Turn;
payload actor, tenant, source и parent/root lineage не являются authority.

`OwnerGate` закрепляет назначенные сервером root actor, recipient, process,
current session/turn/attempt/input, policy, schedule/occurrence/`ScheduledRun`,
delivery ID, canonical payload digest и фактический post/interaction receipt.
Общая permission или actor ID из payload не разрешают решение.

- До авторитетного receipt доставки решение закрыто отклоняется.
- Истёкшая карточка не выдаётся на доставку.
- Ограниченный обработчик expiry выбирает просроченную строку по времени
  PostgreSQL.
- Click и expiry блокируют одну строку и дают ровно один конечный эффект.
- `APPROVED`, `REJECTED`, `EXPIRED`, `CANCELLED` и
  `CHANGES_REQUESTED` имеют разные явно документированные переходы.
- `CHANGES_REQUESTED` не отображается автоматически в `FAILED`: если
  продуктовый контракт требует продолжения, прежние attempt/grants закрываются,
  а тот же process получает новые созданные сервером turn, input и
  `RuntimeRevision`; граф расписания хранит отдельную привязку продолжения.
- Повторный `CHANGES_REQUESTED` проходит тот же переход из `CONTINUATION`:
  новый gate/feedback/attempt не перезаписывает историю и атомарно обновляет
  current tuple процесса, occurrence и `ScheduledRun`.

## Свежая `RuntimeRevision`

Декларация `platform.run.delegate` не создаёт разрешённые цели делегирования.
План процесса связывает этап с фактическим исполнителем, а не с текстовым
поручением координатору найти другого исполнителя. Процесс, в котором все этапы
принадлежат координатору и нет обязательного делегирования, не может объявлять
делегирование необходимым инструментом этапа. Проверка draft/публикации должна
отклонять этот нематериализуемый профиль; исторический план не переписывается.
Структурно корректная конфигурация остаётся доступной для чтения и исправления,
даже если новый semantic guard запрещает её выполнение. Readiness закрывает
запуск, а не весь каталог; повреждённая структура по-прежнему отклоняется.

Продолжение процесса наследует входные значения только из доступного текущему
actor завершённого Run той же session/tenant/target. Новый task не подменяет
эти значения. Каталог, single read и command проверяют их против текущей
опубликованной схемы; добавление несовместимого обязательного поля закрывает
продолжение. Кэш целевого профиля не кэширует eligibility чужого input.
Preview показывает те же унаследованные значения; иной caller input отклоняется.
Новая owner-транзакция сохраняет их immutable snapshot прежним LaunchRun путём.
У очереди и interrupt порядок принятия сообщений может отличаться от порядка
исполнения; текущая задача определяется exact submission pin, а не последним
сообщением истории. Предыдущая история и исходные task bytes не переписываются.
Continuation после result gate получает в свежем prompt snapshot последнее
состояние/решение gate точного текущего шага/root. Старое сообщение ребёнка
«ожидает решения» не заменяет этот owner read. Проекция исключает integration
invocations и не выдаёт полномочий: она не разрешает агенту нажать approve,
использовать чужой gate или выполнить send. Unknown/противоречивое решение
закрыто отклоняется; исторические snapshots не меняются.
Диагностический артефакт о недоступном инструменте не является требуемым
бизнес-результатом и не подтверждает его приёмку.

Добавление хода помощника, включая retry и interrupt, проверяет готовность
текущего привязанного окружения до отмены прежнего хода и изменения очереди.
Смена платформенного default image не переписывает опубликованные версии
окружений или исторические inputs. Устаревший pin отклоняется до создания хода;
переход выполняется публикацией новой версии с явным impact/rebind потребителей.
Отказ не создаёт domain event: результат читается через текущую конфигурацию
окружения и список диалогов, очередь остаётся неизменной.

Право помощника использовать configuration tools выводится из принадлежащего
серверу SYSTEM либо PROJECT профиля и точной связки organization/project,
root actor, conversation/session/turn/node/attempt и immutable revision.
Классификация автора события не является источником этого права: проектный
помощник сохраняет автора AGENT, не становится SYSTEM_ASSISTANT и не выдаёт
прав обычному сотруднику. Неизвестный профиль, чужой владелец, несовпадающий
snapshot либо terminal execution закрыто отклоняются. До выполнения эффекта
сохраняется RUNNING; SUCCEEDED/FAILED используют ту же привязку и terminal
fence. Этот путь не заменяет отдельную проверку точного integration grant.

SYSTEM core принадлежит организации, а выбранный в диалоге проект — контекст
экрана, не владелец системного помощника. Непустой context разрешается только
для существующего ACTIVE проекта той же организации. Для PROJECT профиля и
обычного сотрудника сохраняется точная привязка проекта самого агента.
Отсутствующий или чужой context закрыто отклоняется до seal/claim; continuation
закрепляет свежую базу, не переписывая прежнюю immutable историю.

Материализованные `SessionContext` и continuation notice имеют исполняемый
consumer вплоть до фактического provider input, а не только запись в snapshot
или projection. Свежий provider thread получает ограниченную историю как данные;
`thread/resume` не переигрывает уже сохранённую историю и получает только новое
сообщение продолжения, привязанное к текущим revision/session/turn/attempt.
Это сообщение доставляется один раз без дополнительного дублирующего delta;
повтор terminal callback не запускает provider turn заново. Роль сообщения
в истории не становится источником новых полномочий.

`PURPOSE` продолжения может ссылаться на полное текущее задание через
версионированную серверную проекцию его точных размера, digest и
run/node/session/turn/revision/attempt только при доказанной доставке
неизменённых байтов задания в том же provider input. Для предварительного
просмотра используется отдельный prospective вид без выдуманных turn pins.
`INPUT`, ограничения владельца и сохранённая история не усекаются ради
размера notice. Собственный шаблон владельца с `{{.task}}` по-прежнему
получает полное задание; превышение consumer bound закрыто отклоняется.
Исторические sealed snapshots и опубликованные renderer/instruction версии
не переписываются. Связка producer → фактический consumer проверяется одним
синтетическим immutable input для нового и продолженного provider thread.

Свежая root-попытка Workflow, включая Retry в прежней Session, не наследует
выполненность шагов из истории provider, старых callback или `RETRY_OF`.
CP до seal закрепляет свежую owner-проекцию current root/node/session/turn/
attempt, exact опубликованной версии и состояний её шагов в обязательном
PLATFORM `CONSTRAINTS` существующего snapshot. При continuation проекция
перечитывается из текущего owner состояния, а не копируется из прошлого ввода.
Same-root continuation определяется проверенным `CONTINUES`, а не наличием
истории; завершённые шаги этого root не повторяются. Каталог до первого child
result может быть пустым: это само по себе не blocker первого допустимого шага.
Все данные и guidance остаются advisory, не переносят artifacts/grants и не
заменяют свежий owner frontier при effect. История и прежние immutable pins
сохраняются; ошибочное поведение исправляется поставляемым системным контекстом,
не управляющим сообщением от имени пользователя.

| Проекция Workflow prompt | Сохраняемый инвариант |
| --- | --- |
| Initial claim / новый Retry root | Только шаги нового root; прежний INTAKE не считается выполненным |
| Same-root callback / continuation | Только текущие callback receipts и permitted result pins; прежние SemanticValues не переиспользуются |
| Durable snapshot / rejoin | JSON-представление до seal сохраняет все поля и точные числа; Prompt и оба digest совпадают после roundtrip |
| Неизвестная lineage / несовпадающие pins | Закрытый отказ до materialization; grants, историю и старый snapshot не исправляют |

Инструкции публикации результата поставляются всем новым execution через
версионированный PLATFORM `FILES`, а не исправляющим сообщением пользователя.
В outbox помещаются только запрошенные допущенные файлы; промежуточные архивы,
копии репозитория и материалы сборки остаются вне каталога результата.
Доказательства контрактов публикуются отдельными UTF-8 файлами или валидным
JSON. Неподдерживаемый запрошенный формат не подменяется молча: агент сообщает
ограничение. Финальный текст не доказывает публикацию: owner сохраняет прежний
artifact inspection и атомарный terminal commit. Исторический sealed `FILES`
не переписывается новой инструкцией. Отказ completion callback фиксируется
закрытыми stage/reason/code и доверенными execution pins без сырых payload,
credentials и remote error details; запись отказа не подтверждает commit.

Все роли используют один файловый порядок: уже материализованный attachment
сначала находят по имени и точному пути в FILES/manifest, сверяют исходные
size/digest и читают разрешённым локальным инструментом. Редактирование идёт
только в отдельной рабочей копии INPUT либо собственных файлах workspace;
оригиналы, SESSION/context и знания остаются read-only. Каталог через API
применяется для ещё не материализованных разрешённых файлов, в том числе
результатов предыдущей роли. Локальная копия, имя и справочник не выдают
полномочий и не подтверждают публикацию результата. Локальное чтение не
является запасным транспортом после remote denial/failure.

Этот порядок задаётся платформой для сотрудников, SYSTEM и PROJECT
помощников, включая дочерние execution и warm/continuation, а не отдельным
пользовательским промптом. Cwd — `/workspace/work`; `WORKSPACE.md` содержит
карту каталогов и порядок первых действий. Редактируемые INPUT-копии лежат
в `/workspace/work/input`, прочие рабочие файлы — в `/workspace/work`.
Оригиналы и история — `/workspace/input`, контекст — `/workspace/context`,
знания — `/workspace/knowledge`; защита обеспечивается read-only mounts
и проверкой путей, а не только инструкцией. Пользователь называет файл или
задачу; агент самостоятельно разрешает имя по текущему дереву и manifest,
при неоднозначности различая происхождение и версию, без угадывания ID.
При работе с исходниками агент сначала смотрит разрешённое дерево/индекс,
затем полностью читает нужные файлы. Полное byte-integrity/EOF сканирование
пакета и содержательное чтение — разные доказательства; весь репозиторий
не выводится в prompt автоматически. Явное требование полного чтения
конкретного источника сохраняется. Разрешённое локальное чтение до EOF
не требует повторного remote read_file или вывода каждого байта в tool response.
Справочник не выдаёт Git/network/credential/publication grants и не делает
локальные файлы устойчивыми результатами. После join прежнего provider
следующее исполнение заново создаёт рабочую область и копии из pinned
оригиналов; результат до terminal сохраняется разрешённым artifact/Git path.

Полные current `RuntimeRevision.instructions` доставляются native
`turn/start.additionalContext` каждого хода: numbered application entries
сохраняют exact UTF-8 bytes одного snapshot, а отдельный application entry
назначает его SHA256/bytes/chunk count и заменяет прежние runtime instructions.
Каждое значение ограничено3000bytes до provider truncation1000approx.tokens
(4000bytes в закреплённом upstream); порядок задают закрытые
числовые ключи, соединение без разделителей воспроизводит весь исходный текст.
Model/reasoning pins передаются прежними explicit turn fields. В Codex0.160 изменение
`thread/resume.developerInstructions` не заменяет generic developer message
сохранённой истории. Поэтому thread/start/resume получает только устойчивую
инструкцию привязки, без задачи или прав attempt; полная текущая инструкция
доставляется application context developer fragments и явно заменяет старые
runtime instructions. CollaborationMode не используется для этого: model
catalog может предпочесть встроенный preset и проигнорировать custom text.
Experimental API handshake нужен только для закреплённого additionalContext
и не расширяет реестр методов, tools или серверные grants. Файл
`/workspace/AGENTS.md` сохраняет точные
байты для локального чтения и provenance, но не доказывает доставку модели:
parent вне discovered project root может не читаться, а provider doc budget
может обрезать файл. Встроенная base Codex не заменяется; старое содержимое
rollout не становится текущими инструкциями. Дополнительный continuation
notice может содержать только exact opt-in ссылочную проекцию свежей базы;
полные main instructions/core pins сохраняются и доставляются отдельно,
исторические snapshots без marker материализуются неизменно.

Транспортный JSONL frame app-server имеет отдельный ограниченный бюджет8MiB:
полный terminal aggregate может быть больше квоты отдельного смыслового поля.
Это предел платформы, не обещание upstream максимума. Очередь8frames сохраняет
прежний предел64MiB queued raw bytes; scanner и единственный pending frame
ограничены отдельно. Это не верхняя граница RSS: transient decoder copies
имеют собственную зависимость от bounded frame. Semantic1MiB, final/published
limits, strict JSON/identity/authority checks и message count100000 остаются
неизменны. Более крупный кадр закрыто отклоняется, без усечения либо выдачи
непроверенного payload. Stop/full-queue сохраняет cancel/close/join.
Логи сообщают только закрытый stream_error subtype; public diagnostic не
приобретает сырых parser/reader ошибок. Retained rollout item не равен stdout
wire frame: его размер не доказывает конкретный rejecting guard.

| Файловый путь исполнения | Граница и результат |
| --- | --- |
| Уже материализованный источник | Точные текущие FILES pins и проверка оригинальных байтов; обязательный источник читается до EOF, без повторного remote чтения того же вложения |
| Отсутствующий локально результат другой роли | Собственный разрешённый каталог, полная пагинация и проверка происхождения; parent entry_ref не переиспользуется |
| Невалидная форма entry_ref | Закрытый локальный отказ до metadata RPC; FILE_INPUT_INVALID допускает только одно исправление аргументов по собственному каталогу, не угадывание ссылки |
| Валидный, но отсутствующий/недоступный tuple, отозванная lease либо audit failure | Обычный закрытый отказ без автоматического retry, другого транспорта или расширения grants |
| Cancel/delete/terminal/expiry и новая attempt | Прежние fences и owner-транзакции сохраняются; новые файлы, каталог и pins принадлежат свежему execution, исторические snapshots не переписываются |

Continuation после дочерней работы несёт в самом новом вводе результаты
из точных server-owned callback receipts: run/node, terminal state, безопасное
резюме и ссылки на artifacts. Для Workflow туда же входят ещё не
материализованные шаги закреплённой опубликованной версии. Отсутствующие
шаги без нового завершённого дочернего результата не создают continuation:
иначе пустое ожидание превращается в бесконечный цикл попыток. Resume
provider thread не считается доставкой обновлённого SessionContext;
результаты остаются недоверенными данными, не источником authority.
Прежний USER ввод сохраняется целиком либо полностью исключается вместе с
более старым контекстом при исчерпании JSON-encoded бюджета; обрезанный префикс
не выдаётся за исходное сообщение, JSON или завершённую typed-операцию.

Версия immutable исполняемой спецификации не совпадает с OCC-версией её
наблюдаемого lifecycle. Heartbeat, provisioning и health report могут менять
последнюю, сохраняя identity/digest desired specification. Владелец связывает
полный набор исполняемых зависимостей отдельным fingerprint и монотонной spec
version; изменение и последующий возврат зависимости создают новые refs,
чтобы прежний report не авторизовал новую attempt. Проверка полного runtime
digest у consumer сохраняется; исключать из неё version ради обхода churn
запрещено. Adoption текущего состояния не переписывает исторические snapshots.

Перед каждым новым turn, occurrence attempt и продолжением сервер заново
разрешает итоговую конфигурацию из точного набора активных grants и
авторитетных версий. Запрос не выбирает существующую revision как источник
полномочий.

Callback обычного сотрудника сохраняет каталог делегирования по текущей
`platform.run.delegate` и тем же project/tenant/target eligibility. Входящее
ребро `CONTINUES` само по себе не отзывает эту capability: следующий child
получает новое server-owned ребро от текущего узла и свежую revision.
Отзыв capability закрывает каталог и команду; опубликованный Workflow
по-прежнему предлагает только ещё не материализованные шаги своей версии.

Опубликованный Workflow предлагает только текущий серверный фронт: у каждого
`WAITING_FOR` predecessor exact root состояние должно быть `SUCCEEDED`.
`PLANNED`, `QUEUED`, `RUNNING`, ожидание Gate и любой terminal failure не
разрешают queue-ahead. Owner command после проверки lease/capability/relationship
повторно проверяет этот predicate до materialization, receipt, audit и child
effects; отказ использует прежний typed conflict без автоматического retry.
Каталог остаётся advisory, ordinary delegation не получает Workflow-ограничение,
а nested Workflow использует собственный root. Готовые parallel peers допустимы;
scheduler не обходит зависимости. Ранее материализованный заблокированный граф
этим preflight не исправляется и остаётся под штатным cancel/deadline lifecycle.

Исправление локально неверного входа делегирования не является повтором
исполняемой команды. Различимый typed отказ shape/recipient-step/task/input
возникает до owner command; ограниченная подсказка модели разрешается только
после принятого владельцем terminal FAILED activity receipt. Модель может
исправить вход один раз по текущему server-owned каталогу. Сервер не выбирает
пару за неё, не расширяет targets и не повторяет RPC. Принятый effect,
неизвестный outcome, transport/permission/stale/expiry либо отказ activity
projection сохраняют закрытый отказ без recoverable guidance. Текст ошибки
удалённого сервиса не может приобрести полномочия локального typed маркера.

Снимок неизменяемо закрепляет как минимум:

- session, role/agent, chat/room и привязку provider;
- prompt/profile, policy, playbook/trigger и применимые artifacts;
- только разрешённые точные привязки repository/workspace/integration/credential,
  а не объединение всего проекта;
- image, manifest, source и все component versions/digests;
- предшественника, неизменяемый дайджест входа и собственный дайджест проекции.

Отсутствующая, устаревшая, скрытая, неразрешённая или изменившаяся зависимость
закрыто останавливает создание. Ссылочное событие либо содержит полный
безопасный снимок, либо точные aggregate ID+version и имеет достижимый
защищённый путь чтения/повторного присоединения закреплённой версии для каждого
consumer. Этот путь включает producer/client operation profile, регистрацию
полномочий, generated client и readiness тем же рабочим RPC.

## Расписания и provider account pool

`Schedule` принимает только закрытый набор исполнимых целевых видов, например
точные Agent или Playbook, и закрепляет target version/digest, prompt/artifact,
session policy, room/delivery, maximum duration, overlap/coalesce/misfire,
retry/backoff/dead-letter. `FORBID`, `SKIP`, `QUEUE` и coalesce имеют разные
авторитетные состояния и receipts; скрытое решение планировщика запрещено.

Привязка provider выбирается только из точного role/session grant после
проверки активного состояния, возможностей модели, свежести наблюдения,
usage/limit и revision policy. Поддерживаемые режимы:

- ручное переопределение — проверенный выбор из разрешённого множества;
- `least_used` — overflow-safe сравнение рациональных utilization/limit;
- `weighted` — математически корректный детерминированный алгоритм по
  стабильному снимку/cursor.

Исчерпанные, устаревшие, недопустимые кандидаты и кандидаты с нулевым весом
исключаются до выбора. Одинаковые неизменяемые входы дают воспроизводимый
результат; суммы и перекрёстное умножение имеют доказанные границы либо
используют арифметику с защитой от переполнения.

## Происхождение конфигурации и поиска

У конфигурации, которой могут управлять UI и Git, сервер хранит
`managed_by=ui|git`, идентичность source, неизменяемые revision/commit и
дайджест проекции. Изменение принадлежащего Git объекта через UI закрыто
отклоняется до явной `detach|copy`; Git reconcile изменяет только объект с
совпадающими source и revision и оставляет drift/readback. Клиент не может
изменить `managed_by` обычным update.

Поиск, FTS, vector и иные проекции не создают полномочий. Происхождение
запроса, revision модели/policy, версия исходного агрегата, content digest и
дайджест проекции принадлежат серверу. Устаревший или несовпадающий vector не
участвует в ранжировании; авторитетный PostgreSQL fallback и tenant/owner
eligibility применяются к каждому результату. Delete/terminal без события
имеет tombstone/audit/read path.

## Проверка полноты реализации

Developer до review прикладывает:

1. список защищённых видов и специализированных RPC;
2. граф выполнения и матрицу всех переходов выше;
3. набор authority/grant и источники owner/delegation;
4. границу транзакции, ограждение с одним победителем и область idempotency;
5. terminal event либо точный путь read/tombstone;
6. материализацию producer/client/consumer/profile/readiness/deploy;
7. ручные negative scenarios для чужого owner, stale attempt, replay,
   межсессионного дочернего процесса, expiry и частично материализованного
   графа.

Product reviewer проверяет различимость состояний, достижимость обычного и
операторского жизненного цикла, семантику доставки и соответствие каждого
исхода утверждённому продукту. Security проверяет назначаемые сервером
полномочия, tenant/owner изоляцию, привязку grant, replay/idempotency и отзыв
полномочий на каждом terminal path. Architecture reviewer проходит граф
инициатор→владелец состояния→consumer, все переходы, системные аналоги и полный
контур развёртывания.

Ответ автора и статус `resolved` не являются доказательством. Проверяется
новый точный SHA, исходный путь отказа и все аналогичные команды/виды/профили.

В `Web-first baseline` production-дефект виден из статического достижимого пути
и остаётся замечанием без проверки в живом окружении. Применимые unit,
component, contract, render и lifecycle suites выполняются по `GOV-DOC-003`.
Отсутствие отдельного разрешения на disposable/live среду фиксируется как
`NOT RUN` и не ослабляет найденный дефект.

Связанные документы: `AGENT-DOC-001`, `GO-DOC-001`, `GO-DOC-002`,
`GO-DOC-004`, `GO-DOC-005`, `GUIDE-DOC-003`, `GUIDE-DOC-004`,
`INFRA-DOC-001`.

Задержка проекции exact credential не равна отклонению credential провайдером.
Только специализированный owner READ/NONE health-test может передать закрытый
код ожидания проекции. Owner повторяет его с прежним immutable snapshot,
новой fenced attempt и bounded бюджетом; current package/credential/config,
enabled и workload route проверяются снова. Обычный invocation, WRITE,
auth rejection, digest mismatch и произвольная сетевая ошибка нового retry
не получают. Исчерпание ожидания проекции означает недоступность, не доказанную
невалидность credential; authoritative read path — ledger test/connection.

При сравнении delivery precondition с общим OCC агрегата служебный HEALTH или
изменение отдельного grant не должны молча становиться отзывом неизменённой
конфигурации. Исключение оформляется закрытым набором typed transitions и
owner-transaction append-only receipt exact previous/current version. Исходный
immutable snapshot/version не переписывается, прежний неизвестный drift не
усыновляется, а real revoke/config/credential transitions сохраняют отказ.
Все readers нового receipt вводятся до writers либо в явном maintenance окне;
старый reader rollback после активации не считается совместимым. Для mailbox
точная карта и recovery находятся в `OPS-EMAIL-1037`.

Неизвестный исход внешнего Put не равен отсутствию объекта при одном HEAD404.
Если владелец разрешает отказаться от результата, это отдельное аудируемое
решение по точному снимку, а не подтверждение физической очистки. Прежний
результат нельзя повторно присвоить, принять либо загрузить вторым Put.
Минимальная техническая обязанность с точной identity хранилища, ключом,
хешем, размером и поколением переживает удаление родительского проекта.
Положительная версия/ETag фиксируется до Delete; только отсутствие этой
точной версии после удаления даёт подтверждённый исход. Неизвестный HEAD404
оставляет наблюдение без TTL удаления descriptor. Сбой либо истечение lease
ограничивает попытки и требует нового решения после исчерпания бюджета.
Пример полного пути — `OPS-PROJECT-PURGE-1816`; его recovery read остаётся
доступен владельцу по surviving receipt после удаления проекта.

Защита назначаемых сервером authority/receipt полей охватывает и INSERT, и
UPDATE. Проверка только перехода UPDATE не закрывает прямое создание сразу
терминального либо подтверждённого объекта. Начальный INSERT допускает лишь
каноническое исходное состояние; полномочия и решения возникают исключительно
через специализированную команду владельца состояния.

## Настройка модели обычного сотрудника через проектного помощника ([#1841](https://github.com/codex-k8s/kodex/issues/1841))

`PREPARE_AGENT_RUNTIME_CONFIGURATION` — специализированная операция плана.
Она не меняет профиль сотрудника через `UPDATE_AGENT` и не расширяет
`PREPARE_ASSISTANT_RUNTIME_CONFIGURATION`: у двух команд общий внутренний
примитив публикации, но разные закрытые resolver цели.

Карта контракта: запрос пользователя в PROJECT-диалоге → текущий runtime tool
`propose_assistant_plan` → `RuntimeWorkService.ProposeAssistantPlan` с exact
lease/fence/generation → CP выводит actor/tenant/project из owner-состояния →
ordinary-agent resolver + `agent.manage` → owner-план DRAFT с before/after,
версией агента, runtime-profile revision и account catalog/model pins → UI
редактирует только желаемые model/profile/effort/search/account policy → owner
`validateAssistantPlan` и `applyAssistantPlan` с `If-Match`/idempotency → одна
транзакция CP сохраняет runtime policy/configuration, опубликованный overlay,
receipt и audit. На каждую применённую операцию возникает ровно одно
`AGENT_CHANGED` сотрудника, на переход плана — одно
`SYSTEM_ASSISTANT_CHANGED`, с проверенным owner actor и в той же транзакции.
UI читает план, receipt и актуальную конфигурацию. Следующий turn получает новую immutable
RuntimeRevision штатным materialization путём; активный turn остаётся прежним.
Runtime-controller не получает права самостоятельно применять план.

| Переход | Проверка и результат | Событие / авторитетное чтение |
| --- | --- | --- |
| Подготовка | Действующая PROJECT lease, active exact project, обычная цель без assistant profile, `agent.manage`; новые сотрудники адресуются только реальным ref после CREATE_AGENT | DRAFT плана; штатное событие плана, чтение плана |
| Безопасный READ | Fresh lease + `agent.view`, exact project; только allowlist metadata и pins, без TOML/ENV/credentials | События нет; `AGENT_RUNTIME_CONFIGURATION` |
| Редактирование DRAFT | Owner/target/version/pins неизменяемы; желаемые поля заново валидируются; drift не усыновляется | Новая revision DRAFT; штатное событие плана |
| Validate | Owner access, eligibility, profile/account/model/overlay pins и отсутствие чужого draft проверяются заново | VALID либо проблемы; чтение плана |
| Apply | Повторная авторизация до OCC/receipt, точный snapshot; runtime и overlay атомарны | APPLIED + exact receipt и `AGENT_CHANGED`, либо STALE/CONFLICT; штатное событие плана |
| Потерянный ACK / replay | Прежний idempotency key, текущая owner boundary; прежний receipt без повторных эффектов | События повторно нет; чтение receipt |
| STALE refresh | Явное owner-редактирование создаёт новую revision с новым before/pins; смешанная owner identity отклоняется | DRAFT новой revision; чтение плана |
| Отзыв доступа / archive / чужой проект | Закрытый отказ до изменения runtime; assistant targets всегда исключены | События конфигурации нет; авторитетный read по текущим правам |
| Чужой overlay draft / частичная ошибка | Не перезаписывать draft; rollback всех runtime/overlay эффектов общей транзакции | Публикации нет; чтение плана и текущей конфигурации |
| Отказ от предложения / удаление проекта | До Apply публикаций нет; существующий lifecycle проекта, без отдельной cancel-task/lease/grant этой операции | События конфигурации нет; штатное событие удаления проекта и чтение владельца |
| Retry / следующий turn | Новый runtime snapshot штатного turn; конфигурация не запускает run и не создаёт grant | Штатные события turn; version-pinned RuntimeRevision |

Proto enum, OpenAPI enum, runtime schema, CP command registry и UI editor
изменяются вместе. Порядок обновления: **runtime-controller → CP → API gateway
→ PWA**. Новый RC принимает ровно две закрытые формы безопасного чтения:
legacy `model_configuration` из 5 полей и v2 из 14 полей. У каждой формы
проверяется весь известный состав; смешанные, неполные v2 и неизвестные поля
отклоняются. Legacy READ возвращается без синтеза отсутствующих policy/overlay
pins и не считается полной конфигурацией для планирования. Новая prepare-команда
всегда получает свежие pins от CP, независимо от прежнего read snapshot.

До обновления CP новый RC сохраняет прежний READ. После обновления CP доступны
v2 READ и новая операция; прежний CP закрыто отклоняет неизвестную операцию.
При rollback сначала возвращается прежний CP, затем RC; API/PWA откатываются
согласованно, чтобы новая операция не предлагалась старому CP. Схема PostgreSQL
не меняется. Опубликованные настройки восстанавливаются штатным owner-планом,
а не ручным SQL. Локальные component/unit проверки не являются live-приёмкой.
