# Email bridge #1037

Самостоятельный HTTPS deployable для SMTP и IMAP; POP3/POP3S только compatibility.
Он не заменяет
control-plane policy owner и не добавляет почтовый transport в generic executor.

## API

Источник: `contracts/openapi/email-bridge/v1/openapi.yaml`, generated клиент и
модели: `libs/go/emailbridgeapi`. Полный typed command отправляется на
`POST /v1/mailbox-operations`; сохранены send/status endpoints #1028.

MCP capabilities: mailbox.list, message.list/search/read, thread.read,
attachment.list/read, message.mark_read/mark_unread/move/archive/delete,
draft.create/update/delete, message.send/reply/reply_all/forward, health и receipt.
POP не поддерживает threads, folders, flags и drafts и явно возвращает UNSUPPORTED.
Reply/reply_all требуют source_uid и явного списка адресатов; bridge сверяет его
с Reply-To/From/To/Cc исходного сообщения. Forward добавляет исходное письмо как
message/rfc822. Bcc не записывается в MIME headers. Accepted означает принятие
SMTP-сервером, не доставку конечному адресату.

Обе стороны используют TLS. Bridge требует exact mTLS SPIFFE integration-gateway
и bearer, равный exact lease fence. Заголовок `X-Kodex-Email-Execution` содержит
typed JSON `invocation_ref|connection_test_ref` и исходный `WorkLease`.
Online `ResolveEmailAuthorization` вызывается по generated gRPC #1046 через
mTLS, application worker grant и local authority proof. Он проверяет актуальные
authority/grant/gate и возвращает четыре scopes; connection test допускает
только HEALTH без выдуманного agent scope.
Bridge сверяет input digest, tenant, mailbox, operation, effect, config revision,
credential generation и ограничивает I/O сроком grant до чтения descriptors.

## Конфигурация

JSON Schema и Go-модели генерируются из одного OpenAPI. `emailbridgeapi.Decode`
проверяет JSON/YAML, required/unknown/duplicate fields, запрещает aliases;
`ValidateConfiguration` проверяет связанные limits, exact host/SNI, адресатов,
поколения credentials и закрытую политику каждой операции.

Mailbox содержит SMTP/IMAP и optional POP endpoints: host, port, tls_mode=implicit|starttls,
server_name, CA/username/secret descriptors, auth_method=password|oauthbearer,
timeout и limits. Поля allowed_folders, folder, drafts_folder, archive_folder,
reply_to, sender,
envelope_from, hello_name и recipients задают точный envelope scope. Значений
секретов в schema нет. Каждый descriptor `{name,generation}` разрешается только
в read-only mount `<root>/<name>/<generation>` через securefile; повторное чтение
не кэшируется. Отзыв grant проверяет owner, удаление projected credential
отклоняется до protocol authentication.

Конфигурация загружается при старте. Новая revision требует rollout; PostgreSQL
watermark запрещает обслуживать прежнюю revision после запуска новой. Пустая
bootstrap configuration намеренно не READY, пока владелец не подключил mailbox.

## Состояние

Отдельная БД email_bridge, отдельные runtime/migrator principals. Receipt хранит
tenant/mailbox, ключ, digest входа, ID и outcome, provider UID/UIDVALIDITY/folder
и content digest без писем и секретов. Атомарная
reserve записывает unknown, затем `ReportEmailEffectReceipt` должен подтвердить
exact CP UNKNOWN до чтения provider credentials и внешней mutation.
После локальной completion публикуется известный outcome. Отказ публикации
возвращает unknown; повтор вызова может допубликовать receipt, но не повторяет
mail effect. Конкурентный запрос получает
ту же запись; смена входа при том же key возвращает CONFLICT. Сбой процесса,
таймаут final SMTP response либо POP QUIT не запускает автоматический повтор.
Успешная SMTP final response переводит receipt в accepted; успешный DELE/QUIT
в deleted. Event bus, очередь доставки и фоновая повторная отправка отсутствуют;
авторитетный путь — receipt read. Ручное превращение unknown в ready запрещено.
IMAP unknown блокирует другое изменение того же source даже с новым effect key.
Draft update возвращает новый UID; APPEND и удаление старого UID не являются
атомарной операцией IMAP. Частичный результат остаётся unknown без повтора.
BODY.PEEK не выставляет read flag; UID EXPUNGE не удаляет чужие помеченные письма.

## Проверка

- `make test-email-bridge-unit`: API/schema, bridge и email-consumer под race;
  бюджеты test runner 60/90/60 секунд, без контейнеров PostgreSQL.
- `make test-email-bridge`: fake protocols и durable receipts под race с
  disposable PostgreSQL; migration up/status/up, бюджет component suite 90 секунд.
- Для узкой проверки с той же disposable БД runner принимает Go test regexp:
  `bash scripts/tests/email-bridge-test.sh '^TestPostgresReceiptCompletionAfterCancellation$'`.
- `make check-email-bridge-codegen`: воспроизводимый OpenAPI/JSON Schema codegen.
- `make test-email-bridge-render`: isolated staging render с fixture digests.

Реальные mailbox credentials и staging E2E #1031 проверяются root отдельно.
Typed gRPC consumer использует checkpoint #1046 `d31cd4c70`; наличие generated
RPC не доказывает CP SQL, worker trust или key delivery. Local `/readyz`
проверяет PostgreSQL/configuration и local issuer, не полный protected path.
Полная проверка выполняется HEALTH с настоящим owner connection-test claim.
Материализация owner reconciliation и утверждённый mail route требуют стыковки до
финального SHA: существующий egress не разрешает mail mode. Зависимости не обходятся
локальным allow-all или прямым dial. Список доказательств и ограничений:
`docs/operations/email-bridge-1037.md`; действия владельца:
`docs/runbooks/email-bridge.md`.
