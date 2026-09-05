---
id: OPS-EMAIL-1037
title: Email bridge и границы интеграции
type: operations
status: approved
owner: developer
version: 1.1.1
updated: 2026-09-05
---

# Сценарии #1037

| Сценарий | Actor и authority | HTTPS/owner | Idempotency, state и effect | Readiness/read |
| --- | --- | --- | --- | --- |
| YAML/UI | Проверенный управляющий actor у #1046 | Configuration email-bridge/v1 → immutable control-plane revision → read-only projection | managed_by/source/revision; bridge не редактирует конфигурацию | Строгий разбор, persisted revision watermark |
| IMAP list/search/read/thread/attachments | Проверенный invocation, четыре scopes с exact folders | HTTPS → typed owner resolve → IMAP adapter | UID + UIDVALIDITY; native search, BODY.PEEK; cursor связан с tenant/connection/config/folder/filter | SMTP и IMAP обязательны по MVP-UI-41; POP только compatibility |
| IMAP flags/move/archive/delete | Exact source и destination folder; operation policy/gate | UID STORE/MOVE/COPY/UID EXPUNGE | Durable unknown до первой mutation; только exact UID EXPUNGE, никогда общий EXPUNGE | Receipt содержит UID/UIDVALIDITY/folder; неизвестный исход блокирует новый key для source |
| Draft create/update/delete | Exact drafts_folder, UIDVALIDITY, expected content digest при update | APPEND с \\Draft; replacement APPEND → UID delete старого письма | IMAP не предоставляет атомарный replace; partial/unknown сохраняется без повторного APPEND | Message-ID связан с receipt; IMAP-поиск и owner decision, не слепой retry |
| POP read/search/download | invocation token, mTLS integration-gateway; online resolve у #1046 | /v1/mailbox-operations → ResolveEmailAuthorization → POP | user∩agent∩connection∩resource; UIDL, bounded scan; без внешней mutation | Авторизованные protocol auth/NOOP и UIDL; POP не имеет folders/read flags |
| SMTP send/reply/reply_all/forward | Те же scopes плюс exact recipients/from и gate | ResolveEmailAuthorization до credential projection; bridge владеет receipt | reserve unknown до DATA; accepted только после final 250; timeout/crash остаётся unknown | receipt GET; accepted не означает delivered |
| POP delete | Exact UIDL и policy/gate | DELE + QUIT, owner receipt bridge | reserve unknown до DELE; deleted только после успешного QUIT | GET receipt; unknown не повторяется |
| Gate reject/pending | Owner #1046 | resolve возвращает отказ или gate_approved=false | Нулевой credential/protocol effect | Новый owner-approved grant, тот же immutable input |
| Revocation/cancel | Owner #1046 закрывает grant/credential | Каждый вызов resolve свежий, кэша authority нет | До protocol effect проверяются поколения descriptors | Unavailable/unknown state fail closed |
| Retry/crash/duplicate | Тот же tenant/mailbox/effect | Атомарная reserve в PostgreSQL | Один победитель; mismatch 409; unknown никогда не READY | Устойчивый read, без фоновой повторной отправки |
| Cancel после provider response | Полномочия не продлеваются; новый protocol effect запрещён | Текущий handler завершает только запись ранее зарезервированной receipt | Независимый completion context от composition root, не более 3 секунд; SMTP accepted и известные UID частичного IMAP сохраняются | При отказе записи остаётся исходный durable unknown; повтор эффекта запрещён |

События bridge не публикует: source of truth receipt PostgreSQL и авторитетный
HTTPS read. Run/turn/gate lifecycle остаётся у control-plane. Отмена входящего
запроса закрывает protocol connection, но не стирает предварительно записанный
unknown. Автоматического SMTP reconciliation не существует: SMTP не предоставляет
поиск по idempotency key. Владелец сверяет серверные журналы/Message-ID вне bridge;
повтор того же key не отправляет письмо. Новое намерение требует отдельного
grant/effect после решения владельца, прежняя receipt не переписывается.

## Контракты для root

