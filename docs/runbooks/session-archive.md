---
id: RUN-MC-024
title: Диагностика session-archive
type: runbook
status: approved
owner: sre
version: 1.2.0
updated: 2026-08-30
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

Перед созданием `SNAPSHOT` или `RESTORE` Job controller читает exact PVC,
проверяет canonical Session binding и фиксирует Kubernetes UID в аннотации Job
`session-archive.kodex.dev/source-pvc-uid`. Во время выполнения controller
повторно сверяет UID на каждом observation cycle. Он не разбирает текст
scheduler event: отсутствие PVC даёт `SESSION_ARCHIVE_PVC_MISSING`, а объект с
тем же именем и другим UID - `SESSION_ARCHIVE_PVC_REPLACED`. В обоих случаях
Job и task ConfigMap удаляются, после чего результат проходит через fenced
`FailSessionArchiveTask`.

Эти ошибки повторяются только в пределах `maximum_attempts`. После исчерпания
попыток task переходит в `DEAD_LETTER`, а `session_storage` - в `ERROR`; новый
snapshot task автоматически не материализуется. Это закрытый отказ при утрате
или подмене state volume, а не разрешение создать пустой PVC или считать данные
заархивированными. Для восстановления сначала установить причину удаления и
подтвердить источник данных; обходить состояние через ручной SQL, пересоздание
одноимённого PVC или принудительное завершение task запрещено.

Retry выполняет controller через новый fenced claim. Не переводить task или
`session_storage` вручную SQL-запросом и не удалять PVC/Object вручную.
Истёкший snapshot claim создаёт отдельную cleanup-задачу для attempt-specific
object key; dead-letter требует расследования причины до повторного запуска.

## Локальная проверка SeaweedFS

Local bootstrap создаёт bucket `kodex-session-archives`. Канонический
`./dev.sh full-e2e --context <exact-local-context>` запускает
`scripts/tests/local-session-archive-e2e.sh`: wrapper сам читает локальные
Kubernetes Secret во временные файлы режима `0600`, поднимает только loopback
port-forward и вызывает существующий SeaweedFS E2E.

Проверка записывает реальную сессию, получает immutable object version,
восстанавливает и побайтно сверяет исходный JSONL, затем удаляет exact version и
требует `NotFound` через тот же object-storage adapter. Wrapper принимает
только exact disposable local context и явное
`KODEX_E2E_CONFIRM_DISPOSABLE`; значения credentials не передаются аргументами
и не выводятся. Отсутствие bucket, readback или delete evidence является
`FAIL`, а не условным успехом.
