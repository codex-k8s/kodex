---
id: OPS-DOC-KUBERNETES-GATEWAY-STREAM-2145
title: Consumer Kubernetes stream в integration-gateway
type: operations
status: approved
owner: developer
version: 1.0.8
updated: 2026-11-21
---

# Consumer Kubernetes stream в integration-gateway

Часть полного [#2145](https://github.com/codex-k8s/kodex/issues/2145).
`KubernetesStreamConsumer` принимает уже начатый typed stream из
`ExecuteKubernetes`. Он не создаёт owner state, новый process, claim, lease,
artifact либо собственный цикл renewal. App владеет original binding,
renewal, input source и остановкой полного invocation graph.

## Сквозной контракт

### Закреплённый FILE INPUT

Для `kubernetes.cp.upload` сотрудник загружает файл штатным artifact upload,
после чего новый ход создаёт новый `RuntimeRevision` и каталог. Исходный
`ResolveIntegrationInvocation` в своей транзакции закрепляет точную запись
этого каталога, artifact revision и SHA256. Подготовка не выбирает новый head
либо более поздний каталог. Допускаются только разрешённые исходным каталогом
`PROJECT`, `WORKSPACE_INPUT`, `RUN_RESULT`; знание artifact ref не выдаёт прав.

| Переход               | Полномочия и владелец                                                                                                  | Квитанция и повтор                                                                                                                            |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Resolve → origin pin  | Исходный root actor, текущая canonical file eligibility, точный immutable catalog/RR                                   | Owner-транзакция; idempotent rejoin сохраняет прежний pin                                                                                     |
| Prepare → PREPARING   | Protected broker `PrepareManagedKubernetesFileInput(binding, childRef)`; owner независимо разрешает artifact и profile | Один source intent до I/O, без public Prepare                                                                                                 |
| PREPARING → READY     | Точная version/ETag/size/SHA, FILE/TAR normalization ≤16 MiB, штатные inspection/AV/prepared-content и typed adoption  | Один PUT; потерянная квитанция требует точного Head/Get и проверки прежних bytes, без повторного PUT                                          |
| Begin → private INPUT | Точный READY source/version/child, текущие original actor/artifact/grant, исходный конечный permission bound           | Native stream не открывается при отсутствующем source либо несовместимой readiness                                                            |
| Claim → DATA+EOF      | Existing INPUT ledger, непрерывный sequence, только после предыдущего ACK; Range ≤32 KiB из закреплённой версии        | Один Claim; неизвестный ответ не выдаёт те же bytes повторно; последний DATA несёт EOF в том же admission                                     |
| ACK → TRANSFERRED     | Existing owner INPUT receipt и точные manifest chunks                                                                  | Подтверждение локальной передачи, не успешного удалённого copy; итог — отдельная tool receipt                                                 |
| Cancel/expiry/unknown | Existing invocation/stream closure; попытка не превращается в подтверждённое отсутствие эффекта                        | Авторитетный List/Get, без отдельного события FILE; очистка только по точной prepared/source receipt после fencing и отсутствия pending INPUT |

После I/O и перед выдачей/Begin owner повторяет текущую artifact eligibility,
сохраняя самый ранний конечный ACL deadline. Смена head не заменяет исходную
ревизию; действующий whole-artifact quarantine/scan deny продолжает закрывать
и закреплённые bytes. На FILE создаётся один normalized immutable object.
INPUT stages содержат только exact source/range/hash metadata и не получают
собственных object key/receipt. Chunk PUT отсутствует; Range выполняется после
фиксации durable Claim attempt. Общий INPUT object cleanup и physical quota
не должны считать эти metadata объектами: один source удаляет его владелец по
точной квитанции после fencing всех связанных deliveries. Отдельного public raw file endpoint, kubeconfig, storage
locator либо caller-selected revision нет.

PWA показывает источник внутри `kubernetes.cp.upload` до terminal tool result.
Source metadata API — authenticated GET
`/api/v1/integration-invocations/{invocationRef}/kubernetes-file-inputs`
и `.../{sourceRef}`. Каждое чтение доказывает original root и текущую видимость;
ошибка чтения или смена owner скрывает прежние metadata. CLI использует тот же
owner session/cookie renewal и bounded HTTPS transport, что локальный PF bridge:

```sh
node tools/dev/managed-kubernetes-file.mjs login --origin https://control.example --session-file /private/session.json --token-file /private/token.json
node tools/dev/managed-kubernetes-file.mjs list --origin https://control.example --session-file /private/session.json --invocation-ref inv_example000
node tools/dev/managed-kubernetes-file.mjs get --origin https://control.example --session-file /private/session.json --invocation-ref inv_example000 --source-ref kfs_example000
make test-managed-kubernetes-file-local
```

Очистка имеет отдельную явную проекцию `cleanupOnly=true` на тех же List/Get:
`CLEANUP_ONLY` содержит только `sourceRef`, `invocationRef`, `cleanup`; полное
чтение `SOURCE` по-прежнему требует artifact visibility. Исходный root с текущим
`run.view` может узнать состояние очистки после отзыва доступа к исходному файлу,
не получая его имени, artifact ref, hash, path либо object coordinates.
Подписанная ревизия 110 обслуживает чтение состояния и автоматическую очистку.
До отдельной доставки новой политики шлюз не объявляет `repairAvailable`.
HTTP-команда возвращает `503 KUBERNETES_FILE_CLEANUP_REPAIR_UNAVAILABLE` до RPC,
интерфейс показывает состояние очистки без кнопки возобновления, а CLI
проверяет доступность через GET перед отправкой команды. Сверка
квитанции чтением остаётся доступна по исходному ключу.

Кандидат политики 111 добавляет только привязку ручной команды к действующему
OIDC producer. `make test-authority-policy-codegen` проверяет этот переход и
побайтное сохранение ревизии 110. Для включения команды нужна отдельная
поставка подписанной ревизии с проверкой её чтения и маршрута владельца.

При доступном `repairAvailable` команда
`POST .../{sourceRef}/cleanup-repair` принимает лишь `{}`, исходный idempotency key
и `If-Match=cleanup.version`; дополнительно требуется текущий scoped
`artifact.upload` с сохранением первого конечного allowance через COMMIT.
Это технический новый цикл существующего cleanup engine, без Prepare, PUT,
повторного INPUT и native reopen. `cleanup.version` отделён от версии source,
а total attempts и repair generation не сбрасываются. Потерянный ACK допускает
только Get с исходным `cleanupIdempotencyKey`: PWA/CLI не повторяют POST.
Квитанция принятия не означает удаления объекта либо освобождения квоты.
Отдельного события нет; авторитетный readback — указанная minimal Get проекция.

```sh
node tools/dev/managed-kubernetes-file.mjs cleanup-list --origin https://control.example --session-file /private/session.json --invocation-ref inv_example000
node tools/dev/managed-kubernetes-file.mjs cleanup-get --origin https://control.example --session-file /private/session.json --invocation-ref inv_example000 --source-ref kfs_example000
node tools/dev/managed-kubernetes-file.mjs cleanup-repair --origin https://control.example --session-file /private/session.json --invocation-ref inv_example000 --source-ref kfs_example000 --version 3 --idempotency-key original-cleanup-key
```

Optional cleanup port отсутствует — minimal read/repair возвращают Unavailable,
полная view содержит `cleanup: null`, а readiness FILE остаётся закрытой.
Наличие DTO и UI не подтверждает реализацию producer/SQL/worker очистки.

FILE-specific owner port и SQL claim/record/complete возвращают tuple штатного
retention consumer; отдельного цикла обработки нет. Reservation и cleanup делят
один mutex по organization/source. Отсутствие object intent подтверждается только
после writer fence и проверки unique prepared intent под этим mutex, включая
промежуток до записи prepared ID в source. HEAD 404 существующего intent остаётся
неизвестным исходом. Claim ограничивает также число обработанных отсутствующих
объектов; исчерпание трёх попыток требует явного repair с сохранением total.
Единственный DELETE admission сохраняется независимо от worker/repair generation.
Первый known tuple выдаёт одно право на DeleteOnce; subsequent tuple с known version
и `uncertain=true` допускает только HEAD точной версии. Для неизвестной версии
HEAD и Record сначала атомарно закрепляют receipt и единственный admission.
Потеря Record/DELETE ACK не выдаёт второе право. Repair открывает лишь новый цикл
сверки: 401/403 и неопределённый 404 остаются UNKNOWN; наличие объекта также не
доказывает NO_EFFECT и не разрешает повторный DELETE.
Подтверждённое удаление единственного объекта атомарно закрывает source, FILE
metadata stages и typed prepared receipt. Оно не изменяет native ACK либо исходный
artifact FK; последний освобождает отдельный canonical retention transition.
Пока общий dispatcher не подключил эти FILE функции и соответствующие исключения
для metadata stages, FILE5 остаётся закрытым. Наличие SQL source не подтверждает
исполнение миграции и cleanup в PostgreSQL.
Фокусный disposable RAM профиль — `make test-managed-kubernetes-file-postgres`;
оснастка использует реальные public upload/RR/Resolve и owner INPUT/cleanup, но
синтетические native handshake/ACK и versioned in-process store. До фактического
запуска его результат — NOT RUN, а не доказательство S3 либо удалённого copy.

Token передаётся только через owned private file существующему session bootstrap;
JSON stdout содержит лишь проверенные metadata. `READY`, EOF и INPUT ACK не
обозначают remote success. Полный профиль требует точных delivery/OUTPUT/INPUT/FILE
revisions `1/1/1/1` на реальном owner path; отсутствие schema, Range, AV, cleanup
или producer закрывает readiness. Локальные CLI/UI проверки не доказывают
SQL migration, S3 durability либо установленный employee/cluster E2E.

Original server claim → `ExecuteKubernetes` → broker owner/native admission →
immutable descriptor → `NewKubernetesStreamConsumer` → один `Exchange` →
protected accepted-output sink → следующий exact output ACK.

Actor, target, identity и полномочия не принимаются в frame. Они разрешаются
broker из исходного binding через owner. Защищённый client использует existing
`kubernetes.stream.exchange` / `kubernetes.cancel` profiles; consumer не
создаёт bearer/mTLS config и не заменяет их caller metadata.

Binding и descriptor копируются. Сохраняются original invocation/fence/generation,
stream ref, каналы и пределы. INPUT/OUTPUT имеют отдельные непрерывные sequences,
общий byte budget и ограниченное время жизни. Sequence ограничена представлением PostgreSQL bigint; произвольного lifetime count нет. Повтор после uncertain RPC запрещён.
Ошибки и diagnostics не содержат payload либо fence.

`ExchangeResponse.authorized_until` присутствует только для непустого output.
Producer берёт минимум фактического owner frame admission и текущего native
context deadline. Credit ограничен5секундами и hard deadline stream. Broker передаёт owner отдельный `native_authorized_until` из deadline,
который фактическая `CurrentStreamAccess` установила для этого Exchange.
Owner выдаёт durable `delivery_ordinal` от1до4 только для OUTPUT SEND;
INPUT и ответы ACKNOWLEDGED/UNKNOWN оставляют ordinal нулевым.
Broker проверяет свежесть, верхнюю границу5s, исходный native deadline и
монотонность ordinal сохранённого frame. Эти данные не становятся полномочиями
сохранённого buffer. INPUT с OUTPUT credit либо ordinal отклоняется до owner I/O.
Пустой
response не получает output credit. Consumer проверяет unknown fields, state,
remote outcome, reason, следующий sequence, digest, канал, размеры и deadline
до передачи единственного frame в sink.

Sink обязателен уже при создании consumer. Он получает исключительное владение
payload, копии original binding/descriptor, exact invocation/stream/generation/binding digest и AuthorizedUntil.
Nil error означает фактически принятый protected stdout/artifact до deadline,
не запись счётчика, discard, помещение в очередь либо detached async upload.
Sink не сохраняет и не объявляет принятыми bytes после истечения credit;
ошибка/cancel очищает отвергнутые bytes. Callback не вызывает consumer повторно.
Новый owner-bound durable admission для поздней выдачи требует отдельного
исполняемого контракта; этот consumer такой допуск не синтезирует.

После возврата sink срок проверяется повторно. ACK отправляется только следующим
Exchange и лишь пока прежний output credit ещё действителен. Expiry/error
блокирует ACK и новые exchanges. Сохранённые bytes сами по себе не дают права
на повторную выдачу. Consumer не заменяет финальный owner Complete.

## Матрица завершения

### Чтение сохранённого OUTPUT владельцем

`ManagedKubernetesStreamOutputService.ReadManagedKubernetesStreamOutput` — отдельная
граница выдачи сохранённых данных. Это часть незавершённого полного #2145;
наличие owner RPC не подтверждает доставленный HTTP/UI либо локальный TCP bridge.

| Звено                  | Контракт                                                                                                                                                                                                                               |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Требование и инициатор | #2145: вывод управляемого kubectl/port-forward; исходный пользователь запуска                                                                                                                                                          |
| HTTP                   | `GET /api/v1/integration-invocations/{invocationRef}/kubernetes-streams/{streamRef}/output?generation=…&afterSequence=…`, существующая owner session; только по одному каноническому decimal query value, без дополнительных selectors |
| Transport              | generated OpenAPI route → control-api-gateway → точный RPC с `platform.integrations.kubernetes.output.read`; проверенные пользователь и tenant, не actor из запроса                                                                    |
| Запрос                 | Только invocation/stream ref, generation и последний полученный sequence; request body целиком связан proof hash                                                                                                                       |
| Owner                  | control-plane разрешает исходный root actor/run, актуальные права `run.view`/`integration.view`, connection/identity/grant, package/input, runtime lease и точный stream snapshot                                                      |
| Хранилище              | Только следующий accepted и owner-ACKed frame; назначенные owner key/version/ETag/digest/size сверяются с реальным ограниченным body                                                                                                   |
| Повторная проверка     | После storage I/O повторяется текущий owner path; immutable pins должны совпасть, первое окно выдачи не продлевается                                                                                                                   |
| Ответ                  | `WAITING`, `FRAME`, `ENDED` либо `INCOMPLETE`; ограниченные bytes и public stage/sequence/digest, без private object locator                                                                                                           |
| Idempotency/version    | Это чтение: owner ledger, ACK, lease, effect и lifecycle не меняются; cursor никогда не перескакивает неподтверждённый frame                                                                                                           |
| Событие                | Domain event отсутствует; авторитетный read path — owner RPC, исходные frame/stage receipts остаются неизменными                                                                                                                       |
| Consumer               | HTTP/локальный bridge должны проверять точный cursor, digest и disclosure deadline; неизвестный исход записи в socket не разрешает replay                                                                                              |

HTTP consumer проверяет закрытые enum/поля, точные origin/generation/cursor,
длину и digest фактических bytes, срок не более5секунд и отсутствие выдуманного
joined end. Ответ имеет `Cache-Control: no-store`. Generation, sequence и counters
передаются decimal strings без округления JavaScript; payload — base64, не HTML.
Чтение не меняет ACK. После синхронной сериализации либо отказа gateway очищает
полученный frame buffer. HTTP source и его synthetic router tests сами по себе
не подтверждают авторизованный browser E2E, локальный TCP bridge или установку.

До записи private bytes gateway требует native `SetWriteDeadline` и ограничивает
его минимумом исходного owner window и срока HTTP-запроса. Cancel прерывает write;
его callback присоединяется до выхода handler. Ответ принудительно flush-ится
при ещё действующем deadline через `FlushError`, включая telemetry wrapper.
Ошибка записи/flush прерывает HTTP response без дописывания второго JSON.
Deadline снимается только после успешного bounded flush; owned encoded buffer
также очищается. Уже отданные transport байты не отзываются, позднее получение
их клиентом не является продлением owner window. Реальная TLS-оснастка HTTP/1
и HTTP/2 задерживает передачу до истечения owner/request deadline и проверяет
cancel; это transport-регрессия, не установленная пользовательская приёмка.
Контракт проверен через Context7 Go `ResponseController` и исходники закреплённого
Go1.26.10 (`net/http/responsecontroller.go`), в том числе оговорка о buffered writes.

Окно выдачи не более5секунд и не позже текущих owner deadlines и сроков текущих
разрешающих ACL bindings. Для альтернатив одного permission берётся наиболее
поздний конец, для разных обязательных permissions — наиболее ранний; будущая
привязка не продлевает текущий допуск. Проверка после commit использует monotonic
deadline. Семантика `clock_timestamp()` сверена через Context7 с
[PostgreSQL18](https://www.postgresql.org/docs/18/functions-datetime.html): время
начала транзакции не заменяет фактическое время проверки.
Окно не является
native credit и не разрешает новые действия в Kubernetes. Истечение старого credit
не удаляет уже подтверждённые данные: их отдельное чтение проверяет текущий доступ
пользователя к данным Kodex. Новый native RBAC допускает только будущий transport,
а сохранённый read не заявляет новую проверку native RBAC.

| Состояние во время чтения                                           | Результат                                                  |
| ------------------------------------------------------------------- | ---------------------------------------------------------- |
| RUNNING, следующий frame не ACKed                                   | `WAITING`, без bytes и storage Get                         |
| RUNNING, accepted + ACKed                                           | Точный bounded `FRAME` после обеих owner-проверок          |
| Чужой actor/project/generation/stream                               | Закрытый отказ до storage Get                              |
| Parent Stop/revoke до либо во время storage I/O                     | Закрытый отказ; полученный buffer очищается                |
| Неизвестный/missing/corrupt stage                                   | Ошибка без пропуска sequence и без пустого успешного frame |
| Подтверждённый joined terminal, cursor достиг watermark, потерь нет | `ENDED`, только завершение stream, не business SUCCESS     |
| Unjoined/loss либо неподтверждённый хвост                           | `INCOMPLETE`, без выдуманного EOF                          |

После остановки parent этот live read закрыт. Сохранённая история выдаётся через
штатный artifact lifecycle; чтение не возобновляет остановленный запуск.

`make test-managed-kubernetes-output-postgres` воспроизводит отдельный owner-путь
на disposable PostgreSQL18.3 и локальном versioned object store: accepted/ACK,
истечение native credit, чужие ссылки/cursor, повреждённый receipt, Stop во время
Get и истечение ACL во время подтверждения настоящего COMMIT. Owner-бюджет80s;
сборка/seed ограничены прежним общим пределом900s. Эти же component-сценарии
остаются в полном `test-managed-kubernetes-invocation-postgres` с owner-бюджетом300s.
Узкий запуск не подтверждает S3, signed HTTP path либо установленный transport.

### Завершение consumer

| Переход                       | Действие consumer                                                                                                 | Авторитетный результат                                 |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| ACTIVE, валидный frame        | synchronous accepted sink, затем pending ACK                                                                      | owner frame receipt при следующем Exchange             |
| INPUT EndOfInput              | exact sequence, закрытие этого input channel после ACK                                                            | original owner frame ledger                            |
| Terminal без output           | сохранить раздельно local state и remote outcome                                                                  | owner stream receipt; не remote process quiescence     |
| RPC/response/sink uncertainty | остановить новые exchanges, не повторять input                                                                    | existing owner read/reconciliation                     |
| Caller cancel/shutdown        | остановить local admission, отменить и дождаться in-flight RPC/sink                                               | самостоятельного owner transition нет                  |
| Cancel                        | mandatory callback подтверждает exact owner closure; затем один broker Cancel в общем cleanup budget, не более10s | owner cleanup proof + отдельный TransportJoined        |
| Cleanup failure               | не сообщать join либо success                                                                                     | authoritative owner/broker read; elapsed time не proof |

Local transitions не создают domain events. Owner frame admission/ACK и исходный
stream lifecycle остаются единственными владельцами устойчивых receipts/events.
Положительный TransportJoined доказывает local broker reader/writer join;
он не доказывает удалённый exit и не повышает UNKNOWN до успешного output.

App создаёт независимый cleanup context после business cancellation. `Cancel`
сохраняет его remaining deadline для ожидания local lane, owner closure и broker
RPC; истёкший budget не начинается заново. Callback закрывает exact исходный
invocation через purpose owner Cancel. Потерянный ACK допускает один metadata
read; RUNNING либо malformed reply не доказывают closure. После этого app
проверяет отдельный `TransportJoined`: nil transport error сам по себе не join.
Общий shutdown observer удерживает зависимости до положительного завершения.

На protobuf wire используются `BeginFrame.native_authorized_until=9`,
`BeginFrameResponse.delivery_ordinal=3`, `ManagedKubernetesFrame.delivery_ordinal=6`
и `ExchangeResponse.authorized_until=6`. gRPC producer передаёт проверенные domain
values без синтеза; INPUT с ненулевым ordinal отсекается до domain/owner I/O.
Gateway передаёт exact ordinal вместе с original binding/descriptor и owned
payload в protected sink. Ни ordinal, ни локальный счётчик не разрешают
публикацию artifact. Для неё нужны accepted stages, фактические broker ACK и
terminal/EOF. Проверка protobuf roundtrip покрывает все допустимые ordinal1..4.

## Обработчик приложения

`kubernetesExecution` соединяет actual `ExecuteKubernetes`, renewal/watchdog,
owner metadata, stream consumer и общий cleanup budget. Конструктор требует
protected input source и durable output sink. Nil input не означает EOF;
обработчик не подставляет пустой stdin и не создаёт detached readers.

| Ответ / переход                   | Действие                                                                     | Подтверждение                                                                   |
| --------------------------------- | ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Typed APPROVAL_REQUIRED           | bounded wait, fresh owner read, повтор broker admission того же binding      | original plan и current owner; отдельный допуск каждого effect остаётся в Begin |
| Typed RECONCILIATION_REQUIRED     | только owner aggregation/readback                                            | второй Execute не отправляется                                                  |
| Bounded completed result          | очистить локальную копию bytes после owner settlement                        | только owner terminal receipt                                                   |
| Stream ACTIVE                     | protected input poll, exact Exchange, synchronous output sink, следующий ACK | accepted output и owner frame ledger                                            |
| Stream terminal                   | owner settlement, owner closure, broker Cancel/join                          | terminal state и TransportJoined проверяются отдельно                           |
| Cancel до старта / истёкшая lease | owner cleanup без broker Execute                                             | exact closed receipt                                                            |
| Cancel известного stream          | остановка, owner closure, broker join                                        | обычная отмена не повышает remote UNKNOWN до SUCCESS                            |
| Malformed response / неясный sink | сохранить ошибку после cleanup                                               | cancellation не скрывает protocol failure                                       |
| Complete RUNNING                  | закрытый отказ                                                               | compound phase consumer ещё обязателен; local cursor не создаётся               |

Factory приложения пока не объявляет этот путь READY: реальный protected input
consumer, S3 accepted-output storage и owner phase advance должны быть подключены
и квалифицированы в полном unit. Наличие callback либо локальный synthetic sink
не доказывает готовность доставки. Публичная локальная проверка композиции:
`make test-managed-kubernetes-gateway`; она не использует live target или credentials.

## Приёмка

Focused race tests проверяют original pins, actual accepted bytes до ACK,
malformed/expired credit, combined limits, EOF, no replay после uncertainty,
отсутствующие callbacks, cleanup ordering и actual in-flight join.
Отдельные broker tests проверяют сохранение actual minimum credit от
`OutputWindow` до protobuf response. Эти проверки не подтверждают готовность
app/runner sink, signed108, deployed target или всего047. До подключения
protected stdout/artifact sink и authoritative closure callback рабочий consumer
не создаётся. Внешние cluster effects в этой проверке отсутствуют.

Проверены Context7 `/grpc/grpc-go`: официальные cancellation example и
client anti-patterns/deadline guidance. Отмена context прекращает RPC, но
не заменяет отдельный owner cleanup proof либо remote exit receipt.
Источники: [cancellation](https://github.com/grpc/grpc-go/blob/master/examples/features/cancellation/README.md),
[client guidance](https://github.com/grpc/grpc-go/blob/master/Documentation/anti-patterns.md).

## Остановка инструмента до создания потока

Требование #2145: инициатор должен иметь возможность закрыть отдельный вызов,
ожидающий Human Gate, не отменяя весь запуск. Поток ещё может отсутствовать.
Источник полномочий — проверенный actor context CAG и закреплённый исходный
инициатор в owner graph. Ссылки из запроса лишь выбирают объект.

| Сценарий                 | Полный путь и инвариант                                                                                                                                                                                                                                                                  |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Список инструментов узла | UI → GET `/api/v1/runs/{runRef}/nodes/{nodeRef}/kubernetes-tools` → `ManagedKubernetesToolService.ListManagedKubernetesTools` → CP → ограниченная страница invocation refs; текущая видимость run/Connection и исходный actor проверяются для каждого результата                         |
| Точное чтение            | UI → GET `/api/v1/integration-invocations/{invocationRef}/kubernetes-tool` → `GetManagedKubernetesTool` → тот же предикат CP; только состояние, version, операция, безопасные target/connection/run/node refs и наличие открытого Gate                                                   |
| Остановка                | UI → POST `/api/v1/integration-invocations/{invocationRef}/kubernetes-tool/stop`, `If-Match` и idempotency → `StopManagedKubernetesTool` → CP: Connection lock → invocation lock → owner eligibility → receipt/OCC → прежнее закрытие графа; API не создаёт приватного gateway principal |
| Ответ и потребители      | Текущее безопасное состояние получает UI; gateway узнаёт terminal через прежние Renew/Get и прекращает дальнейшие эффекты. Readback не утверждает native join или остановку удалённого процесса                                                                                          |

| Переход                               | Результат, событие и read path                                                                                                                                                                                                                                                                           |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| READY, ещё не выданный вызов          | CANCELLED; новые claim запрещены. Одна транзакция сохраняет receipt и audit; отдельного domain event нет, состояние читается через List/Get                                                                                                                                                              |
| RUNNING, ожидание Gate до Begin       | CANCELLED; Gate, уведомления и зависимости закрываются прежним trigger. Только соответствующий human wait снимается, соседние Gate/Run не отменяются. Для уже завершённого исходного хода прежний scheduler атомарно создаёт ровно одно CONTINUES и TURN_QUEUED; для активного хода List/Get авторитетны |
| RUNNING после начала эффекта          | При неподтверждённом эффекте UNKNOWN_OUTCOME; подтверждённые receipts не меняются. Новые grants/frames закрыты, native join требуется отдельно; List/Get, без нового domain event                                                                                                                        |
| Terminal, включая lost Stop ACK       | Тот же key/intent возвращает актуальное состояние после повторной eligibility; другой intent с прежним key отклоняется. Новый key требует текущую version и не меняет terminal. Receipt/audit одного принятого key атомарны; domain event отсутствует                                                    |
| Отозван execution grant/credential    | Чтение/закрытие остаются возможны при текущей видимости run/Connection и исходном actor; новых execution прав Stop не выдаёт                                                                                                                                                                             |
| Чужой actor/project, потеря видимости | NotFound до проверки version/key и раскрытия состояния; root admin или разрешивший Gate не становятся владельцами личного подключения                                                                                                                                                                    |
| Конкурентный APPROVE/Begin/Stop       | Общий порядок блокировок сериализует вызов. Если Begin уже принят, Stop сохраняет неопределённый хвост; после Stop новый Begin закрыто отклоняется                                                                                                                                                       |
| Deadline/lease expiry, retry          | Старый вызов не оживает; Stop не выдаёт integration lease/attempt. Допустимое продолжение использует новый business turn с новым RuntimeRevision. Поздний readback показывает только сохранённое состояние                                                                                               |

Это контракт текущей доработки, не объявление установленного пути READY.
До приёмки обязательны owner PostgreSQL, transport/negative tests, регистрации
клиентских операций и authority108, UI/browser и проверка установленного candidate.

Проверка завершённого исходного хода охватывает parent и child Gate, Stop с повтором
одного ключа и фактический claim следующего хода той же сессии. UNKNOWN_OUTCOME
не создаёт автоматическое продолжение. Для child Gate используется точная ссылка
закрытого в этой транзакции Gate, а не произвольное прежнее решение.

Панель инструментов в деталях сессии показывает ожидающий вызов ещё до создания
stream, включая parent и child Human Gate. Stop отправляется один раз с текущими
version/idempotency; потерянный ответ проверяется одним точным Get. Смена узла или
владельца отменяет запросы и отбрасывает поздние ответы. Пагинация ограничена,
кнопка закрытия не обещает native process join.

Для проверки используются unit-тесты `kubernetes-tool.test.ts` и синтетический
браузерный сценарий `kubernetes-tool.synthetic.spec.ts`: настоящий Chromium,
перехваченный API, широкий и узкий viewport, обычный ответ и потерянный Stop ACK.
Это локальная проверка интерфейса, а не установленного authenticated пути.
Production build сохраняет существующее предупреждение о размере chunks.

Через Context7 проверены [Vue watchers](https://vuejs.org/guide/essentials/watchers.html)
и [Playwright CLI](https://playwright.dev/docs/test-cli). Выбор локального профиля
Playwright 1.61.0 дополнительно сверяется его `--list`; latest docs не заменяют
проверку установленной версии.

Если исходный ход завершился в ожидании Gate, а Stop поступил после APPROVE,
историческое согласие остаётся APPROVED с прежними version/comment. Канонический
контракт результата передаёт отдельно исходное APPROVE и последующее CANCELLED;
это не новое разрешение на действие. Для child Gate ссылка выбирается владельцем
из точного invocation/plan/child: открытый Gate предпочтителен, иначе используется
последнее согласованное решение. Обычный reader извлекает ссылку из приватной
неизменяемой задачи, затем повторяет полную проверку lease/lineage/доступа и точное
сравнение задачи и source digest. Caller не передаёт GateRef как authority.

NONE без Gate сохраняет синхронную семантику: живой исходный ход читает terminal
результат; искусственный Gate либо новый ход не создаются. Полный Kubernetes
runtime polling consumer требует отдельной квалификации: generic integration
reader намеренно не обслуживает KUBERNETES_TARGET. Панель Stop этот пробел не
закрывает и не является доказательством сквозного агентного исполнения.

Полный disposable invocation-профиль имеет явный бюджет owner-suite 240 секунд
и внешний предел 260 секунд, оставляющий время на диагностику Go. Проверки
отдельных сценариев сохраняют собственные deadlines; общий профиль ограничен
900 секундами. Бюджет не меняет production lifecycle или execution limits.

Для завершённого исходного хода уже выданный RUNNING Kubernetes claim сохраняет
read/renew во время OPEN child Gate в том же root/project/invocation/plan/child.
Исключение требует exact invocation lookup и WAITING_HUMAN; оно не допускает
начальную выдачу нового claim. Begin независимо требует согласования точного
child Gate. После APPROVE при отсутствии активного agent node root продолжает
ждать результат, как в каноническом parent Gate lifecycle. Это не продлевает
hard deadline плана и не восстанавливает отозванные grant/credentials.

При SUCCEEDED/FAILED после завершения исходного хода completion выбирает последнее
APPROVED решение дочернего действия только из точного invocation/plan/child и
передаёт его прежнему scheduler. При отсутствии такого решения сохраняется
родительский путь; NONE без Gate не получает вымышленного согласования.
В одной транзакции сохраняются terminal результат, receipt, CONTINUES, новый
turn и событие TURN_QUEUED. Повтор completion читает прежний receipt и не
создаёт второе продолжение. UNKNOWN_OUTCOME автоматически не продолжается.
Исторический Gate не переписывается; новый ход получает свежие права и revision.
Receipt неуспешного завершения остаётся в owner-аудите, но не выдаётся сотруднику
как receiptRef доказанного успешного эффекта. Обычный reader воспроизводит ту же
проекцию результата и проверяет её digest. Проверка
`TestKubernetesCompletionContinuationOwnerComponent` включена в публичный
invocation-профиль; её наличие само по себе не означает PASS.

Readiness общего delivery и Kubernetes execution проверяет все обязательные
табличные права раздельно. `TestKubernetesReadinessACLOwnerComponent` выполняет
точные production SQL под отдельной LOGIN-ролью без наследования: SELECT-only
остаётся not ready, полный набор даёт readiness, отзыв каждого из 19 табличных
прав и отдельного EXECUTE закрывает соответствующий профиль; восстановление
возвращает его. Проверяется также реальная семантика прежнего ANY-условия.
Тест использует только disposable БД, случайное имя роли и синтетический пароль.
Текущие credentials, runtime роли и ACL установленной платформы не меняются.

### Чтение результата действующим сотрудником

`runtime-controller` выбирает `ReadManagedKubernetesInvocation` по закреплённому
`kubernetes-managed` grant. Запрос переносит invocation ref и точную runtime
lease/fence/generation/input digest; actor, project и target не назначаются caller.
Owner разрешает immutable RuntimeRevision/session/turn/node/root, проверяет
живую lease/deadline и текущую видимость run/Connection исходного инициатора.
Общий `GetIntegrationInvocation` продолжает исключать Kubernetes.

| Сценарий                                  | Проверка и результат                                  | Изменение состояния / событие                            |
| ----------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------- |
| READY/RUNNING                             | Exact live lease и исходный узел; bounded polling     | Нет, authoritative read                                  |
| OPEN дочерний Gate                        | Отдельный waiting flag; callback возвращает pending   | RUNNING не подменяется terminal                          |
| SUCCEEDED                                 | Exact receipt/effect/input, SHA256 результата, 64 KiB | Нет; принятый effect не повторяется                      |
| FAILED/REJECTED/CANCELLED/UNKNOWN         | Без success receipt и result body                     | Нет; UNKNOWN требует решения                             |
| Другая lease/generation/digest/invocation | Закрытый отказ без generic fallback                   | Нет                                                      |
| Завершённый origin turn                   | Прежняя lease больше не читает результат              | Новый continuation читает прежний canonical outcome path |
| Отзыв видимости run/Connection            | Свежий eligibility, не исторический grant             | Закрытый отказ                                           |

Карта: pinned runner grant → callback private reader → exact registered RPC →
service caller guard → PostgreSQL lease origin → общий original-root visibility
rule → проверенная typed projection → callback pending/result. Query не создаёт
новую task, Gate, receipt или событие. Аудит результата сохраняет прежний
lease-bound tool projection. Нет права на bytes незавершённого stream.

Для PostgreSQL18 через Context7 проверены
[Repeatable Read и row locks](https://www.postgresql.org/docs/18/transaction-iso.html)
и [повтор полной транзакции после serialization failure](https://www.postgresql.org/docs/18/mvcc-serialization-failure-handling.html).
Используется прежний bounded locked-read retry, без повтора внешнего эффекта.
Этот source slice не является установленной приёмкой: общая authority108
registration/render/readiness и live путь полного Kubernetes unit ещё обязательны.

Повторный final read проверяет lease/deadline после блокировки Connection.
Остаток срока из DB clock переводится в консервативный monotonic deadline до
roundtrip и проверяется после commit; ожидание не продлевает authority.
Component case удерживает настоящую Connection lock до фактического истечения
короткой lease и требует пустой NotFound вместо выдачи результата.

Сборка authority108 использует существующий `controlplaneclient/cmd/policygen`:
40 текущих Kubernetes operations получают exact unary request digest, включая
вложенные lease/fence/generation и mutation OCC/idempotency. Отдельные scalar
authority dimensions запрещены; это не отменяет проверок внутри owner transaction.
Прежние475 operation bindings сохраняются.36 методов к control-plane явно
классифицированы как пользовательские либо служебные в его target-owned policy.
Runtime reader остаётся только у runtime-controller; native transport получает
integration-gateway через отдельный producer `secret-broker.managed-kubernetes`
с прежними mTLS, platform worker grant, proof resolver и local issuer.

Этот registry source ещё не является полным release108: capture publisher,
публичные port-forward методы и их consumers добавляются после квалификации.
Профиль gateway в composition root пока разрешает только health execution;
публикация policy сама не включает execution capacity и не доказывает readiness.
До доставки обязательны общий render, проверка issuer/verifier/key delivery
и живой вызов через сотрудника. Политика не применяется к кластеру отдельно
от совместимых компонентов.

## Локальный TCP bridge PF

`tools/dev/managed-kubernetes-port-forward.mjs` использует Node24.17+ и один
KPF codec с PWA; Go/Node проверяют общие binary vectors, включая uint64 выше
точного диапазона JavaScript. Bridge слушает только `127.0.0.1`, без kubeconfig,
произвольного target URL, remote port или чтения browser cookies.

Первый `login --origin <https-origin> --token-file <private-json> --session-file
<new-private-session> [--ca-file <trusted-ca>]` выполняет один существующий
`POST /api/v1/session`. Private JSON содержит только `origin` и `accessToken`; exact origin из файла
сверяется с выбранным HTTPS origin до отправки token. Значения
не передаются через argv/env/stdout. Исходный файл и session file принадлежат
пользователю, имеют private permissions и находятся в каталоге0700. Session
записывается без замены существующей цели. Потеря ACK не повторяет login. Успех требует authenticated metadata readback
тем же cookie/renew client до private persistence и ответа CLI.

`list --origin … --session-file … --run-ref … --node-ref …` выдаёт bounded
metadata page с `nextPageToken`; optional `--page-token` продолжает discovery.
`profile` с точными `--invocation-ref`/`--stream-ref` показывает server-derived
ports/targetPorts, timeout, target/resource и бюджет16active до подключения TCP.
Эти поля не разрешают caller менять target либо создавать собственный mapping.
`forward` дополнительно требует точные `--invocation-ref`, `--stream-ref` и
`--map <approved-slot>:<local-port>` (до16 разных slots, port0 назначает ОС).
Каждый TCP клиент получает отдельные owner-assigned ref/generation через Open.
Выбор stream всегда явный. Протокол должен быть `PORT_FORWARD_SPDY_V1`;
WebSocket v4 отклоняется до listener/native Open. Target mappings не меняются.

CLI ожидает свежий `KubernetesStreamInputView` с25 полями, включая
`maximumStagedBytes`/`maximumStagedObjects`: квоты текущего private storage,
а не ограничение числа кадров за всю жизнь stream. Старый23-field ответ
закрыто отклоняется; при composition обязательна соответствующая owner версия.

Session renewal, cookie rotation и CSRF остаются в прежнем session client.
Его default acceptance profile сохраняет запрет production hostname; отдельный
закрытый `OWNER_LOCAL_RUNTIME` проверяет тот же exact HTTPS origin и host-bound
cookies. Созданный CLI private state содержит `runtimeOrigin`; до первого GET
проверяется exact scheme/host/port, а legacy browser storage state не принимается
runtime profile без этой привязки. Runtime HTTPS adapter требует TLS1.3, точные SNI/CA, bounded headers,
no redirect и positive socket cleanup. В этом контуре нет OAuth client, обхода
SSO или прямого входа в control-plane.

INPUT writers сериализуются по stream OCC. Только authoritative
`409 VERSION_MISMATCH`, возвращённый owner до intent, допускает максимум три
попытки с новым If-Match, теми же key/coordinates/bytes и свежим Get.
UNKNOWN/timeout/5xx не воспроизводят payload: один exact Get original key,
затем closed failure при неизвестном исходе. Local EOF передаёт HALF_CLOSE;
пустого legacy EOF и caller-assigned OPEN/CLOSE envelope нет.

OUTPUT проверяет canonical cursor/digest, ref/generation/slot, native ordinal
и текущий disclosure deadline. TCP bytes очищаются после socket write либо
closed failure; failed/uncertain write не повторяется и закрывает только своё
соединение, сохраняя явный incomplete result. Unknown connection DATA закрывает
bridge: по одному slot чужие кадры не маршрутизируются. Возобновление другого
CLI с историей требует отдельного согласованного cursor/bridge admission;
пользовательский skip-offset не вводится. Обычные последовательные соединения
одного bridge не получают lifetime count cap. Максимум16 — одновременно живые.

SIGINT/SIGTERM закрывает listeners и owned sockets, выполняет bounded exact
owner connection Close и positive local join. Это не whole-tool Stop и не
remote success. Итог отдельно содержит `deliveryComplete`, `nativeConnectionsJoined` и
`afterSequence`; отсутствие native join не становится успешным результатом.

Публичная локальная проверка: `make test-managed-kubernetes-port-forward-local`
(30s, synthetic token/CA, реальные TLS1.3 и TCP sockets, без cluster calls).
Installed owner authentication, общий bridge/owner E2E и cross-CLI resume этим
профилем не подтверждаются; полный #2145 остаётся NOT READY до их квалификации.
Для Node24 через Context7 проверены net/stream backpressure и half-close,
native TypeScript stripping, HTTPS/TLS и AbortSignal lifetime в официальных
[документах Node](https://nodejs.org/docs/latest-v24.x/api/).

Ограниченный ACL исходного actor сохраняется при Resolve, child Gate, Begin и
перед выдачей нового frame credit. Восстановленный позже grant не продлевает
ранее сохранённый effect intent или stream deadline. Context loss остаётся
инфраструктурным отказом; exact late native outcome и hash сохраняются отдельно
от usable result credit, без повторного dispatch. Проверка после COMMIT
отказывает в выдаче нового допуска, сохраняя уже зафиксированное состояние.

`make test-managed-kubernetes-effect-acl-development` использует disposable
composed047 → proposed common048 → shared KUBE health facet → SSH ledger.
Shared facet расширяет существующий KUBE health ledger взаимоисключающими
personal/shared test refs и same-organization FK. Выбор credential остаётся
единственным авторитетным shared selector; отдельный SERVICE не заменяется
personal binding. Native профиль проверяет real shared setup, отказ без
`organization.manage`, typed health claim, private Begin/Complete/replay,
отзыв до результата и shared RuntimeRevision → invocation claim → broker Resolve.
Личные health и infrastructure-failure сценарии проверяются в том же профиле.
Это проверка development source, а не canonical048 upgrade/restore, installed
108/109, полный #2145 READY или разрешение на кластерную установку.
