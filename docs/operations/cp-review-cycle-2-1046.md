---
id: OPS-CP-REVIEW-1046
title: Исправления общего review — owner lifecycle control-plane
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Карта исправлений общего review

Исходный review `e1e844762` остаётся FAIL. Этот документ фиксирует реализацию
в существующем полном CP unit #1046 / PR #1071 поверх нормализованного
`5edc27c6`; прежняя source ancestry не восстанавливается. Ни один пункт ниже
не считается проверенным до отдельного результата на новом SHA.

| Требование / Issue | Инициатор и authority | Внешний путь / RPC | Транзакция владельца, OCC и повтор | Результат и потребитель |
|---|---|---|---|---|
| CFG source / #1083 | Текущий actor, exact project/resource; отдельные image.source.view и image.source.manage | Existing RoleImage list/get/manage и managed history | Source predicate до чтения source и replay; manage также требует image.build; immutable build inputs сохраняются | Metadata без Dockerfile для обычного читателя; разрешённый editor получает source; HTTP/PWA используют server actions |
| Claim progress / #1085 | Controller proof; сохранённый root actor и exact candidate pins | Existing ClaimExecution | Expected candidate failure ограничен savepoint; полный terminal graph фиксируется в owner transaction; инфраструктурная ошибка откатывает batch | Healthy claims и expiry сохраняют прогресс; Run/Graph/Event показывают безопасную причину отказа |
| Delete account / #1089 | Текущий actor и provider account permission | Existing delete/revoke + отдельная queued delete command и impact read | Account lock до OCC/replay; durable delete intent; blockers читаются из Agent/pool/Automation/turn/warm; cleanup exact credential revisions | После фактической очистки DELETED tombstone; default list скрывает, protected get/history/replay сохраняют identity/audit |
| Verify/reauth / #1091 | Actor account permission; broker workload proof | Existing device verify/reauthorize | Verify текущей credential через реальный adapter; reauth новая attempt той же account, exact retry; atomic successful revision swap | LastChecked/readiness/actions; старый pinned turn не получает новую credential; cleanup прежней revision отдельный |
| Assistant context / #1090, #1092 | Caller из проверенной session; общий canonical read каждого ресурса | Existing create/get/list/turn и event/receipt | PROJECT/AGENT/WORKFLOW/RUN/FILE/ENVIRONMENT/INTEGRATION_CONNECTION разрешаются до receipt; project mismatch закрыт; fresh operations | Только доступные name/version/project/actions; исторический context после revoke не раскрывает metadata |
| Draft KEEP / #1084 | Exact broker operation/fence/revision proof | Existing draft completion | No-effect FAILED+KEEP имеет отдельный подтверждённый terminal outcome; не выдаётся за замену credential | Draft/operation/cleanup readback согласован с broker; повтор не выполняет внешнее действие |
| BFF renewal / #1086 | Новый проверенный OIDC bearer той же session identity | Existing authority proof resolution | CP не владеет refresh tokens; sid/session_revision берутся из verified token, не изменяются клиентом | BFF владеет durable rotation/revocation; каждый новый bearer требует fresh CP proof |

## Матрица переходов

