---
id: RUN-EMAIL-1037
title: Эксплуатация email-bridge
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-04
---

# Email bridge

## Подготовка владельцем

После review и отдельного допуска применяются кодовые ресурсы
`deploy/k8s/overlays/staging/email-bridge`. Release renderer подставляет разные
неизменяемые digests runtime и migration images. Bootstrap PostgreSQL выполняется
до migration 20260904000700; runtime schema migrations сам не запускает.

Необходимые Secrets выпускаются владельцем secret delivery, не runtime SA:

| Secret | Keys / назначение |
| --- | --- |
| email-bridge-postgresql-bootstrap | admin-password, runtime-password, migration-password; только database bootstrap |
| email-bridge-runtime-database | dsn; email_bridge_runtime, verify-full, exact PostgreSQL hostname, sslrootcert=/var/run/email/tls/ca.crt |
| email-bridge-migration-database | dsn; отдельный email_bridge_migrator, verify-full и тот же CA path |
| email-bridge-authority | service-bearer для online owner API; health-token с разрешением только health для workload email-bridge |
| email-bridge-mailbox-projection | immutable CA/username/password generations, items отображаются на name/generation из Configuration |
| email-bridge-tls | cert-manager workload certificate/key/CA, mTLS SPIFFE и exact DNS |
| email-bridge-postgresql-tls | cert-manager server certificate/key/CA |

Runtime и migration SA не имеют RoleBinding и Kubernetes API token. Runtime
не читает bootstrap/migration credentials, migration не читает mailbox passwords.
NetworkPolicy разрешает только exact egress-gateway, control-plane authority,
свою PostgreSQL, telemetry и DNS. PostgreSQL хранит только receipts и revision
watermark, письма не сохраняются. PVC включается в backup-профиль владельцем
#1042; потеря receipt store запрещает повтор старого effect key.

## Проверка готовности

Local /readyz отражает bounded online authority, PostgreSQL role/schema и
SMTP AUTH/NOOP плюс POP AUTH/UIDL. Health использует отдельный owner-issued token,
тот же authorization API и provider transport. Пустая конфигурация, отсутствующий
credential, TLS mismatch или недоступный owner/egress дают NOT READY.
Отказ одной mailbox означает NOT READY bridge, остальные вызовы всё равно
проверяются по собственной mailbox policy.

Владелец проверяет три policy: read allow/send gate, все gate, все allow.
Pending/rejected gate не читает почтовые credentials. Подтверждение относится
к exact operation/input/effect; payload клиента не подтверждает gate.
Затем проверяет list/search, MIME attachment fetch, send attachment, reply-all,
повтор того же key и отказы чужой mailbox/revoked credential/TLS mismatch.

## UNKNOWN_OUTCOME

1. Остановить исходное намерение у control-plane, не повторять SMTP DATA/POP QUIT.
2. Прочитать receipt через authenticated API точной mailbox. Повтор key безопасен
   только как чтение той же durable записи, не как инструкция новой отправки.
3. Владелец сверяет SMTP logs по Message-ID либо POP UIDL. Bridge не объявляет
   delivered по одному accepted и не делает вывод «не отправлено» из отсутствия
   записи у провайдера.
4. Новое намерение возможно только после явного решения владельца с новым grant
   и effect. Не менять receipt через SQL; старый unknown остаётся историей.

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
