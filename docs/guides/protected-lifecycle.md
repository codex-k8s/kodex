---
id: GUIDE-DOC-006
title: Защищённые агрегаты и граф фонового выполнения
type: guide
status: approved
owner: architect
version: 1.1.12
updated: 2026-10-08
---

# Защищённые агрегаты и граф фонового выполнения

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
Каждый путь terminal/cancel/delete/retry/expiry повторяет одну проверку owner и
отзывает все grants/leases/claims в той же PostgreSQL-транзакции. Путь чтения
скрывает claim как активный, если его авторитетный граф уже завершён, даже до
фоновой очистки.

Гонка claim/renew/cancel, owner click/expiry и complete/retry разрешается
row/OCC/fence-моделью с одним победителем. Повтор с тем же idempotency scope и
request hash возвращает сохранённый результат; новый эффект не создаётся.

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

Право помощника использовать configuration tools выводится из принадлежащего
серверу SYSTEM либо PROJECT профиля и точной связки organization/project,
root actor, conversation/session/turn/node/attempt и immutable revision.
Классификация автора события не является источником этого права: проектный
помощник сохраняет автора AGENT, не становится SYSTEM_ASSISTANT и не выдаёт
прав обычному сотруднику. Неизвестный профиль, чужой владелец, несовпадающий
snapshot либо terminal execution закрыто отклоняются. До выполнения эффекта
сохраняется RUNNING; SUCCEEDED/FAILED используют ту же привязку и terminal
fence. Этот путь не заменяет отдельную проверку точного integration grant.

Материализованные `SessionContext` и continuation notice имеют исполняемый
consumer вплоть до фактического provider input, а не только запись в snapshot
или projection. Свежий provider thread получает ограниченную историю как данные;
`thread/resume` не переигрывает уже сохранённую историю и получает только новое
сообщение продолжения, привязанное к текущим revision/session/turn/attempt.
Это сообщение доставляется один раз без дополнительного дублирующего delta;
повтор terminal callback не запускает provider turn заново. Роль сообщения
в истории не становится источником новых полномочий.
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