| Агрегат / исход | Переход и атомарная граница | Повтор / чтение / отмена |
|---|---|---|
| QUEUED candidate пригоден | Свежая проверка pins/actor/dependencies → immutable RuntimeRevision + lease/grant | Exact attempt/fence; обычный renew/complete/cancel/expiry сохраняются |
| QUEUED candidate ожидаемо непригоден | Отмена частичной подготовки candidate → FAILED полного root graph, закрытие связанных grants/leases/turn | Safe reason в защищённом Run/Graph/Event; следующий tick не выбирает прежний failed candidate |
| Ошибка PostgreSQL/инфраструктуры | Общий rollback batch | Никакого ложного terminal успеха или потерянных claims |
| Delete без blockers | Durable revoke/delete intent → cleanup всех credential revisions → DELETED | Lost ACK читает тот же receipt/tombstone; enable/authorize запрещены |
| Delete с blockers | Обычный delete закрыт; отдельная явная queued команда запрещает новые исполнения и сохраняет pinned активные | Cleanup ждёт освобождения exact consumers; error/retry/expiry не теряют delete intent |
| Pending account без credential | Отмена pending attempt и authoritative отсутствие credential effect → DELETED | Pending DEVICE_CODE допускается; поздний completion не воскрешает account |
| Reauth AUTHORIZED | Exact blockers → новая versioned attempt, прежняя identity сохранена | Повтор живой attempt возвращает тот же challenge; expiry/cancel не заменяет current credential |
| Reauth success | Новая immutable credential становится current атомарно; прежняя получает bounded cleanup | Late/stale attempt rejected; текущие runtime pins не переписываются |
| Verify AUTHORIZED | Проверяется текущая exact credential через adapter; обновляется безопасное наблюдение | Unavailable не выдаётся за AUTHORIZED; без создания фиктивного pending challenge |
| Context create/read/replay/event | Общий owner read каждого вида и свежая проверка operations | Hidden/deleted/foreign context закрыт; сохранённые metadata не служат authority |
| Source permission revoked | Все пользовательские readbacks перестают выдавать source | Build execution сохраняет immutable source по своему workload grant, не по UI permission |

Новые схемы/permissions доставляются forward-only migrations и canonical
generation. Event/read consumers, completion semantics broker и публичные
DTO согласуются с владельцами HTTP/PWA до публикации. Проверки реализации,
общий baseline, повторный review и deployed acceptance пока NOT RUN.

## Согласованные уточнения provider lifecycle

`ListProviderAccountBlockers` обслуживает exact account, kind, query и page.
Owner возвращает только читаемые refs/versions и отдельное число скрытых
блокировок; скрытая ссылка продолжает блокировать удаление. Общие counts
в `ProviderAccount.deletion` не заменяют пагинируемый каталог. Контекстный
digest связывает actor, account/version, deletion intent/version и весь
снимок зависимостей; cursor дополнительно связан с фильтром.

`CancelProviderAccountQueuedWork` принимает не более 64 выбранных Run refs,
digest каталога и обычную mutation. До каждого эффекта проверяются текущее
право account revoke и exact `run.cancel`. Активное выполнение эта команда
не отменяет. В ответе возвращаются текущий account и отдельный исход каждой
выбранной строки; replay не сохраняет отозванные полномочия.

`DELETING` запрещает новые назначения и claims. Существующие exact active
grants сохраняют credential до завершения. `DELETED` наступает только после
очистки всех materialized credential revisions и pending device attempt.
Повтор delete с новой mutation может возобновить исчерпанную cleanup attempt,
но не создаёт новую account identity и не отменяет чужие активные процессы.

Verify авторизованного Device Code создаёт отдельную owner verification
attempt и новый credential-bound remote catalog task. Старое наблюдение
не завершает эту attempt. Scope результата ограничен
`CREDENTIALED_CATALOG_REACHABILITY`; это не provider SLA. Смена account или
credential pins оставляет прежний ответ неприменимым. Reauthorize создаёт
отдельную authorization attempt той же account; успешная смена current
credential атомарна, предыдущие runtime revisions неизменяемы.

## Pending authorization: протокол metadata и очистки

`ObserveDeviceAuthorization` различает `POLL` и `METADATA_ONLY`. Второй режим
проверяет защищённые account, authorization attempt, task и generation;
`materializer_attempt_ref` повторно выводится из account и attempt. Он не
запускает provider polling и не возвращает user code, masked account или
credential material. Допустим только безопасный descriptor произведённой
credential, необходимый для отдельной очистки.

| Наблюдение | Следующая immutable owner task | Эффект broker и подтверждение |
| --- | --- | --- |
| PRESENT с exact UID/resourceVersion | AUTHORIZATION_ATTEMPT | Durable tombstone/fence, cancel/join exact попытки, exact delete; receipt и descriptor credential после join |
| ABSENT_UNFENCED с exact account/attempt/ref | AUTHORIZATION_ABSENCE | Только CAS создания tombstone и повторное чтение под fence; существующий объект без UID/version не удаляется |
| Объект появился одновременно с absence fence | Новая generation с новым PRESENT snapshot | Conflict; прежняя immutable task не переписывается |
| CONFIRMED_ABSENT | Повтор terminal read/cleanup по exact binding | Допустим только после durable tombstone; отсутствие Secret само по себе не доказывает завершение |
| Produced credential после fence/join | Отдельная CREDENTIAL cleanup task | Account не становится DELETED до exact удаления этой credential |
| Restart после fence до receipt | Повтор exact task/generation | Tombstone запрещает recreate и late Complete; descriptor orphan сохраняется для очистки |

