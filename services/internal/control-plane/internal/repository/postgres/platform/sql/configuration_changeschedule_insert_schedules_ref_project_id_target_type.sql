-- name: configuration_changeschedule_insert_schedules_ref_project_id_target_type :one
INSERT INTO control_plane.schedules(
    id, ref, organization_id, project_id, name, target_type, target_ref, preset,
    cron_expression, timezone, input, session_policy, notification_policy,
    enabled, next_run_at, created_by, current_revision_id
) VALUES (
    $1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, true, $14, $15::uuid, $16::uuid
)
RETURNING ref, name, preset, cron_expression, timezone, session_policy,
          notification_policy, lifecycle_state, enabled, version, next_run_at,
          last_run_at, created_at, updated_at
