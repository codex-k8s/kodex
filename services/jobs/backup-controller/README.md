---
id: JOB-MC-003
title: Backup controller
type: service
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-28
---

# Backup controller

`backup-controller` — один deployable unit для периодического logical backup
PostgreSQL и инвентаризации platform-owned S3 objects. Он пишет данные только в
versioned backup bucket, использует условную immutable-запись и сохраняет в
manifest exact `bucket`, `key`, `versionId`, `ETag`, размер и SHA-256 каждого
объекта.

## Жизненный цикл

1. Controller захватывает один глобальный immutable S3 operation lock.
2. Для каждой PostgreSQL database открывается `REPEATABLE READ READ ONLY`
   transaction, экспортируется snapshot и из него создаются custom dump и
   schema-only dump.
3. Для каждого platform-owned S3 object фиксируется текущий exact version,
   metadata SHA-256 проверяется полным чтением, после чего содержимое копируется
   в backup repository.
4. `manifest.json` становится видимым только после всех payload. Успех backup
   фиксирует отдельный `verification.json` после полного независимого readback.
5. Retention учитывает только manifest с корректным exact verification receipt
   и никогда не удаляет последний backup с проверенным restore drill.

Незавершённые prefixes и failure receipts не считаются restore points. Crash с
неосвобождённым operation lock закрыто блокирует новые мутации до owner recovery.

## Restore drill

Команда `restore-drill` принимает repository credential, owner approval и
отдельный набор target credentials только из Kubernetes Secrets. Approval
связывает `approvalId`, `restoreId`, exact `backupId`, fingerprint target set и
срок не более 24 часов. Controller отказывается работать с существующей target
database или непустым S3 target prefix, не удаляет и не перезаписывает target.

Успешный drill фиксируется immutable receipt только после schema-version
readback всех database и полного checksum readback восстановленных объектов.
Повтор той же команды допустим только при точном совпадении approval и receipt.

## Матрица lifecycle

| Инициатор | Исходное состояние | Проверка | Успех | Закрытый отказ |
| --- | --- | --- | --- | --- |
| Periodic loop или команда `backup` | Нет operation lock | S3 versioning, PostgreSQL readiness, exact source inventory | `attempt -> payloads -> manifest -> verification` | Immutable `failure.json`; manifest без verification не виден retention |
| Команда `verify` | Есть immutable manifest | Полный checksum/version/ETag readback | Один semantic verification receipt | Повтор с другим manifest receipt конфликтует |
| Retention | Есть verified catalog | Minimum age, keep count, последний успешный drill | Exact version deletion с пустым-prefix readback | Без restore point outcome `protected`, удалений нет |
| Owner-created `restore-drill` | Есть свежий approval Secret | Exact backup, target fingerprint, отсутствующие DB, пустой S3 prefix | Intent, isolated restore, readback, drill receipt | Terminal failure; тот же restore ID не повторяется |
| Любая мутация | Lock отсутствует | Conditional put глобального S3 lock | Exact-version release после terminal result | Crash оставляет lock и блокирует takeover до owner recovery |

Production transport требует HTTPS S3 и PostgreSQL `verify-full` с exact SNI и
CA. HTTP S3 и plaintext PostgreSQL разрешены только для staging fixture с
явным local флагом. Runtime image содержит pinned PostgreSQL 18.3 tools и
поддерживает server major 17 и 18.

Deployment находится в `deploy/k8s/base/backup-controller`. Эксплуатация:
[`docs/runbooks/backup-controller.md`](../../../docs/runbooks/backup-controller.md).

Локальное доказательство ненулевого архива Session запускается публичным
entrypoint `scripts/tests/local-session-archive-backup-restore-e2e.sh`. Он
работает только с явно подтверждённым disposable hot-reload профилем: создаёт
объект под `session-archive/v1`, выполняет one-shot backup, проверяет exact
manifest и payload, изменяет source fixture, выполняет изолированный restore и
сравнивает восстановленные bytes с исходными. Все версии fixture и disposable
restore targets удаляются с readback в рамках сценария.
