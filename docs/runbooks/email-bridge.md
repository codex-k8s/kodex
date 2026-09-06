---
id: RUN-EMAIL-1037
title: Эксплуатация email-bridge
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-09-05
---

# Email bridge

## Отказ migration CLI

`Email bridge migration failed` содержит только `stage` и закрытый
`error_class`. `configuration`/`arguments` — вход CLI; `dsn_read`/`secure_file` —
чтение bounded Secret; `database_open` — создание handle; `database_connect` —
настройка pgx, TLS и Ping до goose; `dialect`, `migration`, `status` — этап goose.
Классы различают timeout/canceled, network, filesystem, connection_configuration,
tls_verification и database (отдельно authentication/permission/missing/unavailable).
`unknown` не доказывает конкретную причину; raw error, DSN и SQL не публикуются.

При повторном up сначала сверить UID/GID/mode/size и symlink boundary DSN/CA,
Secret metadata и reuse material. Затем по классу проверять TLS CA/SNI/expiry,
service endpoints или PostgreSQL. Не снимать read-only/TLS и не ротировать
пароль по одному общему failure. CLI сохраняет минутный бюджет и forward-only
goose; предварительный Ping не применяет миграций. Причина staging #1114 пока
не подтверждена: live acceptance требует нового разрешённого code-first up.

## Подготовка владельцем

После review и отдельного допуска применяются кодовые ресурсы
`deploy/k8s/overlays/staging/email-bridge`. Release renderer подставляет разные
неизменяемые digests runtime и migration images. Bootstrap PostgreSQL выполняется
до migrations 20260904000700 и 20260905000100; runtime schema migrations сам не запускает.

Необходимые Secrets выпускаются владельцем secret delivery, не runtime SA:

| Secret | Keys / назначение |
| --- | --- |
| email-bridge-postgresql-bootstrap | admin-password, runtime-password, migration-password; только database bootstrap |
| email-bridge-runtime-database | dsn; email_bridge_runtime, verify-full, exact PostgreSQL hostname, sslrootcert=/var/run/email/tls/ca.crt |
| email-bridge-migration-database | dsn; отдельный email_bridge_migrator, verify-full и тот же CA path |
| Application grant projection | worker grant в `/var/run/secrets/kodex/email-bridge/application-grant/application-grant.jws`; выдача owner #1046 и exact trust должны быть материализованы до запуска |
| email-bridge-mailbox-projection | immutable CA/username/secret generations, items отображаются на name/generation из Configuration |
| email-bridge-tls | cert-manager workload certificate/key/CA, mTLS SPIFFE и exact DNS |
| email-bridge-postgresql-tls | cert-manager server certificate/key/CA |

Runtime и migration SA не имеют RoleBinding и Kubernetes API token. Runtime
не читает bootstrap/migration credentials, migration не читает mailbox passwords.
NetworkPolicy разрешает только exact egress-gateway, control-plane authority,
свою PostgreSQL, telemetry и DNS. PostgreSQL хранит только receipts и revision
watermark, письма не сохраняются. PVC включается в backup-профиль владельцем
#1042; потеря receipt store запрещает повтор старого effect key.

## Проверка готовности

Local /readyz отражает только PostgreSQL role/schema, configuration watermark,
локальный issuer. Пустая bootstrap mailbox configuration не требует SMTP.
Это не доказательство CP SQL,
worker trust, mail credentials или доступности внешнего транспорта.
Protected HEALTH требует исходный owner connection-test claim с lease fence,
online typed CP authorization и SMTP AUTH/NOOP, IMAP authentication/SELECT;
POP AUTH/UIDL проверяется отдельно при наличии compatibility endpoint.
Typed health возвращает
`protocol_readiness.smtp/imap/pop3`: ready, not_ready или not_configured.
Отказ optional POP не выключает исправный основной SMTP+IMAP профиль.
Самовыданный health-token отсутствует. Неподключённая mailbox, недоступный owner,
credential или egress закрывают protected HEALTH.
Остальные вызовы всегда проверяются по собственной mailbox policy.

Владелец проверяет три policy: read allow/send gate, все gate, все allow.
Pending/rejected gate не читает почтовые credentials. Подтверждение относится
к exact operation/input/effect; payload клиента не подтверждает gate.
Затем проверяет list/search, MIME attachment fetch, send attachment, reply-all,
повтор того же key и отказы чужой mailbox/revoked credential/TLS mismatch.
Для IMAP дополнительно проверяются thread, attachment.list, mark_read/unread,
move/archive/delete и draft.create/update/delete. UID всегда связывается с
UIDVALIDITY и папкой; старый UID после пересоздания папки не используется.
Draft update возвращает новый UID и content digest. Thread pagination не
захватывает новые UID, появившиеся после начала просмотра.

## UNKNOWN_OUTCOME

1. Остановить исходное намерение у control-plane, не повторять SMTP DATA,
   IMAP APPEND/MOVE/EXPUNGE или POP QUIT.
2. Прочитать receipt через authenticated API точной mailbox. Повтор key безопасен
   только как чтение той же durable записи, не как инструкция новой отправки.
3. Владелец сверяет SMTP logs по Message-ID, IMAP Message-ID/UIDVALIDITY/folders
   либо POP UIDL. При draft replacement проверяются и новый APPEND, и отсутствие
   старого UID: это не атомарный replace. Bridge не объявляет
   delivered по одному accepted и не делает вывод «не отправлено» из отсутствия
   записи у провайдера.
4. Новое намерение возможно только после явного решения владельца с новым grant
   и effect. Не менять receipt через SQL; старый unknown остаётся историей.

Неизвестное IMAP-изменение блокирует другие keys для того же source. Consumer
выбирает bounded batch из `email_bridge.owner_receipts` после окончания исходного
lease плюс 3 секунды completion. Пустой DecisionRef запрашивает текущее server-owned
решение CP, затем exact DecisionRef повторно авторизуется перед local commit.
NOT_FOUND сохраняет блокировку; expired/revoked/mismatch закрыто отклоняются.
Одна транзакция сохраняет decision actor/grant/version/outcome и снимает source
lock, не меняя UNKNOWN receipt и provider metadata. Повтор решения идемпотентен;
поздний protocol completion после reconciliation запрещён.
Статусный GET и новый effect key сами по себе не снимают блокировку. Отсутствующий authority RPC
или неутверждённый сетевой маршрут запрещают объявлять unit готовым к deploy.

## Ротация и остановка

Новые credentials/CA сначала публикуются как новое generation/revision, затем
меняется owner configuration и выполняется rollout. Старые grants отзываются;
online resolver перестаёт подтверждать их до projection. Удалённые файлы не
кэшируются. TLS server certificate/CA обновляются rollout; overlap CA задаёт
cert-manager/owner. PostgreSQL configuration watermark не допускает откат
прежней конфигурации: rollback кода использует текущую schema и новую revision.

Shutdown отменяет protocol contexts и ждёт HTTP/worker join до закрытия pool.
Если deadline прервал final response, запись остаётся unknown. Tracing flush
получает независимый bounded context. Логи содержат только route/status и
фиксированные сообщения; адреса, headers, body, attachments и secrets запрещены.

После остановки protocol I/O handler имеет отдельный бюджет 3 секунды только
для completion уже зарезервированной receipt и bounded CP report. Подтверждённый final response
сохраняется даже при отмене HTTP; известные UID частичного IMAP сохраняются
вместе с unknown. Ошибка completion не разрешает повтор, а оставляет исходную
unknown receipt. Этот cleanup не продлевает grant и не выполняет mail I/O.
