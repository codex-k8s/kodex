---
id: SERVICE-EMAIL-1037
title: Email bridge
type: service
status: approved
owner: developer
version: 1.2.0
updated: 2026-09-05
---

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
в read-only AtomicWriter mount `<root>/<name>.<generation>` через securefile.
Один pinned `..data` содержит `mailboxes.json` и все credential bytes snapshot;
смена symlink не смешивает поколения. Отзыв grant проверяет owner.

Конфигурация загружается при старте и каждые 15 секунд одним bounded monitor.
В режиме `EMAIL_BRIDGE_CONFIGURATION_MODE=managed` Deployment закрепляет
`EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION` и
`EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST`; несовпадение отклоняется до записи
watermark. После durable приёма и построения сервиса bridge подтверждает точные
revision/digest через защищённый `ReportEmailConfigurationReadback` у CP.
До ACK новый snapshot не обслуживает запросы. Отказ callback закрывает readiness;
следующий цикл повторяет только идемпотентное подтверждение, без mail effect.
CP должен принимать точный PENDING snapshot до READY; его отдельный readback
Deployment доказывает готовность всех реплик и исключает цикл ожидания.

Новая managed revision меняет Deployment pins вместе с egress policy digest и
требует rollout. Старый in-flight запрос сохраняет прежний immutable snapshot;
новые запросы отклоняются при несовпадении pins либо rollback. PostgreSQL
watermark переживает restart. Режим `bootstrap` допускает только пустой shipped
seed: revision 1, managed_by=git, source=release-bootstrap. Он не отправляет ACK
и не доказывает приём owner-публикации. Неподключённая mailbox отклоняется до
credentials/provider; local readiness не означает доступный mail route.

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
CP receipt binding сохраняется до provider write. Фоновый consumer выбирает
связанные durable UNKNOWN после окончания исходного lease и completion budget,
получает текущее owner decision и повторно авторизует exact decision перед commit.
Audit и source unlock фиксируются атомарно, исходный UNKNOWN не переписывается.
Consumer не имеет provider port и не отправляет почту повторно.
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
проверяет PostgreSQL/configuration, Deployment pins, local issuer и managed
owner ACK; provider readiness отдельно проверяется авторизованным HEALTH.
Полная проверка выполняется HEALTH с настоящим owner connection-test claim.
Owner reconciliation consumer проверен с fake CP и disposable PostgreSQL;
реальные CP producer/#1059 key delivery требуют сквозной проверки до
финального staging SHA. Mail listener8082 реализован в #1029; пустая bootstrap
проекция по-прежнему не разрешает live mail. Зависимости не обходятся
локальным allow-all или прямым dial. Список доказательств и ограничений:
`docs/operations/email-bridge-1037.md`; действия владельца:
`docs/runbooks/email-bridge.md`.

Каждый CONNECT проверяет семь exact readback headers до TLS/provider bytes.
`EMAIL_BRIDGE_EGRESS_POLICY_DIGEST` обязателен и совпадает с
`EGRESS_GATEWAY_MAIL_POLICY_DIGEST` из утверждённого render #1029. Source
revision/digest вычисляются из текущего immutable reload snapshot, а не из
caller headers. При смене конфигурации/DNS pins owner обновляет оба deployment
ожидания; несовпадение во время rollout закрыто отклоняется без direct/8080
fallback. STARTTLS greeting остаётся opaque, credentials передаются только после TLS.

Production overlay включён в `web-only` и `web-with-mattermost`. Release manifest
содержит `email-bridge:runtime` и `email-bridge-migration:migration` как разные
immutable images. Worker defaults: batch 16, interval 15 секунд, cycle budget
5 секунд; допустимы batch 1..64 и interval 5..300 секунд через
`EMAIL_BRIDGE_RECONCILIATION_BATCH`/`EMAIL_BRIDGE_RECONCILIATION_INTERVAL_SECONDS`.
