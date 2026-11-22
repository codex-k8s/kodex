---
id: OPS-PERSONAL-EMAIL-2168
title: Личная почта и ограниченное согласие на операции
type: operations
status: approved
owner: developer
version: 1.0.20
updated: 2026-11-21
---

# Границы

Issue [#2168](https://github.com/codex-k8s/kodex/issues/2168), эпик #1830.
Используется существующий EMAIL_HTTPS bridge и его 21 capability, а не новый
движок интеграций. Личная почта имеет отдельный owner-профиль
`MAILBOX_CONFIGURATION`. У package остаются `credential=nil` и generic
`credentialDelivery=NONE`; это не PAT, TOKEN или BROKER_IDENTITY.

Один Connection имеет один текущий mailbox. Владельцем нового личного Connection
становится активный USER, выведенный сервером из проверенного transport context.
Другой пользователь выбирает собственный Connection через существующий lifecycle
agent grants. Payload не заменяет Connection, grant, root actor или mailbox owner.
При личном `INITIATOR_REQUIRED` корневой инициатор должен совпасть с владельцем.
Администратор, утверждающий Human Gate, не становится владельцем credentials.
Специализированная команда создания назначает только точечную системную роль
`integration.view`/`integration.manage` на новый Connection в `RESOURCE_INSTANCE`.
Создание не выдаёт права на проекты, агентов, их grants или управление организацией.
Как и обычный CreateConnection, новое подключение включено и имеет
`NOT_CONNECTED`; это позволяет сразу настроить mailbox credentials. Оно остаётся
недоступным для runtime без принятой конфигурации, health и отдельных grants.
Имя ограничено160 Unicode codepoints, сохраняется без пробелов по краям;
authorizer, общий Create/Update consumer, новый API и UI используют эту границу.
Для выбора либо восстановления SERVICE независимо требуется `organization.manage`;
владение личным Connection не разрешает сменить его на сервисную identity.
`ownerEligible` вычисляется сервером из текущих subject/configurer: сохранённый
`ACTIVE` не доказывает eligibility после отключения владельца.

`PROJECT_SHARED` требует отдельного явного выбора активной SERVICE identity
владельцем настройки после disable/drain. Переход не усыновляет прежние секреты:
новое поколение владельца требует новых descriptor revisions и новой публикации.
Старые mailbox, credential, invocation и receipt записи остаются историей.
Legacy конфигурация без доказанного владельца не получает молчаливого общего
доступа: сначала требуются явная настройка владельца и замена реквизитов.

## Почтовая конфигурация из Git

Для первого импорта оператор создаёт отдельный Connection, отключает его и
штатной командой выбора владельца назначает активную SERVICE identity.
После включения конфигуратор добавляет реквизиты текущего поколения.
В конфигурации развёртывания оператор указывает exact tenant, Connection,
стабильный source и текущее `credential_owner_generation`. Импорт сверяет поколение
с авторитетной записью и переносит значение из этой записи в публикацию.

Control Plane импортирует этот файл как системный субъект установки. Он
проверяет текущее `PROJECT_SHARED`, активность SERVICE и назначившего её USER,
точные дескрипторы и связь Connection с источником. Включённый ящик
отклоняется до создания публикации, если Connection отключён. Первый импорт занимает
только Connection без предыдущей конфигурации. Последующие ревизии должны
сохранять исходную связь. После detach настройки принадлежат UI, и дальнейшее
согласование источника сохраняет эту границу. Приёмка публикации требует
совпадения версий и подтверждённого readback. Runtime-доступ и отправка писем
остаются под отдельными grants, Human Gate и проверками текущего владельца.

# Карта контракта

| Сценарий | Источник полномочий и путь | Pins, переход, результат и потребители |
| --- | --- | --- |
| Создать Connection | Активный USER → POST personal-email-mailboxes → PersonalEmailMailboxService.CreatePersonalEmailMailboxConnection → специализированная owner command | Fixed EMAIL package, tenant/creator/owner/self ACL назначает сервер в одной транзакции; произвольные owner/project/grants не принимаются; существующие connection audit/event/read |
| Сверить setup после потери ACK | Тот же проверенный USER → GET personal-email-mailboxes/mutation-receipt → GetPersonalEmailMailboxMutationReceipt | Только CREATE/CONFIGURE/REVOKE, exact actor/operation/idempotency key, current Connection access; историческая квитанция не заменяет fresh ownership read, чужие receipt скрыты |
| Настроить владельца | Проверенный пользователь → специализированный mailbox owner endpoint/RPC → CP owner command | Сначала текущий Connection/owner, затем If-Match и idempotency; только собственный USER либо явно выбранная SERVICE, disable/drain и новое поколение; connection event/read |
| Настроить credentials | Собственный mailbox view → существующий credential endpoint → CP materializer → immutable descriptor owner | Проверка owner до материализации и повторно при commit; Secret bytes не входят в read DTO, события или логи; текущие connection version и owner generation |
| Проверить SMTP/IMAP | Текущий конфигуратор → прежняя TestConnection → integration-gateway health claim → email owner authorization → bridge | Сервер закрепляет owner generation, mailbox revision/source digest в неизменном test snapshot; claim/Begin/completion отвергают замену владельца или конфигурации; это READ без отправки письма |
| Читать/редактировать настройки | Проверенный текущий владелец → существующие mailbox draft/query commands | Managed-by UI/GIT, exact immutable revision, OCC и eligibility проверяются до read/replay; чужой mailbox скрыт |
| Git import | Поставляемый файл установки → внутренний Control Plane → exact Connection под publication lock | Системный субъект проверяет `PROJECT_SHARED`, поколение и неизменяемый source mapping. Ревизия и digest задают точный replay. `GIT_SYNC` пишет publication, receipt, audit и connection events существующим потребителям |
| Validate/publish/bind | Тот же владелец → managed mailbox owner → существующая publication queue/reconciler | Проверяются provider profile, exact descriptors и их owner generation; publication claim/recovery/commit закрепляют прежние configuration и connection versions; событие не означает внешнюю доставку |
| Preflight/run/continuation | Root USER из owner run/session lineage → CP launch owner → mailbox-specific dispatch | Чужой или отсутствующий owner, revoked/stale credentials, schedule под USER и непригодная публикация закрывают запуск; положительный результат не является grant |
| Resolve invocation | Проверенный runtime lease → CP integration owner → текущий grant/package/mailbox | Immutable mailbox revision/source digest/owner generation/root initiator закрепляются с новым intent; повтор сохраняет прежний pin; существующие invocation/Gate/read paths |
| EACH/SCOPED | Проверенный approver → существующий owner gate/approval scope ledger | Mailbox policy может запретить либо ужесточить package minimum. Согласие имеет exact mailbox/folders/operation/root or initiator lifetime, срок и лимит; смена pins требует нового Gate |
| Claim/Begin | Gateway lease/fence/generation → CP claim → email-bridge authorization/receipt Begin | Повторяются root eligibility, owner/revoke/version/policy, current mailbox pins и расход существующего SCOPED consent; descriptor не передаётся через generic PAT admission |
| Native effect | Email bridge после startup barrier → exact SMTP/IMAP egress/TLS | Единственный effect после durable owner Begin; native protocol и receipt сохраняют прежние bounds, quarantine и attachment trust |
| Complete/UNKNOWN | Exact bridge binding → CP receipt/report owner | Точный lease/fence/input/effect; потеря ответа не доказывает отсутствие действия. UNKNOWN остаётся неопределённым, автоматического resend нет; authoritative receipt/read |
| Revoke/expiry/cancel/retry | Собственный owner command либо существующий run/scope lifecycle | Новые Begin запрещены; старые admission/receipt/history не переписываются. Retry использует новое owner attempt/revision; UNKNOWN не превращается в доказанный remote cancel |

Новые события исполнения не вводятся: используются действующие invocation,
RunEvent, connection event, audit и защищённый receipt/read path. Новое согласие
не хранится во втором почтовом consent store: ограничение принадлежит
`integration_approval_scopes`, а почтовые pins только связывают его с точным
mailbox context и не выдают самостоятельного права.

## Точные операции настройки владельца

`ControlAPIGatewayOperations` связывает отдельный `PersonalEmailMailboxService`
с уже проверяемыми owner permissions. Generic mailbox credential/profile не
подменяет эту регистрацию. Все шесть операций принадлежат только
`control-api-gateway` с обычной пользовательской identity, без project selector:

| RPC | Operation / permission | Привязка запроса |
| --- | --- | --- |
| CreatePersonalEmailMailboxConnection | `platform.email-mailboxes.mine.create` | Полный Proto digest и idempotency; существующий resource/version не принимаются |
| ListMyEmailMailboxes | `platform.email-mailboxes.mine.list` | Полный Proto digest, без resource/version/idempotency |
| GetEmailMailboxOwnership | `platform.email-mailboxes.ownership.read` | Полный Proto digest и exact Connection locator |
| ConfigureEmailMailboxOwnership | `platform.email-mailboxes.ownership.configure` | Полный Proto digest, exact Connection/version и idempotency |
| RevokeEmailMailboxOwnership | `platform.email-mailboxes.ownership.revoke` | Полный Proto digest, exact Connection/version и idempotency |
| GetPersonalEmailMailboxMutationReceipt | `platform.email-mailboxes.receipts.read` | Read-only operation/key внутри полного Proto digest; не создаёт effect либо retry |

`make test-personal-email-operations` проверяет соответствие всему generated
service descriptor, request profiles и отсутствие этих методов в worker maps.
Это локальный контрактный профиль без PostgreSQL, Docker или внешней сети.
Он не доказывает выдачу proof, обслуживание signed policy110, её rotation или
установленный startup. Canonical110 материализуется только поверх настоящей
108/109; private candidate, построенный на107, не заменяет такую поставку.

Миграция049 требует настоящего predecessor048 после047. До его квалификации
не назначаются предполагаемые schema counts, catalog digests, purge graph
fingerprints либо restore proofs. Тело `capture_integration_health_input` взято
из actual Goose048
`services/internal/control-plane/cmd/cli/migrations/20261114160000_managed_ssh.sql`
на точном source `5c3671d8273b8e1d9e58a73f464dbd3e242e28a9`;
SHA256 миграции — `4ab4acba9922351377202e8421fee9d91e0712cff591a7b1fa03ce26e20938df`.
Этот pin исходного SQL не заменяет полного release manifest и его квалификации.

В общий trigger добавлены только локальная переменная и EMAIL hook перед
штатным назначением snapshot/digest. EMAIL принимает только `NONE`, получает
текущий mailbox snapshot через прежний owner guard и отклоняет его отсутствие.
Прежние `SET search_path=pg_catalog,control_plane`, ветки NONE/TOKEN/BI,
проверка неизменности всех pins, SERVICE/target
guards и ранний terminal return сохраняются побайтно. После добавления EMAIL
snapshot тот же общий код назначает digest; закрытие после revoke не требует
свежего mailbox owner и не переписывает retained evidence. Это source-only
composition: actual PostgreSQL и полный048→049 ещё `NOT RUN`.

# Матрица операций

| Операции | Минимальное согласие |
| --- | --- |
| health, mailbox/list/search/read/thread, attachment list/read, status receipt | NONE |
| mark read/unread, move/archive, draft create/update/delete | HUMAN_SCOPED |
| send, reply/reply-all, forward, message delete | HUMAN_EACH_EFFECT |

`DENY` mailbox policy всегда запрещает операцию. Прежний `HUMAN_GATE` сохраняет
EACH даже для операции, которой package допускает SCOPED. Разрешение mailbox
`ALLOW` само не ослабляет minimum package. Согласие SCOPED не переносится на иной
mailbox, папку, destination, credential generation, изменённую конфигурацию либо
другую операцию. Операции чтения не предоставляют право отправки.
Историческая shipped1.4.2 с прежними NONE grants не получает runtime fallback
в1.5.0: проверяются её настоящие immutable source bytes и digest, включая UI/Git
варианты. Восстановление принятой mailbox configuration сохраняет отдельный
startup replay protocol и само не разрешает исполнение старого package.

У SCOPED-вызова папка обязательна; MOVE/ARCHIVE также требуют явного destination,
а draft create/update — явных `to`, `cc`, `bcc` (пустые значения задаются явно).
Все получатели и destination входят в server-derived scope. Изменение темы либо
текста черновика не меняет scope, скрытая смена cc/bcc/destination требует нового
согласия. NONE-чтение не наследует write consent.

Отзыв или изменение владельца сразу закрывает будущий Begin. Ограниченная owner
maintenance затем завершает устаревшие READY/WAITING_APPROVAL, RUNNING READ leases и read-only health
записи (до100 за транзакцию). Открытый Gate и его ожидающие доставки закрываются
в той же транзакции; CLAIMED доставка становится UNKNOWN. Отмена Gate публикует
`OWNER_GATE_RESOLVED`, invocation остаётся доступной через защищённый граф/read;
отказ уже одобренного вызова публикует прежний integration outcome event.
Оба пути используют существующий continuation. Использованные consent counters
не возвращаются. До следующего batch новая READ success completion всё равно
перепроверяет текущие mailbox pins под Connection lock перед invocation lock;
старый lease не разрешает выдать результат после revoke. Та же проверка обязательна
для частичного ResultSummary при FAILED/UNKNOWN: caller Success не является
источником authority. Пустая failure metadata может завершить exact lease без
выдачи данных. Native-сценарий создаёт
100 настоящих OPEN Gate до свежей READ lease и проверяет именно этот промежуток.
RUNNING mutation и UNKNOWN_OUTCOME не объявляются отсутствующими,
не получают выдуманного receipt и не отправляются повторно. Повторное APPROVE
EACH/SCOPED перед решением заново проверяет mailbox корневого инициатора.

# Исходный срок и исторический факт

Новый dispatch credit ограничен исходными lease/authorization window и наиболее
ранним текущим allowance всех необходимых permissions. Используется общий
`requireAccessUntil`, без второго evaluator. Исходный stored expiry не растёт
при повторе либо восстановлении более долгого grant. Перед COMMIT и после
фактического ACK проверяется тот же monotonic deadline; просроченный ответ не
выдаёт usable decision. Scan/SQL/cancellation сохраняют `Unavailable`, а не
превращаются в доказательство отзыва полномочий.

| Состояние | Новый dispatch / disclosure | Исторический report |
| --- | --- | --- |
| Текущий actor/source, live credit | Только исходный intent и его deadline | Первичная UNKNOWN receipt требует fresh authorization |
| Исходный credit истёк либо owner отозван | Не выдаётся; восстановленный grant не оживляет stored intent | Exact уже admitted receipt принимает поздний факт от проверенного email-bridge, без продления lease/credit |
| Уже сохранённый terminal факт | Не разрешает новый effect | Только точный replay; несовпавшие receipt/source/fence/generation отклоняются |

Для admitted report владелец повторно блокирует исходную invocation и связывает
её с существующими receipt/authorization, workload, lease, fence и generation.
Это maintenance существующего факта; READ data, новая SMTP/IMAP команда и
новая attempt этим путём не выдаются. Состояние внешнего действия не меняется на
«не произошло» из-за истечения actor grant.

Development native fixture отдельно проверяет успешный SELECT с client Scan
failure без SQL abort, неизменность owner rows, успешный COMMIT с ACK после
реального finite ACL expiry, отказ replay после восстановления access и fresh
health test ref. Другой сценарий сохраняет поздний admitted mutation факт после
owner revoke при ещё живой исходной lease. Эти новые native сценарии требуют
отдельного фактического запуска; прежние development PASS их не покрывают.

Native READ перед передачей mailbox data дополнительно проверяет исходный
operation context и его deadline. Успешный IMAP ответ и завершённое socket I/O
не разрешают выдать поздний buffered result. Истёкший READ возвращает пустой
result и `Unavailable`; positive health readiness также не выдаётся после
deadline. Mutation completion, admitted report, terminal receipt metadata и
независимый cleanup сохраняют собственный прежний lifecycle. Профиль
`TestPersonalMailboxReadHandoffDeadline` получает настоящие данные через TLS
IMAP, затем задерживает только handoff за исходный срок; test-only wrapper не
создаёт данные и не повторяет внешний запрос.

# SMTP/IMAP профили

`CUSTOM` сохраняет явную конфигурацию и прежние проверки endpoints. Профиль
`TIMEWEB_PASSWORD` использует `smtp.timeweb.ru:465` implicit TLS либо `:587`
STARTTLS и `imap.timeweb.ru:993` implicit TLS либо `:143` STARTTLS. Профиль
`GMAIL_APP_PASSWORD` использует `smtp.gmail.com:465` implicit TLS либо `:587`
STARTTLS и `imap.gmail.com:993` implicit TLS. Host равен SNI; plaintext,
skipTLSVerify и произвольный alternate host не допускаются.

Gmail app password выбирается явно и доступен лишь если политика реального
аккаунта это разрешает. Название профиля не подтверждает успешную аутентификацию.
Обычный пароль Google не предлагается. Этот профиль не заявляет OAuth consent,
refresh/revoke lifecycle или XOAUTH2. Существующий OAUTHBEARER остаётся отдельным
протоколом и не отображается как Gmail OAuth.

Проверены Context7 `/websites/developers_google_workspace_gmail_api` и
`/websites/timeweb_cloud`; точные SMTP/IMAP настройки сверены по официальным
[Timeweb](https://timeweb.cloud/docs/mail/email-clients-configuration),
[Google endpoints](https://developers.google.com/workspace/gmail/imap/imap-smtp),
[Google app passwords](https://support.google.com/accounts/answer/185833) и
[Google XOAUTH2](https://developers.google.com/workspace/gmail/imap/xoauth2-protocol).

# Bootstrap почтовой проекции

Оба существующих entrypoint (`email_projection.py` и local deploy) запрашивают
JSON successful create ACK, сверяют исходные seed bytes и закрепляют его UID
перед GET. Замена UID при тех же имени/labels закрыто отвергается; malformed либо
пустой successful ACK не заменяется усыновлением GET. Прежняя опубликованная
CP-проекция не записывается повторно. Неизвестный исход create не порождает
вторую запись: сохраняется только прежний readback существующей CP-проекции,
а не доказательство отдельного creation receipt. Проверка без live Kubernetes:
`make test-email-projection-bootstrap`. Это существующий UID invariant
`GUIDE-DOC-003`, а не новый journal либо общий target/kubeconfig refactor.

Проверены Context7 `/websites/kubernetes_io`,
[семантика server UID](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/)
и [JSON output kubectl](https://kubernetes.io/docs/reference/kubectl/).

# Квалификация и ручная проверка

Полный unit проверяет два пользователя и два проекта, configure/replace/revoke,
личный root и делегирование, каждый из NONE/EACH/SCOPED, mismatch папок и
credential/configuration pins, срок/лимит/конкуренцию, отказ stale completion и
native SMTP/IMAP TLS/auth/lost ACK. Наличие кода или draft не означает PASS.
Итоговые команды, source SHA и фактические результаты фиксируются перед PR.

Профиль обязательно собирает текущий gateway test binary под race detector.
Настоящие health/READ/SCOPED/EACH claims владельца проходят штатный Proto caster,
отдельный процесс gateway и его HTTP adapter с локальным bridge fixture.
Проверяются fence, отсутствие generic credential, отказ подмены resource scope,
истёкшего claim и отсутствующего binding до запроса. Этот seam не заменяет
отдельный настоящий SMTP/IMAP TLS loop и будущий policy110 transport.

Публичный профиль `make test-personal-email-owner-development` готовит отдельную
пустую disposable БД текущими исходниками до exact head049 и проверяет owner
transactions двух обычных пользователей под Go race detector: scoped Gate,
receipt Begin и UNKNOWN остаются действиями оснастки без SMTP/IMAP effect.
Он не является full-schema upgrade,
backup/restore или проверкой transport policy110. Обычный закрытый selector
`test-control-plane-postgres` продолжает отклонять049 до появления её полного
верификатора. Для ограниченного локального запуска используются уже кешированный
PG18.3 и `KODEX_TEST_POSTGRES_STORAGE=ram`; это не обращение к рабочему кластеру.
Независимый source `fdcd012451c75bf34b6f81b69588037865b53dc8` сохраняет старый
development seed для отдельной оснастки; на нём завершён только прогрев трёх
бинарников, actual owner PostgreSQL не запускался. Новый EMAIL hook требует
typed shared-health columns и BI source048: прежний прямой046→049 seed не
подтверждает его совместимость и не подменяется permissive fallback.

`make test-personal-email-protocol-native` проверяет реальные loopback TLS
SMTP/IMAP, AUTH PLAIN/LOGIN, CA/SNI и отсутствие message effects для точных
Gmail/Timeweb endpoints. Gmail IMAP остаётся implicit TLS на993; Timeweb IMAP
проверяется отдельно на993 и143 с обязательным STARTTLS для обоих допустимых
режимов SMTP. Эти проверки не разрешают plaintext authentication. Настройки
Timeweb повторно сверены через Context7 `/websites/timeweb_cloud` с официальным
контрактом `email-clients-configuration`. In-memory owner/receipt fixtures не заменяют
PostgreSQL либо доступ к реальным аккаунтам. Полный `make test-email-bridge`
сохраняет существующие durable receipts и protocol fault cases под race detector.
Его RAM mode использует тот же `KODEX_TEST_POSTGRES_STORAGE=ram`: cached exact
PG18.3, read-only container root, ограниченные tmpfs/cgroup с фактическим readback;
cleanup адресуется только подтверждённому create ACK container ID.

Ручная проверка: войти обычным пользователем, настроить собственный mailbox,
выполнить read без Gate; одобрить SCOPED для одной папки и убедиться в отказе
для другой; отдельно подтвердить одну отправку; отозвать доступ и убедиться,
что новое действие закрыто после restart. Живая отправка требует конкретного
аккаунта и разрешённых адресатов. Сейчас таких входов нет: live проверка
`NOT RUN`, внешние credentials и получатели не выводятся из настроенных hosts.

Новое личное подключение создаётся включённым, в состоянии `NOT_CONNECTED`.
Это позволяет назначенному владельцу добавить почтовые credentials, выбрать
профиль и опубликовать конфигурацию; enable сам по себе не разрешает отправку.
Проверка SMTP/IMAP принимает текущего личного владельца либо конфигуратора явно
назначенной сервисной identity. До публикации соответствующей конфигурации
команда возвращает conflict, не переводит подключение в `TESTING` и не ставит
проверку в очередь. Для смены владельца или перехода к сервисной identity нужно
явно отключить подключение и завершить зависимую работу; обычное право настройки
своего ящика не даёт права назначать сервисного владельца.

Schema049 и policy110 следуют за квалифицированными047/048 и108/109.
FK новых immutable mailbox pins допускают отложенную проверку только внутри
транзакции; обычный runtime по-прежнему не получает UPDATE/DELETE. Их delete
guard учитывает существующий защищённый `project_purge_authorized()`.
Перед `GRANT SELECT,INSERT` обе таблицы явно выполняют `REVOKE ALL` для `PUBLIC`
и `control_plane_runtime`, снимая базовые default privileges. Рабочие pin queries
выполняют только SELECT/INSERT; row locks адресуют связанные connection/invocation,
а не pin aliases. Public owner-development fixture содержит проверку actual ACL
отдельной nonowner session и отрицательные UPDATE/DELETE/column UPDATE/PUBLIC SELECT
overgrant cases; новый regression пока `NOT RUN`. Готового049 schema-readiness
consumer ещё нет, поэтому эти ACL не объявляются проверенными через прежний
projection-readback Check. Purge privileges этим изменением не расширяются.

Чтение effect receipt и выдача reconciliation decision сохраняют наиболее ранний
current integration/run ACL allowance и проверяют его до/после COMMIT в исходном
monotonic budget. Resolve возвращает копию decision с usable expiry не позднее
исходного stored expiry и текущего ACL; RPC timeout ограничивает передачу ответа,
но не заменяет срок самого grant. Исторические receipt/report facts и stored
decision не переписываются: после восстановления доступа свежий readback видит
прежние факты. В owner-development добавлен отдельный synthetic native профиль
`TestEmailReceiptDisclosureDeadlineComponent`: finite binding, задержка actual
COMMIT ACK за expiry, zero result при живом caller, затем restore и exact readback.
Новый код и regression пока source-only, Go/PostgreSQL `NOT RUN`.
Это подготовка owner-таблиц к атомарному purge: закрытый граф, его fingerprints
и full-schema/restore qualification049 ещё должны быть захвачены на настоящем048.
Production migration/delivery и rollback выполняются отдельным адресным owner
протоколом; возврат к старому runtime не должен повторить неопределённую отправку
или восстановить отозванное согласие.

## Адресная поставка cohort 046→049

Профили `personal-integrations` и `personal-integrations-backup` существующего
`tools/release/control-plane-migration.mjs` закрепляют predecessor046 и final049.
В одном остановленном окне штатная CLI исполняет `up-personal-integrations`
либо `up-personal-integrations-backup-maintenance`; сама CLI проверяет exact
catalog и Goose history перед047, перед048 и перед049. Отдельного запуска
consumer между версиями нет. Частичный FAIL на047/048 не разрешает handoff,
возврат старого runtime или автоматический повтор неизвестного DDL.

Image cohort закрыт: `control-plane`, `runtime-controller`, `integration-gateway`,
`control-api-gateway`, `staff-control-center`, `secret-broker`, `email-bridge`,
`egress-gateway`, `artifact-retention`. `interaction-gateway` и `agent-runner` этой волной не меняются.
`artifact-retention` уже установлен: его прежний writer входит в общий stop/join
до DDL, а issuer-specific template/network/material переход остаётся отдельным
точным owner prerequisite. Новый пустой Deployment вместо старого UID не создаётся.

Plan требует clean exact source, девять stock component locks и выполненные
node image proofs для exact target, а также положительный stop/join и Service
isolation API через существующие `component-api-maintenance`/`component-api-traffic`.
Одного replicas=0 недостаточно. Generated `personal-integrations-*-migration-*.sql`
и manifest являются текущим exact baseline; изменение любого captured SQL
или reader закрывает plan до нового capture/generation. Произвольный schema
number, переданный fingerprint или таблица expected counts не принимаются.

Normal и canonical backup разделены полностью. Backup требует существующие
`backup-controller-maintenance` plan/evidence, отсутствие backup sessions и
точный canonical SELECT/defaultACL на обеих границах. `table-only-backup`
остаётся offline restore-профилем и не допускается как live backup migration.
Исторические migration profiles и их manifest не меняются.

Синтаксис нового плана использует прежние аргументы; значения ниже — только
пути и публичные pins, без credentials:

```sh
node tools/release/control-plane-migration.mjs plan \
  --profile personal-integrations-backup \
  --context "$CONTEXT" --kubeconfig "$KUBECONFIG_PATH" \
  --source "$SOURCE" --revision "$SOURCE_SHA" --plan "$DIR/migration.json" \
  --base-render "$DIR/migration-job.yaml" \
  --component-publication "$DIR/component-publication.json" \
  --portable-target "$DIR/target.json" \
  --personal-prerequisites "$DIR/personal-prerequisites.json" \
  --api-close-plan "$DIR/api-close.json" \
  --api-close-evidence "$DIR/api-close.jsonl" \
  --api-isolation-plan "$DIR/api-isolation.json" \
  --api-isolation-evidence "$DIR/api-isolation.jsonl" \
  --backup-maintenance-plan "$DIR/backup-stop.json" \
  --backup-maintenance-evidence "$DIR/backup-stop.jsonl"
node tools/release/control-plane-migration.mjs inspect \
  --profile personal-integrations-backup \
  --context "$CONTEXT" --kubeconfig "$KUBECONFIG_PATH" \
  --plan "$DIR/migration.json" --evidence "$DIR/migration.jsonl"
node tools/release/control-plane-migration.mjs handoff \
  --profile personal-integrations-backup \
  --context "$CONTEXT" --kubeconfig "$KUBECONFIG_PATH" \
  --plan "$DIR/migration.json" --evidence "$DIR/migration.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

Если первый immutable Job завершился `Failed` после частичного commit, прежние
CP, workers, API и backup остаются остановленными. Команда `retry` использует
те же `--plan`, `--evidence`, `--profile`, `--context`, `--kubeconfig` и
`--confirm APPLY-STAGING-CP-MIGRATION`; новый plan либо backup stop не создаётся.
Допускается ровно один Job `control-plane-migrate-20261114170000-attempt2` с
прежними image, argv и всеми параметрами исполнения. Повтор вызывает тот же Go
upgrader: он продолжает только с квалифицированного зарегистрированного head.

| Состояние исходной попытки | Требуемый readback | Допустимый переход и consumer |
| --- | --- | --- |
| CREATE ACK неизвестен, Job найден | Exact имя/root annotation/spec/UID и Job-owned Pod | `inspect` читает прежний Job; новый create не допускается |
| Job либо его Pod отсутствует/неизвестен | Положительный join отсутствует | Закрытый отказ, остановленный cohort сохраняется |
| Exact Job `Failed`, единственный Pod закончен с nonzero exit и pinned imageID | Durable FAIL/parent UID + свежий stopped join всех writers и mode-specific owner proof | Explicit `retry` записывает intent2 перед единственным create; root plan/backup CAS не меняются |
| Текущий head046/047/048/049 | Exact catalog/history своего head и normal/canonical-backup mode, все idle counters0 | Только прежний Go upgrader; промежуточные heads не выдаются за046/049 |
| Intent2 записан, create/readback неизвестен либо Job исчез | Сохраняются intent2 и известный UID, если он получен | Только чтение attempt2; третьей попытки и повторного create нет |
| Attempt2 `PASS` + exact final049 owner proof | Новый recognized UID, immutable Job/Pod и прежний stopped/staged cohort | Existing handoff/staged-policy/backup terminal consumers принимают только текущую попытку; parent FAIL остаётся в журнале |
| Attempt2 `FAIL`/неопределённый исход | Сохранённая history и authoritative inspect | Автоматического продолжения нет; backup resume на047/048 закрыт |

Публичная локальная проверка протокола: `make test-personal-integrations-migration-recovery`.
Она использует синтетический Kubernetes transport и captured metadata;
не квалифицирует PostgreSQL recovery SQL, реальный failed Job либо поставку.
Действующий `PERSONAL_INTEGRATIONS_DELIVERY_PREREQUISITE_UNQUALIFIED` сохраняется:
новый синтаксис не открывает публичные effects до общей приёмки.

Для normal выбирается `personal-integrations`, backup аргументы отсутствуют.
Название confirmation сохраняет прежний CLI protocol и не расширяет target.
Handoff меняет только image/свои metadata; env, TLS, NetworkPolicy и projection
не применяются неявно. Их exact delta обязана быть доказана заранее.

План закрепляет `PHASE_BOUND_OWNER_READBACK_V1`. `check`, `stop`, `apply`,
`retry` и `handoff` заново проверяют исходный source, установку, authority107,
материалы `artifact-retention` и допуск образов на узлах. После подтверждённого049
размещаются exact policy projection и issuer assets. Внутренний OPEN запускает API
за изолированным Service. `start-readers` для каждого компонента проверяет свежий
подписанный DELIVER110. `resume` подтверждает READY девяти компонентов и подписанный
terminal110. Внешний Service восстанавливается после проверки этого результата
по тому же журналу миграции и свежему owner readback. Старый API reader045 для
двух новых профилей закрыт.
Локальная точка Node-проверок без Go/PostgreSQL/кластера:
`make test-personal-integrations-migration-release`.

Для переустановки new cohort также допускает исходный canonical base046.
Это не подмена текущего predecessor: полный фактический installation seal
(source, release-lock, profile hash, schema-heads, UID и target hash) остаётся
обязательным, а live DDL отдельно должен быть046. Upgrade от исходных042/043
сохраняет прежний seal. Allowlist исторических profiles не расширяется.

CLI поддерживает explicit rejoin от уже зафиксированных exact047/048. Однако
generic immutable attempt2 требует прежний RESUME_ACK/predecessor046; этот
старый маршрут не является forward recovery частичного cohort. До отдельного
qualified owner протокола exact stage047/048 + failed Job positive join +
исходные stopped cohort/source/OCI/mode такой rejoin в release tool закрыт.
Возобновление старого CP для получения RESUME_ACK не допускается. Это отдельный
integration hook вместе с фазовым delivery verifier, а не автоматический retry.


### Раздельные readback перед issuer assets и authority107→110

`readPersonalIntegrationsStoppedAdmission({sourceSHA,target,admission}, kube)`
читает шесть owned private paths: `planPath/evidencePath` для API close,
`trafficPlanPath/trafficEvidencePath` для generic API isolate и
`migrationPlanPath/migrationEvidencePath` для committed049 cohort. `target`
содержит отдельные `context`, `clusterUID`, `namespaceUID`, `installationUID`,
`installationSHA256`. Generic isolate не получает `migrationRestore` либо
baseline106: он доказывает только физическую изоляцию Service. Проверка SQL049,
Job/Pod и всех девяти остановленных writers принадлежит существующему fixed
`control-plane-migration inspect`. Этот inspect может дописать owned локальный
readback journal; Kubernetes mutation здесь не выполняется.

Возврат `kodex.personal-integrations-stopped-admission/v1` содержит исходный
`apiTarget`, `apiAdmission` и его `apiAdmissionSHA256`, а также отдельный
`cohort049` с `migrationTargetSHA256`. Эти hashes стабильны при новом observation:
в них нет `observedAt` либо счётчиков optional readers. Перед каждым effect
фабрика выполняется заново; сохранённый JSON не заменяет fresh owner read.
`verifyPersonalIntegrationsStoppedAdmission(current,pinned)` сравнивает точную
immutable проекцию. Ни результат STOPPED, ни material LOGIN receipt не являются
подписанным DELIVER/RETIRE authority110. После handoff нужен отдельный STAGED
reader, а старый STOPPED proof не переиспользуется как допуск нового ART spec.

Перед изменением issuer assets проверяются все producers в namespace, включая
шаблоны фоновых задач и ссылки на Secret. При смене снимка из-за планового Job
исполнитель читает список до четырёх раз. Для допуска нужны два одинаковых
соседних снимка, полученных в пределах 20 секунд от начала чтения. Конфликт
полномочий, превышение срока или нестабильный список закрывают допуск до
записи intent.

Для встроенного Issuer cert-manager v1.21.1 может записать пустое значение
`cert-manager.io/issuer-group` в TLS Secret. Допуск требует наличия аннотации
и сверяет её эффективную группу с Certificate. Digest учитывает фактическую
строку аннотации.
[Исходник cert-manager v1.21.1](https://github.com/cert-manager/cert-manager/blob/v1.21.1/pkg/controller/certificates/issuing/internal/secret.go#L1255-L1262).

### PRE_ROTATION проверка staged cohort

`readPersonalIntegrationsStagedAdmission({sourceSHA,target,authorityTarget,
admission,assets,material,issuerMigration,authorityImage}, kube)` предназначен
только для периода после handoff всех девяти workloads и до первого registry CAS.
`admission` сохраняет прежние шесть путей. `assets` содержит `planPath/evidencePath`;
`material` и `issuerMigration` дополнительно сохраняют свои отдельные `sourceSHA`.
`authorityImage` содержит `publicationPlanPath`, `publicationEvidencePath`,
`nodeProofPath` для публикации authority из того же source049. Режим проверки CRI
только `readback`: повторного pull здесь нет.

Фабрика заново читает committed049/STAGED, изоляцию API, current107 owner,
issuer-role00200 Job/Pod/SQL, LOGIN material и его ancestry/exact4files,
source-derived CP/ART template и14 ассетов с TLS Secret UID/metadata custody.
После блокирующих проверок повторяются cohort, material, ресурсы и owner107.
Возврат `kodex.personal-integrations-staged-admission/v1` содержит прежние
`apiTarget/apiAdmission/apiAdmissionSHA256`, `cohort049`, отдельные
`owner107/issuerMigrationReady/materialSource/materialReady/assetsResources`
и `artifactRetentionStage` с hashes прежнего/staged spec, inputs и двух публикаций.
В `owner107` сохраняются immutable coordinates; observation время и optional
readback counters не входят в сравнение плана. Saved JSON не заменяет фабрику.

Historical installation seal authority20260912000200 не переписывается actual
predecessor00200. Для новых personal profiles producer и consumer сохраняют
оба значения раздельно; actual00200 перед effect обязан подтвердить отдельный
owner reader принятой role migration. Fresh reinstall base046 по-прежнему
требует полного канонического installation seal. Старые профили не расширяются.

STAGED фиксирует stopped cohort перед CAS. После заполнения проекций publisher
применяется POST_CAS reader, а допуск readers и внешнего API определяется
соответственно подписанными DELIVER и terminal.

Локальный воспроизводимый reader/derivation контур:
`make test-personal-integrations-admission-local` (Node, bounded240s, synthetic
transport/owner fixtures, без Go/PG/кластера). Полный migration-release target
с exact047/048/049 pins остаётся отдельным обязательным контуром.


Initial `plan` дополнительно требует `--personal-prerequisites` — owned private
JSON `kodex.personal-integrations-release-inputs/v1` с exact ключами
`authorityTargetPath`, `material`, `issuerMigration`, `authorityImage`, `assets`,
`schema`. Объекты material/issuerMigration содержат исходные `sourceSHA` и
`planPath/evidencePath`; authorityImage — три пути publicationPlan/metadata
журнала/nodeProof, assets — будущие два пути plan/evidence. Никаких готовых
boolean/grants или команд в этом JSON нет. Target читается из owned private файла.

Planner вызывает `readPersonalIntegrationsDeploymentInputs` и до своего hash
сохраняет **все три** immutable части: `personalIntegrationsInputs`,
`personalIntegrationsPrerequisites`, `personalIntegrationsRelease`. Последняя
часть содержит exact authority target и прежние refs, а prerequisites сохраняет
original role/material plan/evidence/source proofs и stable owner107 pins.
STAGED factory требует эти исходные части и сравнивает с fresh reads. Две
совпавшие новые квитанции не заменяют прежнюю; missing pins также закрыты.


## Фазы authority107→110

`authority-rotation-transition.mjs` принимает отдельный
`--policy-transition PERSONAL_INTEGRATIONS_107_110`. Глобальный historical
portable registry и Telegram106→107 не расширяются. Подтверждение apply —
`APPLY-PERSONAL-INTEGRATIONS-AUTHORITY`; `abort` нового профиля закрыт.
Rotate plan дополнительно получает прежний `--personal-prerequisites` config,
четыре пути `--maintenance-plan/--maintenance-evidence/--traffic-plan/--traffic-evidence`
и exact `--migration-plan/--migration-evidence`. Они закрепляются как
`personalIntegrationsRelease` и `personalIntegrationsAdmission`, а прежний
полный результат STAGED — как `maintenance`. Publisher остаётся на своём
точном принятом immutable образе; restart меняет только operation annotations.

| Фаза | Авторитетный допуск | Результат / запрет |
| --- | --- | --- |
| PRE_ROTATION | Fresh committed049, все9 STAGED/joined, exact107 owner, role00200, LOGIN, image и v2/14 generation0 custody | Только registry CAS исходного UID/RV/data |
| POST_CAS / lost ACK | Exact desired registry+intent/operation, прежние receipts и refs, fresh STAGED/API, material и поздняя v2 custody; fixed SQL видит прежний RETIRED107 либо точную новую operation/base | Forward rejoin publisher/watch; unpublished intent не разрешает читателей |
| DELIVER | Исходный migration header/Job049/handoff, physical API isolation, CP policy110 UID/hash, v2/14 custody и fixed late SQL опубликованной текущей epoch | `PENDING_AUTHORITY`; можно запускать только exact cohort readers |
| SWITCH / RETIRE | Та же operation/registry/base, монотонная owner phase/snapshot; все подписанные обязательные readers проверяет owner | Новый snapshot может обнулить ACK count; тот же snapshot не может регрессировать |
| TERMINAL | Fresh exact RETIRED owner с полным canonical required set | Подписанный terminal; all9 readiness и внешнее открытие API отдельно проверяет migration lifecycle |

`readPersonalIntegrationsAuthorityDelivered/Terminal({plan,events,freshPlan,freshEvents,
migration:{plan,events}}, kube)` не запускают повторный migration `inspect`:
вызывающий lifecycle уже владеет своим journal lock. Status Job/Pod сохраняют
provenance; свежесть подтверждает поздний fixed owner SQL. Все hash границы
API, authority и migration остаются раздельными. Стабильный `bindingSHA256`
закрепляет operation/registry/base и исходные prerequisites, а не замораживает
snapshot DISTRIBUTE на время законного SWITCH/RETIRE.

Late dynamic Secret metadata подтверждает только прежний UID/manager/allowlist
ключей, opaque generation и TLS custody. Generation не равен snapshot revision;
пустая generation0 restore-only проекция не доказывает ни отказ, ни готовность.
Допуск выдают owner signed required receipts, а не заполненность Secret.

Воспроизводимый synthetic контур —
`make test-personal-integrations-authority-local` (Node, bounded240s).
Живая установка требует подписанного readback110 на её собственном target.
При изменении DDL pins нужно повторить exact capture/generation.

На backup-профиле после `resume` миграции выполняется `resume` backup-контроллера
с exact OPEN планом и его журналом. Этот шаг проверяет успешную текущую
migration Job, канонический ACL049 и durable terminal читателей. После
подтверждённого Ready backup-контроллера восстанавливается внешний Service.


Personal rotation journal имеет версию `PERSONAL_INTEGRATIONS_ROTATION_V1`.
Перед CREATE записывается/fsync intent; prior UNKNOWN/INTENT с отсутствующей Job
и замена известного Job UID закрывают повторный CREATE. Журнал перечитывается
из того же owned FD до resume, а существующий `privateJournal` защищает
производный `<evidence>.custody` lock. Старые personal планы без этого protocol
marker не принимаются как доказательство отсутствия прежнего эффекта.

Исходные PostgreSQL Pod/StatefulSet UID и backend SystemID/databaseOID/SQL pin
совпадают у original107 admission, rotation и status proof и входят в stable
binding. Новое status наблюдение не перепривязывает старую operation к другому
backend; такой переход требует отдельного проверенного восстановления.


Для `personal-integrations` и `personal-integrations-backup` полный compact
component lock ограничен640KiB; base64 ограничен `4*ceil(655360/3)` символами.
Все четыре Git manifest сохраняются целиком, включая source/base/release lock,
production diff и Dockerfile/build receipt hashes. Прежние профили сохраняют
128KiB. Общие prepared/publication остаются ограничены2MiB, приватный migration
plan читается CP CLI строго меньше4MiB, authority readers — не больше4MiB.
Наличие большего per-lock budget не разрешает превысить aggregate budget.
Nested cohort inspect использует executor-relative CLI и конечный8MiB stdout;
backup inspect —4MiB. API restore требует того же exact executor/source SHA,
поэтому одно изменение release tooling не разрешает смешать новый executor со
старыми image/source pins. До остановки cohort используются единый frozen SHA,
его публикации и перепланированные API/backup receipts.


После уже принятого handoff допускается только отдельный проверенный personal
executor. Для `inspect`, `start-readers`, `resume` передаётся
`--personal-executor-sha EXACT_EXECUTOR_SHA`; исходные plan/image/application
SHA не меняются. Точный successor должен быть clean, происходить от исходного
application и менять только закрытый перечень Node operator/tests/docs. DDL,
schema proofs, phase SQL, registry, policy, deploy и application остаются
неизменными; их Git bytes и phase hashes сверяются заново. Первая mutation
сохраняет `PERSONAL_EXECUTOR_BOUND`, дочерний read-only inspect сверяет binding
без записи. Новый непроверенный executor не переиспользует прежний binding.

API `restore` этим executor планируется с `--application-source` и
`--application-revision` исходного application; оба SHA сохраняются раздельно
в `sourceCompatibility`. Telegram profile и его прежний resolver не меняются.
Backup resume использует существующий `--stopped-source-sha` и exact owner SQL;
его child получает тот же executor автоматически. Internal CAG OPEN остаётся
с исходным application SHA и изолированным Service. Только настоящий typed
OPEN/staged spec допускается общим `imageScope`, `RESTORED_READERS_READY` для
personal не подходит.

`start-readers` возвращает `READERS_STARTED_PENDING_READINESS`, а не READY.
`resume` не ожидает rollout автоматически: он требует fresh READY всех девяти
и signed RETIRED110 и возвращает `RESUMED`. Backup apply может вернуть
`CONVERGING`; до внешнего открытия требуется его `status` со `state=RESUMED`.
Late personal child inspect ограничен240s (тот же full049 SQL и девять serving
inventories и successor source checks); ранний pre-rotation cohort inspect180s, прежние120s и Telegram backup300s сохраняются. Owner readback
имеет отдельный30s freshness bound; увеличение child timeout его не продлевает.

## ART API defaults и повторный handoff

В личных профилях сравнение ART сохраняет исходные planned spec и их hashes.
Только добавленные pinned `internal-rpc-authority-issuer`,
`platform-worker-grant-agent`, `internal-rpc-authority-socket-init` допускают
полную точную API-проекцию отсутствующих container/probe/fieldRef defaults.
Явные поля, прежний main, UID и остальные spec остаются строгими.
Принятый `PAIR_HANDOFF_INTENT` присоединяется без повторного PATCH ART.

Проверенный successor выполняет только поздний `handoff` с явным
`--personal-executor-sha`; application source остаётся прежним immutable SHA.
Полный049 postcondition, остановка/join всех девяти и изоляция Service
обязательны до повторного чтения прежнего107 fixed module, сверки всех
исходных deployment prerequisites и durable executor binding перед эффектом.
`plan/check/stop/apply/retry` по-прежнему требуют исходный executor.

## Forward-регистрация ART worker после049

`up-artifact-retention-grant-catalog` принимает только head049 либо уже
применённый `20261121000100`. Новая Goose-миграция сверяет два точных прежних
CHECK, добавляет только `artifact-retention` и enabled generation1. Прежние
строки, public backup grants, RLS, instance receipts и watermark не меняются.
Успех имеет отдельную квитанцию
`Artifact retention grant catalog migration completed at version 20261121000100`.
Повтор проверяет новые CHECK и активную регистрацию, не восстанавливает
отозванную регистрацию и не снижает generation floor. Итоговое чтение выполняет
принадлежащая мигратору транзакция `REPEATABLE READ READ ONLY` с локальной
ролью `control_plane_owner`. Unknown COMMIT требует readback.

Для новых планов `personal-integrations-backup` с
`kodex.personal-integrations-staged-inputs/v2` запускайте
`plan-artifact-grant-catalog` и `apply-artifact-grant-catalog` сразу после
`handoff`, пока все девять Deployment находятся в `STAGED` с нулём реплик
и подтверждённой остановкой Pods. Оператор повторно проверяет закрытый
изолированный API, остановленный backup, исходный authority 107 и полный
postcondition 049. Job использует опубликованный digest migrator 049 из того
же плана. Допуск к ротации 110 и запуску readers требует квитанцию ART050,
подтверждённую чтением PostgreSQL. Новый handoff назначает ART ту же группу
socket volume, что и канонический render.

```bash
node tools/release/control-plane-migration.mjs plan-artifact-grant-catalog \
  --profile personal-integrations-backup --context "$CONTEXT" --kubeconfig "$KUBECONFIG_PATH" \
  --plan "$MIGRATION_PLAN" --evidence "$MIGRATION_EVIDENCE"
node tools/release/control-plane-migration.mjs apply-artifact-grant-catalog \
  --profile personal-integrations-backup --context "$CONTEXT" --kubeconfig "$KUBECONFIG_PATH" \
  --plan "$MIGRATION_PLAN" --evidence "$MIGRATION_EVIDENCE" \
  --artifact-grant-catalog-plan-sha "$ART_PLAN_SHA256" --confirm APPLY-STAGING-CP-MIGRATION
```

Для уже записанных планов v1 поздний repair использует
`--personal-executor-sha` и проверенную публикацию мигратора в
`--artifact-grant-catalog-inputs`. Он допускается после подтверждённого
исправления ART grant. Нужны `READERS_BOUND`, внутренний `OPEN` за
изолированным Service и свежий подписанный `RETIRED` authority 110 той
же операции. Окно закрывается при фиксации терминала cohort или начале
возобновления backup.
Применение принимает точный `--artifact-grant-catalog-plan-sha`, связывает
Job UID и фактический образ завершённого Pod. Потеря ответа на создание
разрешается чтением той же Job по журналу; новая Job не создаётся.

Публичная synthetic-проверка: `make test-artifact-retention-grant-postgres`
с существующим disposable RAM-профилем. Матрица:049 отвергает ART v2;
после миграции два Pod instances и exact replay проходят; замена envelope,
низкое поколение, expired grant, disabled registration и неизвестный workload
отклоняются. Сравниваются прежние строки, owner/ACL/RLS и backup defaults.
Это не проверка установленного стенда и не разрешение новых mail effects.

## Типы параметров обычного ART claim после 047

`claim_due.sql` явно закрепляет `batch_size` и `lease_seconds` как PostgreSQL
`integer`. Неявный тип `LIMIT` — `bigint`, поэтому прежний `greatest` выбирал
несуществующий overload `artifact_manual_retention_claim(text,bigint,integer)`.
Миграция и overload функции не меняются; сохраняются SKIP LOCKED, общий budget
пачки, lease, manual/reconcile-only поля и запрет повторного внешнего DELETE.
`make test-artifact-retention-claim-postgres` применяет 049 в одноразовой БД.
Прежний запрос даёт ошибку разбора `42883`. Исправленный запрос имеет параметры
`int4/text/int4` и выполняется под ролью ART.
