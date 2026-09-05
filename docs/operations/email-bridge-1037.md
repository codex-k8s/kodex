---
id: OPS-EMAIL-1037
title: Email bridge и границы интеграции
type: operations
status: approved
owner: developer
version: 1.1.0
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
  `controlplaneclient`, canonical transport → caster → service. Ранее указанный
  HTTP `/internal/v1/email-authorizations/resolve` не является доступным API
  control-plane и должен быть удалён из OpenAPI/client при стыковке.
  Семантические модели `AuthorizationRequest/AuthorizationDecision` пока
  находятся в `libs/go/emailbridgeapi`; точный Proto service/method и номера
  полей согласуются с владельцем producer до финальной сдачи.
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

## Передача 2026-09-05

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
