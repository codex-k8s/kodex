# Email bridge #1037

Самостоятельный HTTPS deployable для POP3/POP3S и SMTP. Он не заменяет
control-plane policy owner и не добавляет почтовый transport в generic executor.

## API

Источник: `contracts/openapi/email-bridge/v1/openapi.yaml`, generated клиент и
модели: `libs/go/emailbridgeapi`. Полный typed command отправляется на
`POST /v1/mailbox-operations`; сохранены send/status endpoints #1028.

Операции: health, mailboxes, list, search, fetch, download, send, reply,
reply_all, forward, delete, receipt. POP mark явно возвращает UNSUPPORTED.
Reply/reply_all требуют source_uid и явного списка адресатов; bridge сверяет его
с Reply-To/From/To/Cc исходного сообщения. Forward добавляет исходное письмо как
message/rfc822. Bcc не записывается в MIME headers. Accepted означает принятие
SMTP-сервером, не доставку конечному адресату.

Обе стороны используют TLS. Bridge требует exact mTLS SPIFFE integration-gateway
и bearer invocation token. Online `ResolveEmailAuthorization` принадлежит #1046;
он проверяет актуальные authority/grant/gate и возвращает четыре scopes.
Bridge сверяет input digest, tenant, mailbox, operation, effect, config revision,
credential generation и ограничивает I/O сроком grant до чтения descriptors.

## Конфигурация

JSON Schema и Go-модели генерируются из одного OpenAPI. `emailbridgeapi.Decode`
проверяет JSON/YAML, required/unknown/duplicate fields, запрещает aliases;
`ValidateConfiguration` проверяет связанные limits, exact host/SNI, адресатов,
поколения credentials и закрытую политику каждой операции.

Mailbox содержит POP/SMTP endpoint: host, port, tls_mode=implicit|starttls,
server_name, CA/username/password descriptors, timeout и limits. Поля sender,
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
tenant/mailbox, ключ, digest входа, ID и outcome без писем и секретов. Атомарная
reserve записывает unknown до внешней mutation. Конкурентный запрос получает
ту же запись; смена входа при том же key возвращает CONFLICT. Сбой процесса,
таймаут final SMTP response либо POP QUIT не запускает автоматический повтор.
Успешная SMTP final response переводит receipt в accepted; успешный DELE/QUIT
в deleted. Event bus, очередь доставки и фоновая повторная отправка отсутствуют;
авторитетный путь — receipt read. Ручное превращение unknown в ready запрещено.

## Проверка

- `make test-email-bridge`: Go/race, fake protocols, disposable PostgreSQL,
  schema/codegen и staging render.
- `make check-email-bridge-contract`: воспроизводимый OpenAPI/JSON Schema codegen.
- `make test-email-bridge-render`: isolated staging render с fixture digests.

Реальные mailbox credentials, staging E2E #1031, интеграция producer #1046 и
mail-aware egress #1029 проверяются root отдельно. Эти зависимости не обходятся
локальным allow-all или прямым dial. Список доказательств и ограничений:
`docs/operations/email-bridge-1037.md`; действия владельца:
`docs/runbooks/email-bridge.md`.