Cleanup request использует закрытый `target_kind` и ровно один descriptor.
Kind, account, task, generation и все object pins входят в exact Proto request
digest. Неизвестный kind, смешанные descriptors и чужая account закрыто
отклоняются. Metadata RPC не подменяет owner authority и не завершает deletion.
CP сохраняет прочитанные pins и создаёт отдельную попытку очистки с новым
поколением; поздний ответ прежней попытки не меняет её snapshot.

На момент source checkpoint `8dd6f0e5` исполняемый broker companion и полный
PostgreSQL delete lifecycle имели исход NOT RUN; один успешный compile не
подтверждает удаление. Полный совместный owner lifecycle остаётся незавершённым.

## Явная проекция managed RoleImage source

Для `ROLE_IMAGE` owner заполняет optional
`ManagedConfigurationRevision.source_available` (поле 13) и
`ManagedConfigurationSet.source_editable` (поле 12). Presence обязательна в
current/history/binding/receipt проекции этого kind. Для других managed kinds
поля отсутствуют, сохраняются их прежние проверки.

`source_available=false` означает metadata read: content и validation diagnostics
пусты. `source_editable` отражает дополнительный допуск `image.source.view` и
`image.source.manage` по точному связанному RoleImage instance; он не заменяет
общую authority команды, `managed_by`, OCC и idempotency. До создания recipe
используется существующий project либо organization scope. Project self-query
не подменяет instance rule для существующей recipe.

Source generation и компиляция трёх затронутых пакетов прошли локально на WIP
дереве; этот additive контракт отдельно не заявляет завершённый owner baseline.

## Свежие полномочия повторного запуска и продолжения (#1094)

`RetryRun` и `AddSessionTurn` разрешают сохранённую цель AGENT/WORKFLOW у владельца
и проверяют тот же `agent.launch` либо `workflow.launch`, что обычный `LaunchRun`,
до чтения idempotency receipt и проверки версии. `run.view` и locator сессии
не предоставляют право запуска. Для Workflow не добавляется отдельное требование
`agent.launch` координатора. Отзыв target permission закрывает также точный replay;
`Run.nextActions.RETRY` использует тот же допуск цели.

Если первый запуск завершился до первого claim и не создал RuntimeRevision,
точный `pgx.ErrNoRows` при поиске predecessor означает новую первую материализацию.
Другие ошибки PostgreSQL остаются закрытыми. Это изменение существующего RetryRun;
будущий RetryRunNode из #1016 в пакет не входит.

## Промежуточная проверка исполняемого пакета

Приведённые результаты относятся к незакоммиченному рабочему дереву после
`727f90ca`, а не к завершённому exact-SHA baseline:

- Полный PostgreSQL Bootstrap + Avatar: PASS, 35,491 с и 0,505 с.
- Retry/continuation: PASS, 0,537 с; разрешённый запуск, отсутствие launch,
  устаревшая версия и отзыв права перед точным replay.
- Poison claim с RUNNING/PLANNED siblings и здоровым кандидатом в том же batch:
  PASS, 0,612 с.
- Typed no-effect CAS cleanup, новая metadata generation и exact failure replay:
  PASS, 0,883 с. Строгий ErrorInfo parser: race PASS, 1,136 с.
- Первый полный race: FAIL только старых ожиданий `nextActions` без DELETE;
  ожидания исправлены, финальный полный повтор ещё NOT RUN.

Исторические full PostgreSQL FAIL сохранены: 20,779 с (несогласованный effort
fixture), 35,702 с (legacy read predicates), 24,561 с (SQL параметры), 37,294 с
(старое ожидание удаления current credential до завершения reauthorization).
Они не объявляются успешными задним числом. Финальные recovery identity,
явное восстановление FAILED deletion и объединённый exact-SHA baseline ещё
должны быть завершены; staging/provider acceptance не запускались.

