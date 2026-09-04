-- name: workers_claimdueschedules_select_schedules_organization_id :many
SELECT s.id::text,
       s.ref,
       s.next_run_at,
       s.version,
       revision.preset,
       revision.cron_expression,
       revision.timezone,
       revision.name,
       revision.target_type,
       revision.target_ref,
       revision.input,
       s.current_revision_id::text,
       revision.ref,
       revision.revision,
       revision.digest,
       revision.dst_gap_policy,
       revision.dst_fold_policy,
       revision.misfire_policy,
       revision.overlap_policy,
       revision.target_version,
       revision.target_digest,
       revision.automation_text,
       revision.prompt_inputs,
       revision.created_by::text
FROM control_plane.schedules s
JOIN control_plane.schedule_revisions revision ON revision.id = s.current_revision_id
WHERE s.organization_id = $1::uuid
  AND s.lifecycle_state = 'ACTIVE'
  AND s.enabled
  AND s.next_run_at <= clock_timestamp()
  AND NOT EXISTS(SELECT 1
                 FROM control_plane.schedule_occurrences occurrence
                 WHERE occurrence.schedule_id = s.id
                   AND (occurrence.state = 'DEAD_LETTER'
                        OR (occurrence.state IN ('CLAIMED', 'MATERIALIZED', 'RETRY_WAIT')
                            AND revision.overlap_policy = 'FORBID')))
ORDER BY s.next_run_at
FOR UPDATE OF s SKIP LOCKED
LIMIT $2
