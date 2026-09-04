-- name: configuration_changeschedule_update_schedules_name_target_type_target_ref :one
UPDATE control_plane.schedules s
SET name = $4,
    target_type = $5,
    target_ref = $6,
    preset = $7,
    cron_expression = $8,
    timezone = $9,
    input = $10,
    session_policy = $11,
    notification_policy = $12,
    dst_gap_policy = $13,
    dst_fold_policy = $14,
    misfire_policy = $15,
    overlap_policy = $16,
    target_version = $17,
    target_digest = $18,
    automation_text = $19,
    prompt_inputs = $20,
    next_run_at = $21,
    current_revision_id = $22::uuid,
    version = s.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.projects p
WHERE s.project_id = p.id
  AND s.organization_id = $1::uuid
  AND s.ref = $2
  AND s.version = $3
  AND s.lifecycle_state = 'ACTIVE'
RETURNING s.project_id::text,
          p.ref,
          s.ref,
          s.name,
          s.preset,
          s.cron_expression,
          s.timezone,
          s.session_policy,
          s.notification_policy,
          s.lifecycle_state,
          s.enabled,
          s.version,
          s.next_run_at,
          s.last_run_at,
          s.created_at,
          s.updated_at,
          s.dst_gap_policy,
          s.dst_fold_policy,
          s.misfire_policy,
          s.overlap_policy,
          s.target_version,
          s.target_digest,
          s.automation_text,
          s.prompt_inputs