## Восстановление очистки без потери результата

`CleanupRequest.recovery_identity` (поле 8, source checkpoint `5781d3aa`)
содержит назначенный владельцем stable origin: task ref, generation и отдельный
legacy upper bound. Current task/generation остаются свежим claim. Все поля
входят в canonical request digest. Для новых задач legacy bound равен 0;
миграция существующих credential tasks сохраняет диапазон 1..N, N не более 32.
Origin неизменяем в БД. Successor того же exact target наследует origin,
а новый descriptor после metadata/CAS получает собственный origin.

Потерянный ACK исходного Delete разрешается его прежним idempotency key/OCC.
После `deletion.state=FAILED` owner предлагает новый отдельный Delete с новым
key и текущей версией. Он атомарно создаёт successor с теми же immutable object
pins и origin, закрывает прежний DEAD_LETTER и не разрешает late Complete старого
claim. Это повтор специализированной идемпотентной очистки, а не вывод об
отсутствии прежнего эффекта. Produced credential из восстановленного receipt
создаёт отдельную cleanup task; DELETED недоступен до её завершения.

Только exact ErrorInfo `kodex.provider_credential_cleanup/CAS_SNAPSHOT_CHANGED`
при FailedPrecondition означает отказ до эффекта. Owner фиксирует отдельный
no-effect receipt, назначает metadata successor и наследует ограниченный бюджет
эффектных попыток. Metadata observation сама не расходует этот бюджет.
Остальные ошибки сохраняют обычную retry/dead-letter семантику.

Последний scoped PostgreSQL повтор: PASS 1,881 с, включая AGENT/WORKFLOW retry
и continuation, API-key/device reservation, FAILED deletion successor,
late old completion и отдельную очистку recovered produced credential.
Первый повтор этого расширенного сценария: FAIL 1,738 с из-за одинакового имени
двух fixture Projects; исправлены fixture names. Финальный exact checkpoint
и совместный broker consumer baseline ещё требуют отдельной проверки.

## Уже выданные grants и новые попытки

`DELETING` запрещает новые bindings/claims, но exact действующий runtime lease
сохраняет доступ к закреплённой credential и её специализированному refresh.
Все tenant/root/session/turn/runtime/attempt/generation/digest проверки остаются;
другое поколение не получает доступ. Blockers удерживают credential до закрытия
active/warm lineage. STT не добавляется как новый blocker kind: его immutable
конфигурация сохраняется, но новый credential projection отклоняется, а readiness
возвращает `STT_PROVIDER_ACCOUNT_INELIGIBLE`.

Scoped expiry и active projection checks прошли (0,47 с и 0,52 с). Выбранный
совмещённый subset в целом FAIL 1,838 с: после успешных STT negative assertions
старый managed fixture потребовал environment consumer, который создаёт полный
Bootstrap. Этот частичный запуск не считается успешным; полный повтор обязателен.

## Проверки consolidated checkpoint

На чистом `838ede35d9e04b8e714a9e3f47ac7c9fc95ed3e4` локально прошли полный
`make test-control-plane-postgres` (Bootstrap 37,067 с, Avatar 0,457 с),
полный `go test -race -p 1 ./...`, `go vet -p 1 ./...` и `go build -p 1 ./...`
модуля control-plane. Эти результаты включают последние recovery, expiry,
active grant и STT negative assertions.

`make check-sql-boundary` первоначально завершился FAIL: девять новых файлов
не содержали обязательный комментарий `-- name:`. Добавлены только эти заголовки,
SQL выражения не изменены; повтор PASS. Затем PASS: Proto lint/build/canonical
generation/clean replay, authority policy 73 codegen/replay, authority ABI render,
worker projections обоих профилей и web-only release render. Buf использовал
предусмотренные exact local plugins после ограничения частоты remote plugins.
Модуль controlplaneclient также прошёл полный race/vet/build.

Это локальные инженерные проверки. Они не заменяют объединённый baseline,
повтор общего product/security/architecture review на одном новом SHA,
ручной шлюз владельца и разрешение на deployment. Живые provider, staging и
production проверки не выполнялись. Source-only checkpoints выше сохранены
как исторические, а не как отдельные завершённые implementation PR.
