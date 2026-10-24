---
id: JOB-MC-003
title: Backup controller
type: service
status: approved
owner: backend
version: 1.0.2
updated: 2026-10-25
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

## Проверка контекста сборки

`make test-local-backup-controller-build-contract` за 60 секунд проверяет, что
Docker-контекст содержит все локальные `replace` из `go.mod`, а изменение
`libs/go/objectstorage` меняет входной hash и не переиспользует старый OCI
archive. Неизменённый источник сохраняет прежний cache. Docker и import в этой
регрессии заменены локальными fixtures: проверка не собирает настоящий image,
не обращается к кластеру, не читает credentials и не заменяет restore drill.
Связанные задачи: [#1003](https://github.com/codex-k8s/kodex/issues/1003),
[#1830](https://github.com/codex-k8s/kodex/issues/1830).


Штатная OCI-only сборка: `tools/release/build-stable-local.sh --component
backup-controller --source-sha <чистый-SHA> --output-directory <новый-приватный-каталог>`.
Она использует credential-free `git archive`, создаёт private `build.json` с
source/context/archive/manifest digests и не выполняет import, admission,
publication или node pull. Тот же закрытый component registry должен
допускаться проверкой OCI и publication receipt; незарегистрированные
компоненты и выдуманные результаты проверок остаются запрещены.


## Одноразовый scoped data drill

`tools/dev/scoped_backup_drill.py` предоставляет create-only custody, readback
неизвестного create, отдельный backup Job и render изолированного PG/TLS с
UID cleanup. Полный порядок и lifecycle matrix находятся в
[runbook](../../../docs/runbooks/backup-controller.md#одноразовый-data-drill-10031830).
Новые безопасные CLI: `list-verified`, `inspect-backup`, `inspect-restore-drill`,
`preflight-restore`, `inspect-restore-targets`. Только явный
`cleanup-restore-drill` удаляет receipt-owned target versions;
`inspect-restore-cleanup` читает его частичный/пустой исход без подтверждения
полноты восстановленных данных. Никакие metadata команды не запускают serve.

`make test-scoped-backup-drill` — bounded hermetic custody/render регрессии;
`go test -p 1 ./...` в этом модуле — Go unit. Они не заменяют настоящий drill.
Shared S3 fixture identity не доказывает credential-level prefix isolation;
инструмент не меняет глобальные identities и не передаёт credentials моделям.
Нулевой release revision отвергается runtime; full environment renderer
связывает его с тем же exact backup image из release lock.
