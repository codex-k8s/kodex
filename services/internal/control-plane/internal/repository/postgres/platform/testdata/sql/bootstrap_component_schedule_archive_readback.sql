-- name: bootstrap_component_schedule_archive_readback :one
SELECT schedule.lifecycle_state,
       schedule.enabled,
       schedule.next_run_at IS NULL,
       occurrence.state,
       occurrence.lease_ref IS NULL
         AND occurrence.fence_digest IS NULL
         AND occurrence.workload_instance IS NULL
         AND occurrence.lease_expires_at IS NULL,
       (SELECT count(*)
        FROM control_plane.audit_events audit
        WHERE audit.resource_ref = schedule.ref
          AND audit.action = 'controlplane.archive_schedule'),
       (SELECT count(*)
        FROM control_plane.outbox_events event
        WHERE convert_from(event.payload, 'UTF8')::jsonb ->> 'eventName' = 'SCHEDULE_CHANGED'
          AND convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateRef' = schedule.ref
          AND (convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateVersion')::bigint = schedule.version
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,state}' = 'ARCHIVED'
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,safeSummary}' = 'i18n:SCHEDULE_ARCHIVED')
FROM control_plane.schedules schedule
JOIN control_plane.schedule_occurrences occurrence
  ON occurrence.schedule_id = schedule.id
WHERE schedule.ref = $1
  AND occurrence.ref = $2