- #1046: `ResolveEmailAuthorization`, только typed Proto/gRPC через
  `controlplaneclient`, canonical transport → caster → service. Старый HTTP
  authorization endpoint удалён; потреблён typed контракт d31cd4c70.
  Producer подтверждает живые actor/agent/tenant/connection/grant, exact input
  digest, effect, config revision и credential generation; четыре scopes и gate
  берутся из authoritative state, не из запроса пользователя.
  Execution binding (invocation/test, lease ref/fence/generation,
  continuation и source revision/digest) проверяется по owner state отдельно
  от semantic command digest: смена attempt не создаёт новый почтовый effect.
  Owner decision для unknown должен отдельно связывать исходную receipt/digest,
  установленный outcome и новый owner grant; статусное чтение не является
  разрешением снять блокировку неизвестного IMAP source. Эта часть требует
  согласования с producer вместе с authority RPC до финального handoff.
  Новый workload `email-bridge` требует issuer operation profile, application
  grant и key-delivery/readback/restore registration у владельца authority.
- #1045/#1022: `Configuration`, `Mailbox`, `Endpoint`, `OperationPolicy`, `Limits`
  в том же OpenAPI. UI и YAML используют одну schema, raw secret values отсутствуют.
- #1028/root integration: bearer является opaque invocation token для online
  owner resolve. Статический token подключения не заменяет разрешение операции.
  Старые send/status paths сохранены; полный API — `ExecuteMailboxOperation`.
  `email.message.status.read` принимает ровно одно из `message_id|effect_key`:
  потеря HTTPS-ответа не требует повторять mutation для получения receipt.
  `CommandForIntegration` общий для consumer и producer: digest вычисляется
  после этого mapping, а не из primitive MCP JSON. `cc`, `bcc`, `attachments`
  передаются JSON-строками типизированных массивов; произвольные headers/URL
  не принимаются. Каталог содержит 19 пользовательских capabilities и две
  технические операции health/receipt. Канонические имена чтения:
  `email.message.read`, `email.attachment.read`; прежние fetch/download не
  объявляются в новом shipped catalog.
  `folder`, `destination_folder`, `uid_validity`, `source_uid_validity`,
  `thread_id`, `expected_digest` входят в semantic mapping. Source и destination
  выводятся из pinned конфигурации там, где caller не задаёт папку явно.
  Текущий общий package validator требует `HUMAN_EACH_EFFECT` для mutations:
  #1046 должен применить mailbox-policy при выдаче grant, включая all-allow,
  а не интерпретировать этот default как безусловный запрос Human Gate.
- #1029: сетевое решение ожидает владельца. GUIDE-DOC-003 запрещает mail mode
  существующего egress. Варианты владельца: отдельный controlled exact-destination
  маршрут bridge либо явное изменение утверждённой границы egress. Текущий
  CONNECT-клиент не доказывает готовность production mail route; прямого dial
  fallback нет. IMAP/SMTP/POP transport и локальные фейки от этого не зависят.

### Exact dial для #1029

TCP connection открывается только к `egress-gateway.kodex-system.svc:8080`.
Запрос без body: `CONNECT <fqdn>:<port> HTTP/1.1`, `Host: <fqdn>:<port>`.
Proxy до external dial проверяет exact policy/DNS/destination; bridge после
upgrade проверяет exact SNI/hostname и доверенную CA. Buffered greeting,
пришедший вместе с CONNECT 200, сохраняется для protocol reader.

| Destination | До TLS | После TLS |
| --- | --- | --- |
| SMTP :465 | Сразу ClientHello с exact SNI | EHLO, AUTH, MAIL/RCPT/DATA |
| SMTP :587 | 220 → EHLO → capabilities → STARTTLS → 220 | ClientHello; повторный EHLO, AUTH, MAIL/RCPT/DATA |
| IMAP :993 | Сразу ClientHello с exact SNI | greeting, LOGIN/SASL, SELECT/UID commands |
| IMAP :143 | greeting → tagged STARTTLS → tagged OK | ClientHello; новые capabilities, LOGIN/SASL, UID commands |
| POP :995 | Сразу ClientHello с exact SNI | greeting, USER/PASS, UIDL/LIST/RETR/DELE |
| POP :110 | +OK → STLS → +OK | ClientHello; USER/PASS, UIDL/LIST/RETR/DELE |

