-- name: workers_claimdueschedules_select_expired_occurrences :many
SELECT occurrence.id::text,
       occurrence.ref,
       schedule.ref,
       occurrence.scheduled_for,
       occurrence.schedule_version,
       occurrence.input_digest,
       occurrence.generation,
       occurrence.attempt,
       revision.ref,
       revision.revision,
       revision.digest,
       occurrence.target_ref,
       occurrence.target_version,
       occurrence.target_digest,
       occurrence.automation_text_digest,
       occurrence.prompt_inputs_digest
FROM control_plane.schedule_occurrences occurrence
JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
JOIN control_plane.schedule_revisions revision ON revision.id = occurrence.schedule_revision_id
WHERE occurrence.organization_id = $1::uuid
  AND occurrence.state IN ('CLAIMED', 'RETRY_WAIT')
  AND (occurrence.state = 'RETRY_WAIT' OR occurrence.lease_expires_at <= clock_timestamp())
  AND occurrence.attempt < 3
  AND schedule.lifecycle_state = 'ACTIVE'
  AND schedule.enabled
ORDER BY occurrence.lease_expires_at NULLS FIRST, occurrence.created_at
FOR UPDATE OF occurrence SKIP LOCKED
LIMIT $2
