---
id: RUNBOOK-DOC-009
title: Artifact retention
type: runbook
status: approved
owner: sre
version: 1.0.0
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