Эта таблица фиксирует требования protocol adapter, а не разрешение расширить
egress. Для нестандартного порта необходима явная exact protocol/TLS-mode запись
утверждённого маршрута. Secret/auth/message bytes до TLS запрещены.

## Протокольные ограничения

Проверяемый пример YAML: `contracts/email-bridge/v1/examples/mailboxes.yaml`.
Это безопасная fixture с descriptor names/generations, без secret values.
`tenant_id`, `connection_id`, `managed_by`, `source` и revision в проекции
назначает/проверяет producer; форма UI не становится источником полномочий.
UI и YAML используют `contracts/email-bridge/v1/configuration.schema.json`;
полный список mailbox policies обязателен. Пример отражает read-allow/send-gate,
а all-gate и all-allow проверяются `TestMailboxPolicyProfiles`.

IMAP schema: `receive_protocol=imap`, `imap`, `smtp`, optional `pop`,
`allowed_folders`, `folder`, `archive_folder`, `drafts_folder`, `reply_to`.
Endpoint содержит `auth_method=password|oauthbearer`, `username` и `secret`
descriptors, exact host/port/server_name/TLS/CA. POP допускает только password;
`Mailbox.credential_generation` назначается owner для connection binding;
поколения CA/username/secret descriptors могут ротироваться независимо друг от
друга и между протоколами. Новая проекция всё равно требует новой config revision.
SMTP password использует AUTH PLAIN, IMAP password использует LOGIN после TLS.
OAUTHBEARER не означает XOAUTH2 и не объявляет совместимость с ним.

Поиск IMAP серверный, ограничен UID-окном `scan_messages`; пагинация проходит
следующие окна без загрузки всей mailbox и без включения новых UID в текущий
cursor. `thread.read` ищет Message-ID/References/In-Reply-To внутри разрешённой
папки, не объединяет tenant/folders. BODY.PEEK не меняет read state. IMAP delete
требует UIDPLUS/IMAP4rev2; MOVE без native capability выполняется строго
COPY acknowledgement → STORE \\Deleted → UID EXPUNGE. Неоднозначный COPY/APPEND
не повторяется. Черновик заменяется новым UID, поэтому caller использует UID и
digest из результата, а не продолжает изменять прежний UID.

POP3 имеет один maildrop, отображаемый как INBOX; discovery не выдаёт вымышленных
папок. `mark` возвращает UNSUPPORTED, чтение не меняет server-side flags.
UIDL обязателен. Search выполняется локально по bounded headers; курсор закрепляет
UIDL snapshot и filter, изменение mailbox требует нового поиска. RETR может
передать всё письмо: limits проверяются и по LIST, и по фактическим байтам.
Курсор также связывает tenant/mailbox/connection/config revision. Невалидный
UIDL/LIST snapshot не используется для RETR или удаления сообщения.

