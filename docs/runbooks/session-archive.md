---
id: RUN-MC-024
title: Диагностика session-archive
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-28
---

# Диагностика session-archive

`session-archive` владеет snapshot, restore, удалением подтверждённого session
PVC и retention/GC. `control-plane` владеет metadata, task lifecycle,
lease/fence и разрешением переходов. Role Pod и `agent-runner` не получают S3
credentials.

## Probes и метрики

- `/healthz` проверяет process, `/readyz` закрывается при недоступности рабочего
  control-plane path;
- `kodex_session_archive_cycles_total{outcome}` показывает claim-циклы;
- `kodex_session_archive_tasks_total{kind,outcome}` и
  `kodex_session_archive_task_duration_seconds{kind}` показывают задачи;
- `kodex_session_archive_active_workers` и
  `kodex_session_archive_bytes_total{kind}` показывают текущую нагрузку.

## Диагностика

1. Проверить readiness controller и authority sidecar без вывода Secret.
2. По safe task ref проверить в control-plane kind, attempt, generation,
   lease expiry, `safe_error_code` и состояние `session_storage`.
3. Для `SNAPSHOT` проверить отсутствие active turn и действующего runtime lease.
4. Для `RESTORE` проверить exact archive receipt: format, key, version, ETag,
   digest и size. До успешного completion Role Pod не должен создаваться.
5. Для `DELETE_PVC` подтвердить `current_archive_id` и отсутствие Pod, который
   монтирует PVC. Свободное место само по себе не разрешает удаление.
6. Для `DELETE_OBJECT` проверить retention и terminal/superseded lifecycle.

Retry выполняет controller через новый fenced claim. Не переводить task или
`session_storage` вручную SQL-запросом и не удалять PVC/Object вручную.
Истёкший snapshot claim создаёт отдельную cleanup-задачу для attempt-specific
object key; dead-letter требует расследования причины до повторного запуска.

## Локальная проверка SeaweedFS

Local bootstrap создаёт bucket `kodex-session-archives`. После безопасного
port-forward S3 endpoint на loopback запустить
`make test-session-archive-seaweedfs-e2e`, передав только пути к локальным
credential files через `SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE` и
`SESSION_ARCHIVE_E2E_SECRET_KEY_FILE`, а endpoint через
`SESSION_ARCHIVE_E2E_ENDPOINT`. Entrypoint отклоняет любой не-loopback endpoint
и не выводит credentials.
