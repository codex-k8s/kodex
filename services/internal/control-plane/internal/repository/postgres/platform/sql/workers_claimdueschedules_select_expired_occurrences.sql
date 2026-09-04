-- name: workers_claimdueschedules_select_expired_occurrences :many
SELECT occurrence.id::text,
       occurrence.ref,
       schedule.ref,
       occurrence.scheduled_for,
       occurrence.schedule_version,
       occurrence.input_digest,
       occurrence.generation,
       revision.ref,
       revision.revision,
       revision.digest
FROM control_plane.schedule_occurrences occurrence
JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
JOIN control_plane.schedule_revisions revision ON revision.id = occurrence.schedule_revision_id
WHERE occurrence.organization_id = $1::uuid
  AND occurrence.state = 'CLAIMED'
  AND occurrence.lease_expires_at <= clock_timestamp()
  AND schedule.lifecycle_state = 'ACTIVE'
  AND schedule.enabled
ORDER BY occurrence.lease_expires_at, occurrence.created_at
FOR UPDATE OF occurrence SKIP LOCKED
LIMIT $2
