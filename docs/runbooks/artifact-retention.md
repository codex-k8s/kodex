---
id: RUNBOOK-DOC-009
title: Artifact retention
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-29
---

# Artifact retention

## Назначение

`artifact-retention` удаляет exact S3 object version артефактов через 30 дней
после помещения в корзину. До успешного удаления объекта PostgreSQL tombstone
не фиксируется.

## Диагностика

1. Проверить `/readyz` и метрику
   `kodex_artifact_retention_readiness{ready="true"}`.
2. Проверить bounded failures по
   `kodex_artifact_retention_failures_total`, не извлекая object key или
   credentials в логи.
3. Проверить доступность PostgreSQL и S3 теми же credentials и exact endpoint,
   которые смонтированы workload. Значения секретов не выводить.
4. Для зависшего `PURGE_PENDING` проверить `retention_claim_owner`, generation и
   expiry. После expiry новую попытку безопасно получает другая реплика.

## Восстановление

- До `purge_after` пользователь восстанавливает `DELETED` artifact через UI.
- После `PURGED` восстановление невозможно: object version удалена, а в БД
  остаётся только tombstone и audit.
- Не удалять claim вручную во время действующего lease. При недоступности S3
  восстановить storage path и дождаться следующего bounded polling cycle.

Rollback deployment останавливает новые claims, но не возвращает уже удалённые
object versions. Откат миграции допустим только когда нет `PURGE_PENDING` claims
и workload остановлен.

## Disposable local E2E

Канонический `./dev.sh full-e2e --context <exact-local-context>` проверяет через
Control Center переходы `DELETE -> RESTORE`, `DELETE -> PURGE` и очистку всей
корзины. Перед каждым необратимым действием сценарий сохраняет в приватном
каталоге E2E точные `object_key` и `object_version`; после ответа API он
независимо требует одновременно `PURGED` tombstone без content row и
`NotFound` для этой exact S3 version в локальном SeaweedFS.

Заявление о 30 днях сначала проверяется по авторитетным `deleted_at` и
`purge_after`. Ожидание 30 суток в тесте запрещено: только в disposable local
профиле `scripts/tests/local-artifact-storage-e2e.sh accelerate-retention`
переводит `purge_after` одного заранее удалённого artifact в прошлое, ждёт
штатный `artifact-retention` и повторяет exact-version readback. Production
clock, retention constant и прочие tombstone этот fixture не меняет.

Скрипт требует exact local context, namespace labels, приватный state directory
и `KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION`.
Отсутствующий API, storage locator, tombstone либо object readback является
`FAIL`; обязательная local-фаза не имеет conditional skip.
