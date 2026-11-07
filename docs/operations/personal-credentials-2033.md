---
id: OPS-DOC-PERSONAL-CREDENTIALS-2033
title: Персональные подключения и допуск запуска
type: operations
status: draft
owner: developer
version: 0.21.0
updated: 2026-11-07
---

# Персональные подключения и допуск запуска

Контракт [#2033](https://github.com/codex-k8s/kodex/issues/2033) уточняет
[DOM-MC-007](../domains/integrations-approvals.md) и
[GUIDE-DOC-006](../guides/protected-lifecycle.md). Это матрица реализации
одного полного unit. Локальные проверки и установленная функциональность
разделены: ветка содержит реализацию, а её установка ещё не подтверждена.

## Текущая реализация

Локальная ветка unit содержит доменный выбор credential и полный сбор известных
проблем preflight, owner-qualified materialization identity, OCC/generation
переходы ротации и отзыва. Shared и personal metadata используют общий guard
точного Secret descriptor. Проверки домена, включая чужого owner/tenant,
отсутствие fallback, устаревшие proof pins и непроверенные возможности, а также
`go test ./...` control-plane прошли локально. Хранение собственной привязки,
metadata-only commands create/configure/revoke, read-only receipt reconciliation
перед материализацией и forward034 migration проверены на отдельном PostgreSQL18.3
с Go1.26.6. Native fixture проверяет двух пользователей, одинаковый operation key,
потерянный ответ после реального commit, изоляцию, отзыв и immutable history.
Команда: `make test-personal-integration-credentials-postgres`.

Добавлены собственные RPC и HTTP get/create/configure/revoke, безопасная проекция
без owner/Secret descriptors и повторного значения ключа, отдельные Connection
и binding versions, no-store/ETag. Политика явно видна в общем Connection read/list
и realtime snapshot. Проверены exact RPC dispatch, closed enums, вход без owner
selector, неизвестные поля, write-only input и отказ неполного ответа владельца.

Отдельная команда `SetIntegrationCredentialPolicy` требует `integration.manage`,
точный browser operation и OCC; self credential operations её не заменяют.
Смена требует отключённого Connection без включённых grants, незавершённых
tests/invocations/deliveries и активных root snapshots именно этого Connection.
Общие и личные revisions сохраняются; переключение не копирует ключи между ними.
Общая configure/test команда закрыта в персональном режиме; materializer общего
ключа не вызывается. Native fixture теперь выбирает режим штатной owner-командой,
а не ручной правкой SQL. Проверены отказ включённому Connection, viewer denial
и повтор сохранённого policy receipt. Реальная конкуренция смены политики с
root admission и все виды активных зависимостей требуют отдельной проверки.

Gateway-only Begin, одноразовые receipts, положительный personal runtime и
конкурентный revoke проверены локально на disposable PostgreSQL. Ниже описаны
launch, workflow/delegation и восстановление owner в отдельном OS process.
Закрытые профили схемы034 и их backup-reader вариант проверены отдельно.
Живые provider tests, immutable images и installed acceptance остаются открытыми.
Выбор policy использует отдельную owner command; server launch guard возвращает
полный список проблем. Эти части передаются одним unit, без отдельного PR по слою.

Каталог `GET /api/v1/me/integration-connections` и соответствующий RPC читают
видимые текущему пользователю Connection с его optional binding одним bounded
SQL-запросом в read-only snapshot. Отсутствующая привязка не создаётся и не
заменяется shared credential. Actor/filter-bound cursor не даёт полномочий;
на каждой странице заново действует `integration.view`. Размер страницы до100,
поиск до200 символов, owner query до5s. Native fixture проверяет две Connection
и двух пользователей, разделение ключей, пагинацию, чужой/изменённый cursor и
исчезновение строки после штатного отзыва ACL. Это не двухпроектная live-проверка.
Отключённый adapter не скрывает собственную сохранённую привязку: она остаётся
доступной для отзыва с `available=false`. Потеря `integration.view` скрывает её
из каталога и закрывает адресное чтение; это отдельная проверка доступа.

Подготовлена доменная модель проверки с immutable self/generation/revision/
definition/configuration snapshot. Поздний результат после revoke/rotation или
смены настроек отклоняется. Подтверждаются только явно разрешённые read probes;
health success не подтверждает write capability. Ненаблюдённый expiry остаётся
отсутствующим, transport failure возвращает `CHECK_REQUIRED`, подтверждённый
auth rejection — `INVALID`. Прямые domain tests и DB guard retry-перехода
проверены. Durable test queue, одноразовый worker admission и gateway health
execution теперь подключены в локальном unit. Этот результат не означает
выполненную проверку корпоративного provider либо installed runtime.

## Интерфейс «Мои подключения»

В разделе интеграций есть отдельная вкладка и ссылка
`/integrations?section=personal`. Она использует собственный bounded self catalog,
не требует успешной загрузки общего административного каталога и не принимает
user/Secret selector. Отображаются policy, состояние личного ключа, известный
expiry и явно подтверждённые/отклонённые операции. Неизвестные права не выдаются
за подтверждённые. `PROJECT_SHARED` не предлагает сохранить личный ключ;
сохранённый ранее ключ можно отозвать после смены policy либо отключения adapter.

Добавление создаёт собственную metadata binding, затем записывает write-only
значение через generated SDK, CSRF и отдельные Connection/binding OCC. Поле
пароля очищается до ожидания ответа, при отмене, смене подключения, pagehide и
закрытии owner context. Значение не сохраняется в Pinia, URL, storage либо
recovery metadata и не возвращается readback. После сетевой ошибки нового write
не будет до свежего чтения; автоматического повторения save/test нет. Ошибка
после create не маскируется успехом configure. Поздний ACK/ошибка прежнего owner
либо закрытого компонента не меняют новый интерфейс.

Проверка ставит существующую owner job в очередь. `CHECKING` закрывает повтор
проверки и замену до получения результата; обновление страницы читает состояние
без повторного probe. Отзыв остаётся доступным. Квитанции UI проверяют точные
Connection/policy pins, binding version и закрытый результат действия. Успех
health probe показывает только подтверждённые операции, а не выдуманный полный
набор прав.

Локальные model/API tests проверяют owner lifetime, потерянный ACK, no retry,
раздельный OCC, очистку поля, UTF-8 лимит, pagination и malformed receipts.
Synthetic browser scenarios выполняют add/test/status/replace/revoke, transport
failure и отзыв недоступного adapter в Chromium/Firefox/WebKit. Это HTTP fixtures,
а не живые provider tests, обычный SSO либо installed integration acceptance.
Начальный server launch guard и переход из списка проблем к настройке реализованы
локально. Положительный direct-agent runtime, одноразовый Begin RPC и описанные
ниже workflow/resume пути проверены на disposable PostgreSQL; установленный
workflow с внешним provider требует отдельной приёмки.

## Выбор режима ключа в интерфейсе

Карточка Connection показывает текущий режим и явную настройку «Чей ключ
использовать». Для поддержанного credential-based MANAGED_MCP adapter и текущего
права UPDATE можно выбрать PROJECT_SHARED либо INITIATOR_REQUIRED. Начальный
read заново разрешает Connection; stale package/version или потеря права закрывают
изменение. Форма не записывает ключ, не копирует личные credentials в общий
источник и не меняет настройки без явного сохранения.

Смена возможна на отключённом Connection без включённых grants. Сервер дополнительно
проверяет активные root pins и незавершённые effects; интерфейс не отключает их
автоматически. Dedicated SDK command передаёт только policy с Connection OCC,
CSRF и новым idempotency key. Receipt сверяет точные ref/version/package pins и
новый режим. Потерянный либо malformed ответ требует явного readback: write не
повторяется. Уже зафиксированное после lost ACK изменение обнаруживается чтением.
Закрытие окна, обновление строки и смена owner context ограждают поздние ответы.

Форма объясняет разницу между явно разрешённой service identity и ключом
корневого инициатора, а для personal mode даёт ссылку в «Мои подключения».
Synthetic browser fixtures проходят оба направления смены policy, lost-response
readback без повторной записи и отказ по свежему enabled состоянию. Проверка
не заменяет owner server/native guards, JWT либо installed acceptance.

## Допуск конфигурации и launch preflight

Готовность выдачи grants отделена от ключа инициатора. Новый invoker helper
`integration_connection_configuration_ready(uuid)` сохраняет прежний shared
`CONNECTED`/credential guard. Для personal policy он проверяет текущую включённую
конфигурацию поддержанного adapter, не требует общей credential и не записывает
общий CONNECTED. `integration_grant_admission` и runtime eligibility используют
этот configuration guard только для personal policy; прежний shared CONNECTED
branch сохранён. USE не выдаёт credential: preflight и per-call Begin отдельно
проверяют точного инициатора и его ключ. Forward034 сохраняет полный `pg_get_functiondef`,
OID, argument names и ACL исходной admission function, включая исторические
package branches. Native migration сравнивает именно это сохранённое тело,
а не сокращённую копию его прежней сигнатуры.

Начальный `LaunchRun` в owner transaction читает personal requirements до старой
фильтрации runtime, которая иначе скрыла бы неподготовленные подключения.
Прямой агент проверяет включённые personal grants; workflow берёт только
`RequiredCapabilityKeys` опубликованных этапов. Coordinator с delegation-only
контрактом не добавляет собственные integration grants. Query ограничена256
уникальными requirements с257 sentinel, targets≤256, encoded target snapshot≤128KiB,
весь reader≤5s. Превышение закрыто отклоняется без частичного positive результата.
Shared-policy поведение запуска сохраняется.

До сохранения Create/UpdateSchedule и включения SetScheduleEnabled владелец
применяет тот же preflight с источником SCHEDULE. Для workflow требования
читаются из текущей опубликованной версии с проверкой закреплённого digest;
личный grant координатора не становится требованием этапа. Входящий маршрут
проверяется при ChangeIntegrationGrant и включении через ChangeAgentGrant:
семантика source выводится из точного опубликованного пакета, а не payload.
Автор конфигурации с READY личным ключом не становится интерактивным инициатором.
INITIATOR_REQUIRED возвращает полный типизированный отказ до сохранения revision,
grant либо события. PROJECT_SHARED с явно разрешённой service identity сохраняет
штатный путь. Отключение, архивация и удаление не требуют личного ключа.

Проверка конфигурации не замораживает будущие grants и опубликованные версии.
Изменение target после настройки может сделать существующую автоматизацию
непригодной; launch и сам вызов независимо закрыто отказывают, без перехода на
ключ автора либо общий fallback. Неявная подстановка последнего пользователя,
владельца расписания или согласовавшего Gate запрещена. Текущий входящий consumer
ограничен поддержанным mattermost.inbound; произвольный webhook не добавляется.

Connection locks берутся одной упорядоченной выборкой до self-binding locks.
Missing binding не создаётся. После ожидания сверяются Connection version/policy,
текущие visibility, published package и configuration digest; canonical domain
preflight собирает все известные проблемы. Readiness не становится grant и
не отменяет необходимость проверки при самом tool call. SCHEDULE/INTEGRATION
не используют личный ключ владельца как неявную service credential. Продолжение
существующей personal session требует её сохранённого owner; право launch на
target само по себе не даёт чужую identity.

Отрицательный результат предшествует созданию session/run. Owner возвращает
один bounded `IntegrationCredentialPreflightDetail` через существующий LaunchRun
RPC: только connection ref/name, capability и закрытый problem enum. HTTP выдаёт
409 `INTEGRATION_CREDENTIALS_REQUIRED`, `retryable=false`, `no-store` и полный
`integrationCredentialIssues`. Unknown/duplicate/partial metadata, unknown Proto
fields, неверный status либо extra details превращаются в безопасный502 без
сырых upstream данных. Actor/Secret descriptor и значение ключа не передаются.

Форма запуска показывает все причины и открывает «Мои подключения» в новой
вкладке: текст задачи остаётся на месте. Generated launch SDK/HTTP problem decoder
и notice/settings components проходят synthetic browser scenario без второго
launch request. Прямой query/preflight RPC для обхода обычного launch authority
не добавляется; проверка выполняется внутри настоящей owner mutation до её эффекта.

Native fixture подтверждает два проекта, полный missing/revoked список,
noninteractive refusal, отсутствие новых session/run при отказе, отказ чужому
session owner и успешный прежний launch без personal requirements. Это не
двухпользовательский live JWT либо installed workflow/tool тест. Отдельный
`make test-personal-integration-shared-regression-postgres` запускает весь
`TestBootstrapComponent` на disposable034. Обычный путь новой установки также
проходит forward/repeat, самостоятельный verifier034 и полный BootstrapComponent.
Исторические verifier032/033 сохраняют прежний закрытый контракт. Это локальная
квалификация схемы, а не разрешение обойти план installed migration.

Дополнительный native fixture публикует настоящий SERVER_DAG_V1 через
Create/Validate/PublishWorkflow и проверяет LaunchRun: полный отказ по двум
личным подключениям не создаёт session/run. Отдельный опубликованный workflow
без integration requirements запускается несмотря на personal grant координатора
и затем штатно отменяется. ARTIFACT requirement остаётся обычным неперсональным
контрактом результата; внешний tool и модель здесь не исполняются. Проверена
документация pgx5.10.0 через Context7 `/jackc/pgx/v5.10.0` (QueryRow/Scan/context).
Это проверка local launch/preflight; personal delegation, Human Gate и
continuation проверяются отдельными сценариями ниже, installed acceptance — отдельно.

Native owner fixture дополнительно исполняет два SERVER_DAG_V1 шага с настоящими
read invocation/claim/Begin gRPC/receipt и ARTIFACT completion. Между шагами стоит
result Human Gate: другой USER с собственным READY ключом и штатными MEMBER/project
membership VIEW/RESOLVE_GATES одобряет результат. Следующий physical run и immutable
credential pin сохраняют исходный root initiator и его revision; ключ согласующего
не используется. Gate replay не создаёт нового решения. Новый domain service читает
ту же PostgreSQL history, второй шаг завершает root SUCCEEDED. Transport peer,
provider health/results, object store и AV — synthetic fixture observations;
это не live HTTP/JWT, Pod/process restart либо native session archive/resume.
Отдельная RBAC-only gate projection несовместимость остаётся неквалифицированной;
данный сценарий использует штатный membership путь и не расширяет его прав.

Сценарий personal SYSTEM assistant дополнительно проходит owner snapshot,
PVC-delete receipt, restore, object GC и повторную публикацию. Отдельный OS process
создаёт новый pool/Repository/Bootstrap на прежней disposable БД, получает свежий
runtime claim и проверяет тот же session/root user через настоящий Begin RPC.
Следующий turn после revoke закрыт до записи. После явной замены и проверки
ключа второй OS process получает новый immutable credential pin; прежний успешный
receipt и каноническая история сохраняются. Личный ключ интеграции не передаётся
в native runtime и не является зависимостью его контекста: сама rotation такого
ключа не требует сброса совместимой нативной сессии. Модель, её credentials и
runtime-конфигурация остаются отдельными зависимостями совместимости.
Archive/object/provider tuples синтетические; реальные S3/PVC bytes, запуск
модели и installed Pod restart этим сценарием не доказываются.

Native legacy workflow отдельно создаёт реальный DelegateExecution child,
проводит его read через resolve/claim/Begin gRPC/receipt и сохраняет root initiator.
Revoke запрещает новое намерение, прежний receipt остаётся SUCCEEDED; явная замена
и test разрешают новое намерение с новой credential revision и прежним root user.
Callback создаёт свежий CONTINUES/RR родителя. Новый pgx pool, Repository constructor,
конфигурация и повторный Bootstrap на прежней disposable DB заново читают историю;
повтор проходит через этот новый owner. Другой launcher с собственным READY ключом
не получает чужую session или retry. Revoked key блокирует owner retry до run/turn,
после замены тот же retry intent сохраняет SessionRef/RetryOfRunRef и исходного
инициатора, delegated child и parent continuation завершаются SUCCEEDED. Это
проверка startup/storage reconstruction в одном OS process с synthetic object store,
а не установленный Pod restart, внешний JWT/API или native checkpoint restore.

Описанные workflow/continuation, delegation, owner restart/rejoin и permission
observations проверены локально с synthetic transport/provider observations.
Остаются immutable candidate, установленный workflow/runtime, реальные archive
bytes и разрешённые live provider round-trips. Локальные сценарии их не заменяют.
Direct-agent runtime, exact root/revision pins, UNKNOWN expiry и сериализация
Begin/revoke проверены ниже. Частичный personal mode не выкатывается отдельным PR.

## Помощники и согласованный запуск

SYSTEM/PROJECT assistant turn использует тот же bounded personal preflight до
создания run/turn и до INTERRUPT_ACTIVE. Identity помощника берётся из текущей
беседы и канонического assistant/profile, а actor — из проверенного owner scope.
Для SYSTEM запрос требований выбирает именно agent из assistant_runtime;
контекст проекта в перенесённой SYSTEM беседе не подменяет помощника и не скрывает
его grants. PROJECT requirements остаются внутри разрешённого проекта/агента.
Прежние ограничения владельца беседы и project-assistant Connection purpose
сохраняются. Здесь нет browser selector пользователя либо ключа.

SYSTEM grant readiness отделяет personal configuration от общей credential;
прежний shared/interaction branch сохранён. PROJECT grant проходит прежний
owner-created purpose и canonical admission. Своя credential проверяется перед
turn и при настоящем Begin; готовность одного пользователя не записывает общий
CONNECTED. Каждый новый assistant root получает текущего проверенного actor через
существующий owner insert. Retry того же пользователя проходит тот же preflight
через addAssistantTurnWithAttachmentPolicy; чужую беседу он не открывает.

| Переход | Authority и граница | Отказ / устойчивое наблюдение |
| --- | --- | --- |
| Create conversation | Прежний owner/assistant/profile guard | Не начинает model turn; личная binding не создаётся |
| Add turn | Свежие personal requirements после conversation/OCC, до run/turn и interrupt | Полный typed preflight без новых run/turn либо изменения conversation |
| INTERRUPT_ACTIVE | Тот же guard до cancel прежнего root | Missing/revoked key не отменяет уже допущенный run или effect |
| Assistant retry | Сохранённый owner source и новый turn через общий guard | Revoked key не создаёт root/turn; старая история сохраняется |
| Validate launch plan | Owner plan/revision/context и preflight реальной цели | Personal error возвращает полный typed список вместо generic runtime-unavailable; plan/version не меняются |
| Apply launch plan | Прежние approval/receipt/pins и настоящий LaunchRun guard | Ключ не выбирается из prompt, planner либо Human Gate; новый invocation закрепляет свой root |
| Complete admitted effect | Exact Begin receipt/lease и старый immutable pin | Поздний результат сохраняется после revoke; это не обратная отмена эффекта |

Native fixtures выполняют SYSTEM и PROJECT turn → runtime/RR → invocation →
claim → protobuf/gRPC Begin → completion с synthetic provider observations и
synthetic peer admission. Для PROJECT Connection purpose создаётся реальным
планом самого проектного помощника с validate/apply; raw SQL origin не вставляется.
Проверены missing/revoked key, неизменные run/turn/conversation при отказе,
SYSTEM context move, сохранение начатого эффекта при отказе interrupt и отказ
assistant retry после revoke. Дополнительный PROJECT plan предлагает разрешённый
запуск агента в точном AGENT context. После revoke validate возвращает полный
список и сохраняет план; после новой self credential/test тот же plan явно
validate/apply запускает отдельного агента. Его runtime/Begin получает новую
revision, сохраняя root initiator и прежний effect receipt.

Общий ProblemNotice показывает полный preflight и отдельную ссылку на настройки,
включая compact surface помощника. Общий компонент находится в shared/ui;
feature-to-feature import не добавляется. Новый backend guard и этот UI не
доказывают живой SSO/provider, персональную native-session archive/resume,
workflow delegation/Human Gate всех пользователей, restart либо installed acceptance.
`make test-personal-integration-assistant-regression-postgres` отдельно проверяет
прежние SYSTEM/PROJECT grants и assistant session resume на disposable draft034;
это shared regression, не доказательство personal resume.

## Неизменяемая credential lineage намерения invocation

После обычного admission runtime/grant и только при первой вставке invocation
owner разрешает Connection и сохранённый `root.initiated_by`, а не actor из
worker payload, prompt, Human Gate или последнего пользовательского запроса.
Shared policy сохраняет прежний путь. Personal policy требует текущего
поддержанного Connection, активного USER с `integration.manage`, точного
published package/configuration и положительного capability observation.
SCHEDULE/INTEGRATION root не получает личный ключ владельца как service fallback.
Порядок блокировок Connection → personal binding совпадает с configure/revoke.

В той же owner transaction новая таблица
`integration_personal_invocation_credentials` закрепляет exact invocation,
tenant/Connection/root/subject, binding generation и immutable credential revision,
definition/configuration digests. Composite FK связывают invocation с Connection,
root с инициатором, binding с тем же subject/Connection и revision с binding.
Insert guard сверяет настоящий root исполнения и текущую проверенную binding.
Runtime получает SELECT/INSERT, без UPDATE/DELETE; trigger запрещает переписывание
истории. Исключение DELETE использует только существующий защищённый
`project_purge_authorized()`, не caller-set GUC. Все пять FK двух новых таблиц deferrable;
forward migration проверяет полный прежний purge function body/ACL, прежние
109 nodes и303/304 edges, exact новый delta и readback двух fingerprints.
Неизвестный граф по-прежнему закрыто отклоняется. Backup reader получает SELECT
через штатные owner default privileges.

Повтор существующего intent не вызывает pin writer и не выбирает ключ заново.
Credential revision не добавляется в effect key: ротация не создаёт новое WRITE
намерение под прежним idempotency key. Отзыв закрывает создание нового pin,
сохраняя уже записанную lineage и effect receipts. Отдельного доменного события
для внутреннего pin нет: он входит в существующую атомарную owner-запись invocation;
авторитетное чтение — его immutable row, не browser projection.

| Переход | Источник authority | Устойчивый результат |
| --- | --- | --- |
| Новый personal intent | Текущие grant/runtime admission, сохранённый root, Connection → binding locks | Единственный immutable pin в транзакции invocation либо полный rollback |
| Replay прежнего intent | Существующий intent digest и lifecycle/rejoin | Прежний invocation и pin, без нового выбора ключа |
| Rotate/revoke | Собственная binding command с OCC/generation | Новый binding epoch, старая lineage сохраняется |
| Начало внешнего вызова | Отдельный gateway-only Begin, свежие grant/RR/lease и credential pins | Единственный committed admission возвращает descriptor; pin без Begin не разрешает HTTP |
| Completion/UNKNOWN | Существующий effect receipt и lifecycle | Неизвестный WRITE не повторяется из-за смены ключа |

Native rollback fixtures проверяют настоящее сохранение pin владельца корня,
неизменяемость, отсутствие чужого/shared fallback после revoke и минимальные
ACL/backup defaults. Проверен DB purge проекта с заполненным pin через штатные
owner lifecycle/finalization функции; личные bindings/revisions организации
сохраняются. Внешние PVC/objects в этом fixture не создаются. Fixture напрямую подготавливает verified metadata и
invocation coordinates: это не доказательство runtime grant admission, JWT,
provider permission либо полного tool call. Нормальный resolve path вызывает
writer только для первой вставки. Отдельный production owner runtime path теперь
проходит без общей credential и фиктивного CONNECTED. Это не завершает проверку
всех assistant/workflow/continuation аналогов либо живого provider HTTP.

## Одноразовый допуск непосредственно перед внешним вызовом

`BeginPersonalIntegrationInvocation` — отдельная worker operation
`platform.personal-integration-calls.begin`, только для integration-gateway.
Browser/runtime/interaction allowlists её не содержат. Request profile
`UNARY_PROTO_SHA256` покрывает invocation/lease/fence/generation/instance;
worker не передаёт subject, credential, endpoint либо recovery mode.

Claim personal invocation не читает shared credential и не выдаёт descriptor:
он содержит `credential_admission_required`. Шлюз до Execute/Recover вызывает
Begin один раз. Отказ либо неизвестный ответ не вызывает HTTP, completion,
скрытый retry или fallback. Ответ с authority при `admitted=false`, неизвестными
полями, иным work mode либо неполной credential закрыто отклоняется. Успешная
metadata копируется в локальный claim; исходные owner envelopes не меняются.
Ошибки уже выполненного adapter по-прежнему измеряются до completion.

Owner берёт Connection → binding → invocation locks, разрешает свежую personal
selection и сравнивает её с immutable intent pin. Используется **тот же полный
SQL predicate**, что при claim, с отдельным RUNNING/ref режимом: historical
workflow branches, approval, current grant и RuntimeRevision не заменены
сокращённой копией. Затем сверяются current workload/instance/lease/fence/
generation/deadline. Recovery mode читается из owner-issued lease state.

`integration_personal_invocation_admissions` хранит append-only receipt на lease
и разрешает лишь один EXECUTE на invocation; отдельные recovery receipts не
разрешают WRITE. Descriptor возвращается только после commit admission и audit.
Потеря ACK/commit не даёт повторного разрешения. Истечение admitted personal READ
сохраняет UNKNOWN, а не возвращает его в READY: внешний запрос уже мог состояться.
Истечение не начатого READ и прежний shared режим сохраняют старое поведение.

Известный credential/grant отказ до любого admission закрывает лишь точный
owned RUNNING lease как FAILED с безопасным кодом и audit. После admission такой
отказ не переписывает внешний outcome. Terminal читается через существующий
`GetIntegrationInvocation`; нового доменного события этот внутренний переход
не создаёт. Ошибки инфраструктуры не маскируются credential denial.

Native rollback fixtures проверяют SQL lease/pin matching, отказ чужому lease,
один EXECUTE при replay/новом поколении, UNKNOWN после expiry, сохранение начатой
работы при denial, terminal unstarted denial и purge заполненных pins/receipts.
Lease metadata в rollback fixture задаётся напрямую. Дополнительно native
component выполняет положительный runtime/RPC путь, описанный ниже. Worker
tests исполняют настоящий dispatch helper с управляемыми owner/adapter inputs;
они не являются provider HTTP/JWT проверкой. Все continuation аналоги и live
acceptance ещё должны быть квалифицированы до публикации полного unit.

## Положительный runtime, ротация и отзыв

`make test-personal-integration-credentials-postgres` выполняет настоящие owner
commands: personal configure/test, Grant, LaunchRun, ClaimExecution с immutable
RuntimeRevision, ResolveIntegrationInvocation, gateway claim, Begin и effect
completion/receipt replay. Проверка использует два пользователя и два проекта
одной disposable организации. Project launcher с одним `integration.view`
получает unavailable до запуска; после точного `integration.manage` собственный
отозванный ключ остаётся revoked, несмотря на готовый ключ владельца. Отказ не
создаёт session/run. После проверки своего ключа второй пользователь проходит
runtime и получает лишь собственную revision; immutable pin совпадает с root
initiator, состояние ключа первого пользователя не меняется.

Begin проходит настоящий protobuf/gRPC handler через `bufconn`, production
profile/hash interceptor, principal resolver и PostgreSQL transaction. Только
peer admission и provider observations синтетические; `bufconn` не открывает TCP
и не доказывает mTLS, JWS, JWT, внешний HTTP или installed acceptance. Отсутствие
profile, user bearer, чужие generation/fence/instance не расходуют действительный
Begin slot. Replay не выдаёт второй descriptor. Completion без exact committed
Begin отклоняется; после admission результат допускается и после отзыва ключа.

Для Begin/revoke проверены оба последовательных порядка и одновременное начало
двух операций. Если Begin успел первым, результат сохраняет уже начатый effect.
Если отзыв первым, descriptor не выдаётся, не начатая invocation закрывается
FAILED без effect receipt, выдуманный completion отклоняется. Следующий Begin
не проходит. Это не обещает отмену уже начатого внешнего действия.

Completion блокирует Connection → binding → invocation и сверяет append-only
receipt точного lease/fence/generation. Старые shared intents без personal pin
сохраняют прежнюю семантику даже после поздней смены Connection policy. Generic
adapters объединяют 401/403 в `INTEGRATION_AUTH_REJECTED`; такой результат
сбрасывает **только текущую совпадающую generation/revision** в CHECK_REQUIRED,
очищает прежние подтверждения и закрывает pending probe. Он не выдумывает expiry
или причину отказа. Receipt replay не сбрасывает binding второй раз. Поздняя
ошибка старой revision после ротации сохраняет READY нового ключа; следующий
вызов закрепляет новую проверенную revision и не переписывает старый receipt.

## Сквозная проверка личного ключа

`POST /api/v1/me/integration-connections/{connectionRef}/credential/test`
принимает только `expectedConnectionVersion`; `If-Match` относится к собственной
binding, `Idempotency-Key` — к намерению проверки. Endpoint проходит обычные
session/CSRF проверки. Browser operation `platform.personal-integrations.test`
классифицирована как `USER_CREDENTIAL_REQUIRED`. Owner разрешает пользователя и
видимый Connection, проверяет policy/enabled/OCC, published Definition, health
и закрытый набор registered `READ/NONE` probes. Одна транзакция сохраняет `CHECKING`, immutable
snapshot, DUE job, audit и command receipt. Actor, credential descriptor, endpoint
и произвольный probe не принимаются из browser payload.

Integration-gateway получает только `claim/begin/complete` этой очереди, без
личных management operations. Claim передаёт descriptor точной собственной
revision, published package/configuration, exact probe keys и конечную аренду на30s. Сериализация
блокировок: Connection → binding → job. Один poll берёт одно задание. До HTTP
worker вызывает `begin`: owner повторно проверяет active user/visibility,
configuration/definition и binding generation/revision, одноразово переводит
CLAIMED→PROBING и записывает `probe_admitted_at` с audit. Потерянный ACK не
разрешает повтор begin либо внешний запрос. Неопределённость завершается expiry,
а новая явная проверка создаёт отдельное READ задание.

Adapter получает точный Secret descriptor через существующий mounted credential
backend и исполняет health, затем разрешённые владельцем registered READ probes:
GitLab — только inputless list operations в exact project с page limit1;
Jira Server/DC и Confluence Server/DC — только существующие typed identity reads.
Отсутствующий список в legacy claim разрешает исключительно прежний health.
Gateway не достраивает полномочия старого owner из новых локальных возможностей.
Новый owner со старым gateway принимает прежний health-only результат.
Каждый probe использует прежние HTTPS/auth/SSRF/response limits и общий finite
operation deadline; произвольные endpoint, inputs, READ names и WRITE отсутствуют.
Provider projections отбрасываются: completion содержит лишь exact lease pins,
auth outcome и keys реально успешных/запрещённых операций. Owner сверяет их
с immutable snapshot и текущими binding/configuration/definition. Extra key,
поддельная запись, дубликат либо confirmed/denied overlap закрыто отклоняются.
Наблюдённый403 после health отмечает отдельную capability как denied;401
отменяет всё observation. Timeout/404/429/невалидный response дополнительных
probes не подтверждают их и не выдумывают deny. Успешный list GET не доказывает
чтение произвольного объекта/файла либо WRITE. Права этих операций и ненаблюдённый
expiry остаются неизвестными; launch preflight сохраняет закрытый отказ,
кроме явно зарегистрированного условного профиля проверки объекта ниже.
`AUTH_REJECTED`
переводит binding в `INVALID`; недоступность — в `CHECK_REQUIRED`. Connection и
bindings остальных пользователей не становятся готовыми от проверки одного ключа.

### Права на выбранную страницу Server/DC

Для `confluence-server-dc/confluence.page.update` personal preflight проверяет
точный READY ключ, binding/configuration/definition pins и подтверждённые
`confluence.space.read` + `confluence.identity.read`. Известный deny, отсутствие,
invalid/expiry/revoke по-прежнему закрывают запуск. READ proofs не добавляют
page.update в глобальный список подтверждённых прав. Неизвестные WRITE и другие
непроверенные операции остаются закрытыми.

Owner выводит из опубликованного executable package обязательный профиль
`CONFLUENCE_SERVER_PAGE_UPDATE_V1`, передаёт typed enum в claim field25 и повторяет
его после единственного committed Begin в response field4. Gateway сверяет оба
профиля до dispatch. Старый gateway отклоняет unknown claim field; новый gateway
не принимает personal page.update без профиля либо с несовместимым Begin.
UI отдельно объясняет условную проверку объекта и не показывает глобальный WRITE.
Профиль также сохраняется в immutable invocation credential pin. DB trigger
принимает его только для exact Server/DC page.update/WRITE/Human Gate/space и
двух READ proofs без deny; отсутствие профиля не обходит проверку через прежний
global-proof путь. Begin повторно сверяет профиль с сохранённым pin. Изменена
только неопубликованная локальная draft034; установленная033 не менялась.

Сценарий: root initiator → server launch preflight → runtime/grant → точный
page_id/expected_version/input digest → Human Gate → owner claim → одноразовый
Begin с повторной проверкой личного ключа → typed gateway → фиксированный GET
`/rest/api/content/{pageId}?expand=space,version,ancestors,body.storage,operations`
под заданным context → exact id/current page/space/version и единственное
`update/page` → один PUT со следующей expected version → прежний effect receipt
и owner completion. Body читается отдельным вторым GET только после exact space
eligibility первого; обе выдачи проверяются. Отсутствующие, слишком большие и
неоднозначные operations закрывают PUT. Изменение внешних прав между GET и PUT
проверяет сам Confluence; атомарный межсервисный snapshot не обещается.

Совпавший прежний effect marker/version/input согласуется только чтением,
включая потерянное право update; новая запись не повторяется. `REQUEST_REJECTED`
конкретного объекта не превращает пригодную identity ключа в INVALID. Revoke,
ротация, root lineage и UNKNOWN lifecycle сохраняют прежние owner fences.
PROJECT_SHARED сохраняет штатный путь. Этот раздел описывает локальную
реализацию; installed и live-проверки профиля ещё не выполнены.

### Персональные записи Jira Server/DC

Общий закрытый реестр owner/consumer также задаёт три обязательных профиля:
`JIRA_SERVER_ISSUE_CREATE_V1`, `JIRA_SERVER_COMMENT_WRITE_V1` и
`JIRA_SERVER_TRANSITION_APPLY_V1`. До запуска нужны точные READY key pins и
подтверждённые `jira.project.read` + `jira.identity.read`, без известного deny.
Это условный допуск к проверке выбранного объекта, а не глобальное подтверждение
WRITE. Профиль проходит прежнюю цепочку root → preflight → grant → input digest
→ Human Gate → immutable DB pin → claim25 → одноразовый Begin4 → gateway.
Отсутствующий/чужой профиль закрывается в owner, DB и consumer; shared и Cloud
сохраняют самостоятельное поведение.

Непосредственно перед POST gateway читает фиксированный Server endpoint
`GET /rest/api/2/mypermissions` под Connection context, передавая ровно
`projectKey` для create либо `issueKey` для comment/transition. Перед issue check
сама задача должна пройти exact key/project eligibility. Нужны canonical
`CREATE_ISSUES`, `ADD_COMMENTS` либо `TRANSITION_ISSUES` с `type=PROJECT`, явным
`havePermission=true` и без deprecatedKey. Unknown/false/malformed/auth/timeout
не начинают POST. Вход и выбранный контекст не могут назначать permission name,
Authorization либо endpoint. Cloud-only query `permissions` не используется.
Для transition остаются проверка доступного ID и отказ от обязательных полей.
Внешний API повторно проверяет фактические права и workflow conditions при POST.

Это поднабор по [официальному REST9.12.8](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.8/),
а не перенос latest/Cloud: project context может подтверждать issue-dependent
право лишь для некоторых задач, поэтому comment/transition не используют его.
OpenAPI1.1.0 описывает `jira.permissions.read` как внутреннюю предпосылку;
Definition/Capability/tools этой операции не предоставляют. При совпавшем create
label/payload read-only rejoin возвращает прежний receipt без новой проверки
CREATE_ISSUES и без POST. Неизвестные comment/transition effects по-прежнему
закрыты, пока нет поддержанного read-only reconciliation; повтор WRITE запрещён.

Permission JSON проверяется до typed decode: bounded bytes/depth, запрет duplicate
и Unicode case-fold ключей и trailing documents. Ни одна metadata-проверка не
записывает глобальный WRITE proof или inferred expiry. Object rejection сохраняет
identity ключа; auth rejection использует прежний exact-revision recheck lifecycle.
Для четырёх зарегистрированных personal WRITE adapter создаёт отдельное локальное
наблюдение HTTP mutation boundary: оно меняется непосредственно перед non-GET/HEAD
`client.Do`. Metadata/permission errors до этой точки не превращаются в
UNKNOWN_OUTCOME из-за общего WRITE risk. После начала POST/PUT сохраняются прежние
UNKNOWN stages и запрет повтора при timeout/потере либо невалидном ответе.
Shared и остальные adapter сохраняют прежнюю классификацию. Это наблюдение только
текущего Execute, не durable receipt: потеря процесса/Begin ACK/lease expiry
по-прежнему сохраняет owner uncertainty и не разрешает новую попытку записи.
Новые events/queue/retry и отдельный permission worker не добавляются. Текущая
реализация локальная; её native и provider fixture проверки не заменяют live Jira.

| Переход           | Проверка и атомарный результат                                                        | Повтор / неопределённость                                                           |
| ----------------- | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Queue             | Self/OCC + enabled published READ; CHECKING/job/audit/receipt                         | Exact receipt возвращает прежний результат без второго job                          |
| Claim             | Exact integration worker; свежая eligibility и конечный lease                         | Второй worker не получает тот же job; пустой poll без audit                         |
| Begin             | Exact fence/generation, текущий пользователь и snapshot; durable admission + audit    | PROBING не допускает второго begin; потерянный ACK не повторяет HTTP                |
| Complete          | Только PROBING/live lease; closed outcome; metadata/job/audit/receipt                 | Exact receipt не повторяет mutation; иной outcome под прежним ключом отклоняется    |
| Revoke / rotation | Владелец блокирует binding, закрывает её jobs/leases и меняет generation              | Поздний результат не проверяет новый ключ; начатый READ не отменяется задним числом |
| Connection change | Connection owner закрывает jobs/leases; значимое изменение очищает proof              | Старый receipt не восстанавливает READY; новая проверка отдельная                   |
| ACL/user drift    | До claim/begin/complete свежая eligibility; job закрывается без применения результата | Никакого shared fallback; история остаётся в owner read path                        |
| Expiry            | Owner sweep закрывает job, очищает lease, освобождает точный CHECKING                 | Нет автоматического повторного HTTP; новая проверка требует явного намерения        |

Очередь обслуживается существующим циклом integration-gateway после startup
barrier, до общей очереди проверок Connection, без нового deployable. Worker
завершается по существующему cancel/join контракту процесса. Сначала нужны новая
forward034 schema и authority revision103 со всеми четырьмя operations; старый
установленный профиль033 не получает эти методы отдельно. Смена materialization
backend, ослабление mTLS/JWS и передача ключа в shell для этого не требуются.

Native PostgreSQL fixture подтверждает queue/replay, одноразовый begin,
completion receipt, configuration invalidation, revoke/rotation, disable,
lease expiry, двух конкурирующих workers и соответствие durable admission/audit.
Provider observations в этом fixture синтетические. Отдельный тест реального
цикла gateway подтверждает отсутствие вызова при отказе/lost ACK, finite timeout,
closed outcomes, отсутствие provider payload в completion и учёт probe до ошибки
сохранения результата. Эти проверки не доказывают live provider/JWT E2E,
двухпроектную изоляцию, launch preflight либо обычные tool invocations.

## Сквозная карта чтения личного каталога

Browser identity проходит текущий OIDC/session boundary, gateway получает
operation `platform.personal-integrations.list` и exact unary digest page/query.
Generated client вызывает `PersonalIntegrationCredentialService.ListPersonalIntegrationConnections`;
локальная classification требует `USER_CREDENTIAL_REQUIRED`, worker maps не
содержат эту операцию. Control-plane разрешает principal, читает SQL с текущим
visibility predicate и JOIN только собственной привязки, проверяет descriptor
metadata и возвращает безопасный Proto/OpenAPI view. В ответе нет owner IDs,
Secret descriptor либо значения ключа. Ошибочный/missing PageInfo, неизвестная
policy, duplicate Connection либо несовпадение nested binding pins закрыто
отклоняются. Чтение не публикует event и не выдаёт execution grant.

## Владелец и источник identity

Connection, Definition, Capability и Grant остаются общими сущностями.
`PROJECT_SHARED` выбирает явно разрешённую service credential подключения.
`INITIATOR_REQUIRED` выбирает только персональную привязку к тому же Connection,
organization и сохранённому корневому инициатору. Отсутствующая привязка не
переводит выполнение в другой режим. Личный ключ не копируется в общий
Connection при установке, ротации, повторе или согласовании действия.

Существующий `root.initiated_by` — канонический источник пользователя во время
выполнения. Имя из prompt, текущий собеседник, владелец проекта и approver Human
Gate не выбирают credential. Результат разрешения связывает root, tenant,
Connection/definition/configuration pins, собственника, binding generation и
immutable credential revision. Аутентификация, права запуска и grant операции
проверяются отдельно от наличия ключа.

Персональная привязка содержит opaque secret metadata, а не значение ключа.
Materialization identity включает server-owned tenant, пользователя,
Connection и operation identity: одинаковые idempotency keys разных
пользователей не объединяют секреты. Изменение чужого личного ключа не меняет
готовность другого пользователя или общий Connection.

## Матрица операций

| Операция                            | Полномочие и состояние                                                                             | OCC / идемпотентность                                                          | Эффект и доказательство                                                                                          |
| ----------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| Смена credential policy             | Выделенная browser operation и текущее `integration.manage`; Connection отключён и не используется | Connection OCC и semantic receipt до повтора; owner lock                       | Только policy/version и сброс общей test evidence; audit/event/receipt атомарны, личные bindings не меняются     |
| Список и чтение «Мои подключения»   | Текущий активный server principal; только доступные ему Connection и собственные bindings          | Bounded query и pagination; cache не создаёт доступ                            | Только public metadata, статусы и подтверждённые возможности; значение ключа отсутствует                         |
| Создание персональной привязки      | Собственник назначается сервером; доступность Connection проверяется заново                        | Expected-absent и exact Connection version; semantic command receipt           | Metadata-only создание и audit в одной transaction; события нет, собственный addressed read возвращает состояние |
| Запись credential                   | Собственная привязка и опубликованный поддержанный definition/configuration                        | Binding OCC; actor-qualified materialization identity; exact secret descriptor | Materializer и затем owner commit; потерянный ответ требует адресного readback                                   |
| Проверка ключа                      | Собственная актуальная revision; только фиксированный adapter/endpoint                             | Claim/test generation и bounded deadline                                       | Identity и подтверждённые права; права записи не доказываются произвольным GET                                   |
| Замена                              | Собственная привязка; новая revision без перепривязки старых effects                               | Новый descriptor и OCC, receipt при rejoin                                     | Новый ключ требует проверки; прежнее значение не возвращается                                                    |
| Отзыв                               | Специализированная команда собственника, без generic изменения owner/state                         | Binding epoch/version и one-winner receipt                                     | Новые call starts закрыты; ранее начатые effects сохраняют свой lifecycle                                        |
| Launch preflight                    | Сервер сам получает Agent / Workflow revision и необходимые grants                                 | Только authoritative metadata, ограниченный набор зависимостей                 | Возвращается полный список известных проблем, состояния прав и ссылки настройки                                  |
| Начало запуска                      | Свежие права запуска и текущая доступность обязательных bindings                                   | Проверка в owner transaction перед созданием корня/turn                        | Отрицательный результат не создаёт запуск; disabled button не заменяет отказ API                                 |
| Начало tool call                    | Сохранённый root initiator, текущие grant/configuration и binding epoch                            | Авторитетный begin-call / lease consume сериализуется с revoke                 | Credential передаётся только доверенному gateway для конкретного effect                                          |
| Delegation / retry / resume         | Identity и binding выбираются из того же корня                                                     | Текущая authority; старый effect не получает новый create автоматически        | Approver и новый собеседник не подменяют initiator                                                               |
| Schedule / webhook без пользователя | Явная допустимая service identity для shared policy                                                | Проверка конфигурации до launch                                                | INITIATOR_REQUIRED без подходящего initiator закрыто отвергается                                                 |
| Завершение / UNKNOWN                | Сохранённый effect key, input digest и credential lineage                                          | Exact receipt либо разрешённая read-only reconciliation                        | Revoke/rotation не повторяет неоднозначную запись и не отменяет её задним числом                                 |
| Удаление старого secret material    | Только owner-confirmed отсутствие активных consumers и применимый retention                        | Отдельный cleanup lifecycle и descriptor CAS                                   | User readback не раскрывает значение; audit и effect receipts сохраняются                                        |

## Готовность и смена настроек

Подключение с персональной политикой имеет общую доступность definition и
конфигурации, но готовность credential принадлежит конкретному пользователю.
Неудачная проверка одного ключа не делает все персональные bindings
непригодными. Изменение endpoint, definition или значимых public settings
инвалидирует применимую проверку; ключ нельзя молча отправить другому host.

После записи состояние credential не означает подтверждения всех прав.
Отсутствующий, непроверенный, недостаточный, истёкший или отозванный ключ
отражается отдельно. Неизвестный provider expiry не превращается в выдуманную
дату. Доступность WRITE и её ограничения обозначаются отдельно от успешного
чтения identity; проверка записи допускается только на явно выбранном
synthetic объекте с соответствующим grant и Human Gate.

Preflight не резервирует внешние права. Gateway повторно проверяет допустимость
перед реальным эффектом. Линеаризация revoke и begin-call происходит у владельца
состояния: уже выданный call start учитывается как начатая работа, а не как
обещание отменить внешнюю запись. Произвольный cached positive или ранее
выданный frontend preflight не разрешает новый effect после отзыва.

## Изоляция и совместимость

Обычный project admin не получает доступ к чужим bindings через общие permissions
управления Connection. Payload не выбирает user, secret ref или Authorization.
Смена Connection policy не копирует персональные ключи в shared storage.
OIDC token, model provider credential и integration PAT не взаимозаменяемы.

Персональный режим использует общий проверенный principal в claims-based и
platform-managed OIDC. Он не добавляет обязательных mapper-ов внешнему IdP.
Права управления общими grants/сервисными credentials сохраняют собственные
проверки freshness и authority; self-scoped credential API не выдаёт эти права.

Secrets не поступают в shell, prompt, model config, role image или обычный
frontend readback. Ограниченный git transport и provider credential relay
сохраняют самостоятельные контракты. Текущие admission, SSRF, resource allowlist
и Human Gate проверки не ослабляются.

## Обязательные проверки

### Закрытый профиль схемы034

`personal-credentials` и `personal-credentials-backup` принимают только
033→034. Источник истины — forward-only migration, авторский metadata manifest
и его generated pre/post proof. Manifest создан на synthetic PostgreSQL18.3;
он не содержит строк приложения или credentials. Отдельный capture opt-in
не считается успешной квалификацией release.

Перед DDL проверяются отсутствие034, полный033, права трёх изменяемых таблиц
и отсутствие активных integration invocations/tests. После DDL проверяются
пять новых и три изменённых таблицы,17 функций и17 пользовательских триггеров:
owners/ACL/column ACL/RLS, колонки, defaults, constraints, indexes, inheritance
и точные определения функций. Граф project purge закреплён целиком:111 узлов
и306 связей. Проекция прежних109/303 разрешается только после проверки полного
нового графа и catalog; исторические validators не принимают новую схему напрямую.

Backup-reader получает только SELECT на эти восемь таблиц. Версионный restore034
проверен дважды; чужие write/grant-option права отклоняются. Negative fixtures
проверяют конкретный guard SQLSTATE/message: ошибка вложенной транзакции или
изоляции не считается доказательством отказа catalog. После counterfactual
снова исполняется полный положительный READ ONLY proof.

| Проверка | Локальный результат | Граница доказательства |
|---|---|---|
| Normal034 pre/post и10 catalog mutations | PASS | Disposable PostgreSQL18.3, без корпоративных данных |
| Backup034 forward/repeat/restore2/write denial | PASS | Восстановление прав, не восстановление production dump |
| Frozen032 и033 | PASS | Собственные неизменённые SQL proofs и точный отказ catalog |
| Default fresh install/repeat и полный BootstrapComponent | PASS | Реальная схема034, synthetic providers |
| Immutable migration CLI | PASS87/87 | Plan/check/stop/inspect/resume, один DDL Job после lost proof |
| Сохранение bootstrap effects при033→034 и portable regression | PASS52/52 | Offline prepare, fake cluster receipts, exact034 normal/backup dispatch |
| Полный owner go test/vet/build | PASS | Go1.26.6, без live credentials |
| Установленный candidate034 и его restart/restore | NOT RUN | Нужны immutable images, preimage и exact plan |

Локальные normal/backup verifier не отменяют preimage перед apply и ограничения
forward-only БД. Возврат к прежнему image сам по себе не является rollback схемы.

- Два пользователя / два проекта, одинаковые operation keys разных субъектов,
  чужой binding ref, project admin и подмена owner в payload.
- Missing / invalid / insufficient / expired / revoked ключи; все известные
  проблемы видны до запуска; вызов после отзыва закрыт независимо от preflight.
- Concurrent rotation/revoke, lost materialization/commit response, текущая
  revision и уже начатый либо UNKNOWN внешний effect.
- Delegation, Human Gate другим пользователем, retry, pause/resume и restart;
  schedules/webhooks без интерактивного пользователя.
- Изменение definition/configuration/host, запрет credential forwarding,
  отсутствие fallback и сохранение работы другого пользователя при ротации.
- UI add/test/replace/revoke, отсутствие повторного значения в ответе,
  bounded metadata, отсутствие секретов в events/logs/runtime material.
- Forward-only миграция, exact predecessor и backup ACL; новый immutable
  candidate квалифицируется отдельно от предыдущего установленного релиза.

Native проверки используют synthetic fixtures. Установленная role→tool→gateway
проверка фиксируется отдельно. Недоступный второй живой аккаунт означает
`BLOCKED` соответствующей строки, а не `PASS` из локального контракта.

## Чтение конкретного объекта с личным ключом

Локальный draft добавляет обязательные profiles `GITLAB_PROJECT_OBJECT_READ_V1`,
`JIRA_SERVER_PROJECT_OBJECT_READ_V1`, `CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1`.
Разрешены ровно девять операций: GitLab file/issue/MR/pipeline read;
Jira Server issue search/read/transitions list; Confluence Server page search/read.
Cloud definitions и остальные операции не наследуют этот допуск.

Preflight проверяет текущий личный READY key и точные metadata/identity proofs
этого подключения. Права на выбранный объект остаются **неподтверждёнными** до
запрошенного GET; успешная metadata не добавляет объектную operation в
`verifiedCapabilityKeys`. Known deny, отсутствие/ротация/истечение/отзыв ключа
по-прежнему закрывают запуск. В UI граница проверки объясняется отдельно.

Owner записывает mandatory profile в immutable DB pin; claim25 и Begin4 несут
тот же typed enum. Профиль закрепляет definition/capability/operation, READ/NONE
и resource kind. Старый, неизвестный либо чужой profile закрывает dispatch до
HTTP, даже если кто-то подставил фиктивный global READ proof. Текущий ключ
повторно проверяется штатным Begin по root identity/generation; ключ проекта,
reviewer или последнего пользователя не подставляется.

Затем прежний typed adapter проверяет inputs и configured project/space и
выполняет фиксированный HTTPS GET. Новых URL, HTTP tools, credential transport,
permission или обхода файлов/SSRF не добавляется. Ответ и принадлежность объекта
проверяются существующим operation-specific contract до проекции результата.
Для registered personal object READ HTTP403 означает отказ на объект и не
сбрасывает identity ключа; HTTP401 требует штатной перепроверки exact key pins.
Shared403 сохраняет прежнее поведение. Ни403, ни404 не создают effect receipt,
не добавляют permission proof и не вызывают fallback.

Это локальная часть unit2033, не установленная функция. UNKNOWN read-only
reconciliation, process/archive/restart и GitLab WRITE описаны и проверены ниже;
границы этих fixtures не заменяют reviews, immutable images и живые вертикали.

## Текущие права GitLab перед записью

Draft добавляет три обязательных consumer profiles: `GITLAB_PROJECT_ISSUE_CREATE_V1`,
`GITLAB_PROJECT_BRANCH_CREATE_V1`, `GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1`.
Первый сохраняет WRITE, два остальных — SENSITIVE; все требуют прежний
HUMAN_EACH_EFFECT, root key и exact immutable DB/claim/Begin pins.

Перед POST gateway читает фиксированные version/current user/self token/current
project endpoints. Поддержан metadata contract19.x начиная с19.2. Требуются
явные active/revoked/granular facts, UTC expiry, тот же API user, exact configured
project и известная base role Developer/Maintainer/Owner. Archived/deleting и
отключённые функции закрывают запись. Это предварительные наблюдаемые условия;
внешний API повторно решает фактическое право на POST, все права не угадываются.

Legacy `api` принимается только с явным `granular:false`. Для granular token
дополнительно читаются details **его собственного** ID из self, повторно сверяются
id/user/состояние и exact project action scope. Поддержаны selected_memberships
точного project и all_memberships при явной проектной роли. Group/personal scope
ancestry не угадывается. Один успешный metadata GET не становится WRITE proof.

Branch creation перед POST исчерпывает до5 страниц protected rules по100 записей,
включая inherited entries; совпавшее правило, неизвестный pattern или незавершённая
pagination закрывают запись. MR проверяет видимость source/target одного configured
project; merge и изменение защиты не добавляются. Credentials не выдаются shell.

Отсутствующий либо несовместимый profile закрывает HTTP до первого запроса.
Metadata refusal/timeout не создаёт ложный UNKNOWN внешней записи; после
начатого POST timeout/невалидный ответ сохраняют UNKNOWN без второго POST.
Штатный PROJECT_SHARED путь сохраняется. Internal metadata schemas и3 writes
сверяются с авторским subset `contracts/integrations/providers/gitlab/v1`:
16 operations,14 paths по pinned официальным docs/source19.2.9. Это не полное
покрытие API, не новая произвольная HTTP capability и не live qualification.

Наблюдаемые token-scope ограничения передаются в server launch-preflight;
read-only UNKNOWN reconciliation сверяет payload/provenance без повторной записи,
как описано ниже. GitLab branch и Jira transition не получают автоматического
восстановления. Reviews/images/deploy/live остаются обязательными. Unit ещё
не установлен; локальный PASS не означает приёмку работающей установки.

## Проверка scope до запуска (#2033)

Источник — обязательный preflight персональных credentials. Self USER вызывает
Test с OCC; CP выводит из pinned Definition конечный профиль проверки и допустимые
write-denial keys. Immutable job → typed claim → одноразовый Begin → только
фиксированные HTTPS GET metadata → fenced Complete с наблюдённым профилем,
явными отказами и фактическим expiry → binding текущего key/config/generation.
Профиль не является global WRITE proof: branch/object checks остаются при вызове.

| Исход | Owner state и чтение |
| --- | --- |
| Поддержанные metadata | READY с profile; launch и Begin сверяют current pins и expiry |
| Известный read-only scope/ограничение | READY с явными write denials; preflight блокирует соответствующие операции |
| Неизвестные metadata/старый worker | READ-наблюдения сохраняются; новые write остаются unconfirmed |
| 401 | INVALID; profile и прежние proofs очищаются |
| Revoke/rotate/config drift | Одна owner-транзакция ограждает job; late Complete ничего не усыновляет |
| Lost ACK / replay | Только прежний receipt; Begin не повторяет внешние GET |
| Expiry | Сервер отклоняет launch/Begin; UI показывает истечение, без fallback |

Новых событий нет: существующие audit/receipt и self read остаются авторитетными.
Schema034 пока локальная; rollout verifier/CP и forward migration выполняется
единым проверенным candidate, с default shared regression и без расширения RPC grants.

Metadata scopes и UTC expiry сверены по официальной документации GitLab
`v19.2.9`: `doc/api/personal_access_tokens.md`,
`doc/security/tokens/access_token_scopes.md`,
`doc/user/profile/personal_access_tokens.md` через Context7. Новые неизвестные
legacy scopes не объявляются ни достаточными, ни явно запрещёнными.

## Ошибка read-only recovery не разрешает прежний effect (#2033)

Источник — безопасные повторы и сохранение неизвестных внешних эффектов. CP
назначает `claimed_recovery_read_only` при UNKNOWN→RUNNING вместе с exact
lease/fence/generation; worker не назначает этот флаг через Complete. Completion
разрешает исход из locked owner row. При unsuccessful recovery исходный effect
остаётся UNKNOWN_OUTCOME; код ошибки относится к попытке чтения. Auth rejection
дополнительно ограждает только exact current personal binding и отменяет его
незавершённые проверки. Ротация другого key не затрагивается.

| Переход | Состояние, receipt и событие |
| --- | --- |
| EXECUTE отказ до effect | Прежний FAILED, без fabricated effect receipt |
| UNKNOWN → recovery READ отказ/timeout/403/401 | UNKNOWN_OUTCOME, audit/action event прежнего lifecycle, без continuation и receipt об отсутствии effect |
| Recovery auth rejection | Та же UNKNOWN history + CHECK_REQUIRED current exact binding в общей транзакции |
| Новый recovery claim | Новые generation/lease/fence, прежние effect/input; EXECUTE не разрешается |
| Положительный exact readback | SUCCEEDED только с проверенным effect/input/provider/result receipt |
| Lost Complete ACK | Прежний idempotency receipt; иное observation старого поколения отклоняется |
| Revoke/cancel/expiry | Прежние Begin/lease/root fences; признание cleanup не означает отмену внешнего effect |

Новый RPC, authority grant, схема либо legacy shared key fallback не вводятся.
Это owner guard для обоих credential modes; HTTP reconciliation ещё требует
проверенного payload/provenance и не считается готовым этим guard.

GitLab issue/MR UNKNOWN recovery допускает максимум четыре lease поколения
на intent (первоначальный Execute и не более трёх read-only попыток). Исчерпание
сохраняет UNKNOWN history и status read, не создаёт FAILED/receipt и не разрешает
повторный POST. Новый intent не является способом повторить старое неизвестное
действие: сначала требуется owner reconciliation/решение по внешнему effect.
Branch recovery по существованию ветки закрыт; attribution отсутствует в API.

## Server/DC recovery через owner-pinned GET (#2033)

Официальные справочники закреплены на Jira9.12.8 и Confluence8.5.9;
Context7 дал часть более нового Jira REST, поэтому версия сверена отдельно.
Host read-only подтвердил Confluence8.5.9/build9012, author userKey и точное
чтение historical version без выдачи body/identity в evidence. Это не live
роль→gateway приёмка.

Путь: root USER/явная shared identity → прежний approved immutable invocation →
UNKNOWN owner claim только закрытой операции, максимум4 lease поколения →
одноразовый personal Begin, где применим → fixed current identity/project/space
GET → bounded marker search либо exact page/version read → author и canonical
payload → detail readback → прежние effect/input receipt → owner transaction.
Новый endpoint, Authorization и внешний ref агентом не задаются.

| Операция | Положительное evidence | Неопределённый исход |
| --- | --- | --- |
| Jira issue.create | Единственная label, creator key, exact project/type/summary/description и detail ID | Missing/duplicate/changed/foreign/pagination drift → UNKNOWN, без POST |
| Jira issue.comment.write | Exact issue/project, один body marker, author key, bounded comments + detail ID | Unknown/отказ/timeout → UNKNOWN, без POST |
| Confluence page.create | Unique exact title/space/parent/body marker, version1, createdBy/by current key и detail ID | Чужое/изменённое/неполное evidence → UNKNOWN, без POST |
| Confluence page.update | Exact page, current versionN+1/by и input marker; historical versionN для сохранённых полей | Conflict/другая version/author/body → UNKNOWN, без PUT |
| Jira transition / GitLab branch | Статус/существование не доказывают provenance данного intent | Auto-recovery закрыт; запись не повторяется |
| Recovery 401/403/timeout / lost ACK | Прежний owner UNKNOWN guard и idempotency receipt | Нет fabricated failure/absence receipt; auth rejection ограждает exact current key |
| Revoke/rotation/cancel/grant expiry | Прежние root/key/config/lease fences | Нового authority либо обхода owner gate нет |

Положительные старые marker-only rejoin в Execute заменяются UNKNOWN: только
явный Recover принимает полный проверенный факт. Новых domain events/RPC,
секретов, разрешений либо повторов mutation нет. Owner UNKNOWN selector
включается только вместе с проверенным consumer; live write ждёт отдельные
разрешённые synthetic объекты и требуемый exact-preview approval.

Ограничения нового recovery: Jira pages не более50 items и5 requests, максимум
250 наблюдаемых записей; Confluence title lookup не более20 items×5 requests.
Используются стандартные Server offsets. Два addressed metadata/body чтения
Confluence должны согласовываться по title/ancestor и author coordinates одной
версии. Historical version читается явным `status=historical&version=N`;
opaque `_links.next` никогда не является новым URL назначения.

Проверенные источники: [Jira REST9.12.8](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.8/),
[Issue creator API9.12.8](https://docs.atlassian.com/software/jira/docs/api/9.12.8/com/atlassian/jira/issue/Issue.html),
[Confluence REST8.5.9](https://docs.atlassian.com/ConfluenceServer/rest/8.5.9/),
[Server expansions](https://developer.atlassian.com/server/confluence/expansions-in-the-rest-api/).
Контракты составлены самостоятельно; реальные schemas/payload не копируются.
Synthetic wire tests не доказывают сохранение маркеров реальным сервером:
round-trip в разрешённых тестовых объектах остаётся отдельной live приёмкой.
Personal create Confluence без подтверждённого permission contract остаётся
unconfirmed на owner preflight; HTTP consumer сам не выдаёт такие полномочия.