Проверены Context7 `/emersion/go-smtp` (TLS, AUTH, DATA/final response и deadlines),
`/emersion/go-imap` и исходники
[go-imap v2.0.0-beta.8](https://github.com/emersion/go-imap/tree/v2.0.0-beta.8)
(UID search/fetch, UIDPLUS, APPEND/MOVE, STARTTLS и SASL). Старые примеры wiki
не используются как API v2 и не приравнивают OAUTHBEARER к XOAUTH2.
Проверены также
официальный [go-pop3](https://github.com/knadh/go-pop3),
[RFC1939](https://www.rfc-editor.org/rfc/rfc1939),
[RFC2595](https://www.rfc-editor.org/rfc/rfc2595),
[RFC5321](https://www.rfc-editor.org/rfc/rfc5321),
[RFC8314](https://www.rfc-editor.org/rfc/rfc8314).

## Исходная передача 2026-09-05

Пакет Beauvoir на основе `8571f194f` сохранён без сброса. Дополнительно исправлены
запись receipt после отмены запроса и немедленное закрытие ожидающего CONNECT.
Completion использует переданный composition root базовый context, отдельный
трёхсекундный deadline и синхронное завершение handler до shutdown PostgreSQL.
Ни mail transport, ни authority не получают этот независимый context.
Context7 `/golang/go` проверен для WithTimeout/AfterFunc/cancellation.

Локально на рабочем diff выполнены:

- `bash scripts/tests/email-bridge-test.sh '^Test(Postgres)?ReceiptCompletionAfterCancellation$'`:
  PASS после исправления fixture, ошибочно использовавшей разные configuration
  digests с одной revision. Watermark не ослаблен. Проверены SMTP accepted,
  IMAP partial UID/UIDVALIDITY/digest, отказ completion store, replay без
  provider effect и сохранение unknown source lock.
- Focused race `TestTunnelCancellationDuringCONNECT`, `TestCONNECTTransport`,
  `TestReceiptCompletionAfterCancellation`: PASS; только loopback fake servers.
- Contract codegen readback, staging render без apply и focused vet
  app/mail/mailtransport/component: PASS.
- `TestMutationRequiresCompletionLifecycle` под race: PASS; отсутствующий
  либо отменённый cleanup base отклоняет mutation до reserve/provider.
  Runner с `^TestNoSuchEmailFixture$` закрыто завершился кодом 2 до Docker,
  не объявляя пустую выборку успешной проверкой.

Полные неизменённые protocol suites повторно не запускались. Typed CP
authorization/reconciliation, issuer/key delivery и сквозной protected path
ещё не подключены; старый HTTP authority client не является работающим CP API.
Нельзя объявлять полную готовность #1037 по protocol fixture или render.
Live mail, cluster/remote/deploy и новые сетевые разрешения: NOT RUN.

## Typed CP consumer и UNKNOWN-before-effect

### Матрица фонового reconciliation (решение владельца после c07e66b20)

Canonical `kodex.email.receipt.v1` принят владельцем как opaque commitment;
CP не пересчитывает его из HTTP safe view. Пустой DecisionRef в
ResolveEmailReconciliation означает выбор текущего действующего server-owned
решения CP; NOT_FOUND означает отсутствие решения, не разрешение retry.

| Переход | Authority / effect | Атомарное состояние и failure | Read path |
| --- | --- | --- | --- |
| reserve → CP UNKNOWN → durable owner binding | Исходный invocation lease, generated CP Report с worker grant/proof | CP ref/version, invocation, connection и digest сохраняются до provider write; отказ закрывает effect | Local receipt + CP GET |
| Startup → polling | DB schema/configuration и local issuer barrier; lifecycle context | До barrier нет CP poll или local unlock; worker cancel/join до закрытия pool/client | Local readiness не объявляет полный CP path |
| Report response lost → exact replay | После исходного lease + completion budget; новый worker grant, прежние Binding и idempotency key | Reserve/Complete атомарно сохраняют закрытый журнал запроса; replay не вызывает provider и не создаёт owner ref локально | Local report journal + CP stored observation |
| UNKNOWN → bounded batch | Только durable UNKNOWN с восстановленным CP receipt ref; лимит и fair next-check scheduling | Отсутствие подтверждённого CP receipt оставляет effect закрытым; нет синтеза owner ref | Local receipt/journal |
| Poll → NOT_FOUND/denied/error | Каждый раз fresh typed Resolve по exact receipt ref/external ref/digest; DecisionRef пустой | Нет local state transition, source lock сохраняется, следующий bounded interval | CP authoritative GET |
| Poll → действующее решение | CP выбирает server decision; exact receipt/version/digest/invocation, actor/grant, outcome и TTL | Проверка freshness повторяется перед local commit; просрочка/несовпадение не снимает lock | CP decision + local audit |
| Решение → local audit + source unlock | Только EFFECT_CONFIRMED либо NO_EFFECT_CONFIRMED, без provider port | Одна owner DB transaction: immutable decision audit и unlock; исходный UNKNOWN/outcome/provider metadata не переписываются | Local journal + исходный receipt + CP GET |
| Duplicate/replica/restart | Повтор того же exact decision идемпотентен | Конфликт ref/version/digest/outcome закрыт; audit не дублируется | Durable journal |
| Crash/timeout/cancel | Сроки RPC/cycle/transaction ограничены; нет blind provider retry | До commit всё откатывается; после commit audit и unlock видимы вместе | Authoritative PostgreSQL |
| Shutdown | cancel → join → close CP/PG | Новые batch не стартуют, cleanup имеет отдельный bounded context | Readiness stopping |

Новый event не вводится: для каждого перехода авторитетен local PostgreSQL
journal и CP receipt/decision GET. Фоновый consumer не получает mail Provider,
не читает mail credentials и не создаёт новое намерение или grant.

Следующее состояние заменяет ограничения исходной передачи в части HTTP stub.
Потреблён exact checkpoint `d31cd4c7015e0513db7ca1afeacc12a7a6e155ac`:
Proto/generated/operation profile и policy revision 52. Предшествующие CP
contract additions сохранены; CP SQL/domain ownership не изменялось.
Main `8026633a9` с новым vendor catalog сохранён отдельным merge checkpoint.

- `X-Kodex-Email-Execution` несёт JSON ExecutionBinding с одним источником
  invocation либо connection test и exact исходным lease. Bearer равен fence,
  connection credential больше не используется как invocation authority.
  Binding не входит в semantic command digest.
- Рабочий CP client использует generated gRPC, mTLS, application worker grant
  и local issuer proof. Старый HTTP authorization endpoint удалён из OpenAPI
  и consumer. Connection test разрешает только HEALTH без фиктивного agent.
- Локальная durable reserve предшествует typed `ReportEmailEffectReceipt`
  UNKNOWN. Любой отказ CP/readback закрывает путь до provider credentials.
  После protocol effect сначала сохраняется локальный outcome, затем report.
  Отказ после reserve возвращает UNAVAILABLE/unknown, включая revoked grant:
  его нельзя превратить в безопасный для повтора HTTP 403.
- Повтор исходного command возвращает локально известный receipt, не вызывает
  provider и не подменяет исходный invocation. Допубликация принадлежит
  фоновому worker с устойчивым исходным запросом. Idempotency report привязана
  к immutable receipt digest и outcome; mail effect не получает automatic retry.
- Typed `ResolveEmailReconciliation` adapter проверяет exact receipt/version,
  external ref/digest, invocation, decision/version, actor/grant, freshness и
  закрытый outcome. Он не снимает local source lock сам по себе и не заменяет
  фоновый owner-decision consumer, описанный ниже.

### Канонический external receipt digest

`kodex.email.receipt.v1` реализован, закреплён golden test и принят владельцем.
Прямой `send_input` отсутствует; координация с Bohr идёт через root.
CP сохраняет и сверяет digest как exact
opaque commitment, а не пересчитывает его из public HTTP view.

SHA256 вычисляется по UTF-8 JSON `encoding/json.Marshal` следующего фиксированного
struct, в указанном порядке полей, без отступов и завершающего LF:
`schema`, `tenant`, `mailbox`, `id`, `effect_key`, `semantic_input_digest`,
`resource_digest`, `actor`, `agent`, `grant`, `operation`,
`configuration_revision`, `credential_generation`, `gate_approved`.
Все поля присутствуют, включая пустой resource_digest; schema всегда
`kodex.email.receipt.v1`. Целые revision/generation записываются десятичными
JSON numbers, gate_approved — boolean. Остальные поля — strings с обычным
escaping Go JSON encoder. Результат — bare lowercase hex64.

ID — random16bytes, lowercase hex32. Immutable audit берётся из сохранённой
receipt, не из нового authorization response. Outcome, UID/UIDVALIDITY, folder,
content digest и timestamps не входят в commitment: UNKNOWN → confirmed
не меняет identity. Тело, адреса и credentials в snapshot отсутствуют.
Golden fixture digest:
`6dfdb1521d14b99bec6fac759edeb2a11ce30120cbeb1489ab7baa0d5150e41e`.

### Проверки и блокеры

Локальный EMAIL `go test -race ./...`: PASS, включая 21-operation protocol
fixtures; PostgreSQL tests этого запуска пропущены без DSN. Отдельный
`email-bridge-test.sh '^TestPostgresDurableUnknownBeforeCPAndProvider$'`: PASS
с disposable PostgreSQL, migration up/status/up и race. API/schema race,
focused integration-gateway email race, vet EMAIL, codegen readback,
policy-codegen invariants и isolated staging render: PASS.
Docker runtime target собран локально; runtime и migration binaries собраны
Go 1.26.6 из Dockerfile. Это не registry publish или deployment.

Полный `email-bridge-test.sh` первоначально завершился FAIL: прежний
TestPostgresEffects повышал общий configuration watermark и нарушал изоляцию
следующих PG fixtures. Исправлена только оснастка: каждый PG test получает
отдельную БД, клонированную из мигрированного disposable template. Имена
генерируются случайно, доступ ограничен loopback fixture; runtime RLS role,
rollback policy и monotonic watermark не ослаблены. Повтор всего runner:
PASS, включая все protocol и PostgreSQL scenarios под race.

На checkpoint c07 проверенный CP WT `ac62c263c` не содержал реализаций трёх EMAIL RPC в app/transport.
Note `10132529a` явно оставляет SQL/RPC handlers незавершёнными.
CP WorkerGrantTrustFiles не содержит email-bridge. Registry workloads
`internal-rpc-authority/internal/platformworkergrant/app.go` также не включает
email-bridge. Собственный grant mount настроен, но наличие mount/Secret name
не доказывает выпуск ключа, trust registration, readback или restore.

### Фоновый consumer и production activation

Реализованы durable owner binding до provider, bounded polling (по умолчанию
16 записей каждые 15 секунд, пределы 1..64 и 5..300 секунд), startup barrier,
cancel/join и повторный exact Resolve с выбранным decision_ref перед commit.
Polling начинается после исходного lease + 3 секунды completion budget.
Одна PostgreSQL transaction фиксирует audit и source unlock; исходный UNKNOWN
и provider metadata неизменны. Просрочка во время ожидания row lock отменяет
commit. Поздний Complete после unlock запрещён. Подтверждения CP монотонны:
задержавшийся UNKNOWN не понижает уже сохранённую terminal version.

Новая forward migration `20260905000100_email_reconciliation.sql` вводит
FORCE RLS journal без DELETE для runtime. Worker не имеет provider port.
Локально PASS: полный disposable PG/protocol runner под race, EMAIL race/vet,
codegen и render обоих профилей с fixture release locks, Docker runtime build.
Проверены concurrent commit, stale acknowledgement, corruption, TTL expiry
при row lock, revoke на повторном Resolve, NOT_FOUND, bounded selection и
cancel/join на barrier/PG/RPC/commit. Реальный CP не подменяется этими фейками.

Production overlay `deploy/k8s/overlays/production/email-bridge` включён в
web-only и web-with-mattermost. Runtime/migration включены в image manifest и
закрытый release target registry. Mail egress не расширялся. Пустая mailbox
projection допускает инфраструктурную readiness, но HEALTH/send закрыто
отклоняются. Issuer использует `internal-rpc-authority-email-bridge-workload-tls`
и `internal-rpc-authority-email-bridge-issuer-postgresql`, путь PostgreSQL
`/var/run/secrets/kodex/internal-rpc-authority/postgres`, роль
`ira_email_bridge_issuer_g1`. Общая материализация приходит отдельным #1059.

На исходном checkpoint потеря CP Report response до сохранения owner receipt_ref
оставляла local UNKNOWN без owner binding. Восстановление этого случая описано
ниже; lookup с пустым receipt_ref не вводится. Доставка собственных database
Secrets реализована в разделе установки БД. Полный producer path остаётся
отдельной интеграционной зависимостью #1046.

Общий go-toolchain contract: FAIL вне EMAIL, runtime-controller Dockerfile
не материализует local replacement libs/go/secretbrokerapi; root уведомлён.
Shared CP/security ownership без согласования не изменяется.

Full protected issuer→CP SQL→EMAIL reconciliation: NOT RUN и не READY.
Полный #1037 остаётся незавершённым. Mail route/ports, live mail, cluster,
staging, deploy, push и PR: NOT RUN; запреты владельца сохранены.

## Checkpoint b94400fa6 и зависимость #1059

Consumer/migration/production activation зафиксированы в `b94400fa6`.
Exact root `0765f3dadb901664da3b83e3701a4a739f209e54` перенесён без конфликтов;
совместный checkpoint `80a0ae927`. EMAIL includes обоих профилей и третья
PostgreSQL StatefulSet сохранены вместе с registry revision 7 и exact RBAC.

На `80a0ae927` локально PASS:

- `make check-email-bridge-codegen test-email-bridge-render test-worker-authority-projections`:
  codegen, оба полных профиля, fixture release locks, exact issuer/key/ingress.
- `make test-install-contract test-internal-rpc-authority-abi-render`:
  install contract, IPv6 ingress и ABI sidecars.
- EMAIL `go test -race ./internal/domain/service/reconciliation ./internal/clients/authority -count=1 -timeout=90s`.
- Docker target migration, включая сборку runtime и migration binaries.

`make test-web-only-release`: FAIL. Существующий assertion требует
`project_required=true` у `platform.stt.credential.project`, но сохранённая
policy52 правильно содержит ранее согласованное organization-only `false`.
STT/shared policy не изменялись; расхождение передано root, FAIL не скрыт.

Read-only проверен новый CP handoff `10266a2ef`: owner-selected reconciliation,
worker trust и durable watermark реализованы у Bohr. Но исходники CP ещё не
содержат handlers `ResolveEmailAuthorization` и `ReportEmailEffectReceipt`;
handoff явно отмечает незавершённые mailbox projection и source authorization.
CP checkpoint пока не переносился выборочно без полного producer handoff.
Нужен согласованный полный checkpoint этих handlers/projection и deployment env
`CONTROL_PLANE_EMAIL_GRANT_TRUST_FILE` с public key path из #1059. Этот env
и CP domain остаются ownership Bohr/root, EMAIL не включает их обходным путём.

## Установка собственной БД

Installer создаёт три независимых credentials почтовой БД и проекции
`email-bridge-postgresql-bootstrap`, `email-bridge-runtime-database`,
`email-bridge-migration-database`. Они не входят в credentials основной
PostgreSQL и не получают права authority. DSN закрепляют database/role/hostname
и `verify-full` с installation CA. Отдельный Certificate материализует ранее
отсутствовавший `internal-rpc-authority-email-bridge-workload-tls` с точным SPIFFE.

Порядок установки: Secret projections → Certificate Ready → PostgreSQL TCP
readiness после bootstrap → migration Complete → workloads → Job readback.
TCP probe исключает временный Unix-only сервер инициализации. Повторная установка
использует прежний набор material; генерация поверх существующего каталога
запрещена. Замена паролей существующей БД этим bootstrap не реализуется.

Публичная проверка `make test-email-bridge-install` ограничена 420 секундами.
Она создаёт полный installation material и disposable PostgreSQL из точного
image manifest в изолированной Docker network, применяет исходный bootstrap
и собранный migration binary. Локально PASS: TLS с `0440` root-owned ключом,
`up/status/up`, runtime SELECT, отказ в DDL/SET ROLE/чтении migration history/
DELETE и отказ при plaintext, неверных hostname/password/CA. Временные
credentials удаляются; значения не печатаются. Production/стенд не затрагиваются.

Локально PASS также `test-install-contract` и `test-email-bridge-render` для
обоих полных профилей. Исправлен устаревший STT assertion без изменения policy.
`test-web-only-release` пока FAIL: отсутствует producer
`email-bridge-mailbox-projection`; его доставка относится к #1046. Без неё
полный EMAIL runtime не объявляется готовым. Сквозной issuer → CP → EMAIL и
реальная почта остаются NOT RUN до полного producer checkpoint и staging #1031.

Через Context7 проверена документация PostgreSQL 18:
[TLS](https://www.postgresql.org/docs/18/ssl-tcp.html),
[проверка hostname](https://www.postgresql.org/docs/18/libpq-ssl.html),
[service file](https://www.postgresql.org/docs/18/libpq-pgservice.html).

## Восстановление потерянного Report

Forward migration `20260905000200_email_report_journal.sql` добавляет к той же
FORCE RLS receipt строке закрытый `report_source`, его digest, монотонную
версию и bounded retry schedule. Reserve и Complete сохраняют receipt и
журнал в одном SQL statement: разрыва между effect state и recovery state нет.
Исходный fence хранится только в частном журнале PostgreSQL, не в audit,
публичном API или логах. После terminal ACK он удаляется; для подтверждённой
начальной записи его удаляет bounded cleanup после lease + 3 секунды.

Отдельный worker с теми же ограничениями batch/interval и startup/cancel/join
контрактом восстанавливает исходные Binding, external ref/digest, outcome и
idempotency key. Повтор начинается после исходного lease + completion budget.
Adapter снимает только локальный запрет истёкшего lease для этого replay;
generated client по-прежнему получает свежие transport/worker полномочия.
CP возвращает ранее сохранённое наблюдение либо принимает поздний UNKNOWN
по прежде выданной immutable authorization с теми же organization/source/
lease/fence/generation/semantic digest. Поздний terminal требует прежний UNKNOWN
и не может противоречить решению владельца. Этот путь не продлевает grant,
не открывает invocation повторно и никогда не обращается к mail Provider.

Точное подтверждение сначала сохраняет CP ref/version, затем закрывает
pending report. Сбой между этими шагами приводит к идемпотентному повтору.
CAS по tenant/mailbox/receipt/source digest/report version не позволяет
старому ACK скрыть новый terminal report. Отдельные циклы восстановления
Report и исполнения owner decision имеют независимые bounded бюджеты.

Локально проверены на disposable PostgreSQL под race: потеря начального и
terminal ответа с восстановлением новым repository adapter, отсутствие
повторного SMTP при новом HTTP invocation, конкурентный claim, stale ACK,
atomic validation, удаление fence и отказ при повреждении source. Unit-тесты
проверяют original expired Binding в gRPC mapping, deny/error readback,
частичный прогресс и cancel/join на barrier/PG/RPC/remember/ACK.

В интеграции `25b10c8bf` потреблён CP `bc146c2c7` с поздней записью наблюдения:
если первый Report не дошёл до CP до expiry, worker сохраняет UNKNOWN по
прежней authorization. CP атомарно закрывает истёкший RUNNING источник в
UNKNOWN_OUTCOME; owner reconciliation доступна также для FAILED/CANCELLED,
но новый claim и provider retry запрещены. Terminal без начального UNKNOWN
отклоняется. Источник SUCCEEDED не получает новое UNKNOWN наблюдение.

На этом SHA локально PASS: EMAIL race/vet/build, integrationpackage race,
EMAIL codegen и render обоих профилей. Настоящий CP PostgreSQL contour
`email_configuration|email_receipt` проверил выдачу authorization до expiry,
первый Report после expiry, поздний terminal, owner decision/Resolve,
FAILED/CANCELLED, подмену fence и отсутствие повторного claim. Это отдельные
owner/consumer контуры, не доказательство полной защищённой цепочки.
Полный issuer → CP SQL → EMAIL, доставка mailbox projection и staging: NOT RUN.

## Идентичность POP readback

Ответы POP fetch, download и attachment list сохраняют точный UID из
проверенного UIDL snapshot и разрешённую нормализованную папку команды.
UIDVALIDITY остаётся нулевым: POP не предоставляет IMAP поколения папок.
Consumer может требовать exact UID без исключения для пустого поля.
`TestPOPReadIdentityHTTPS` проверяет generated HTTP client → mTLS handler →
mail service → настоящий локальный POP fixture для implicit TLS и STARTTLS,
двух UID, явной/default INBOX и всех трёх операций. Listing вложений не
возвращает их содержимое. Это локальная проверка, не live mail или staging.
