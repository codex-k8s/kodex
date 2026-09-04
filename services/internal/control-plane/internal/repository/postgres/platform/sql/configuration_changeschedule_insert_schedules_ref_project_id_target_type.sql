-- name: configuration_changeschedule_insert_schedules_ref_project_id_target_type :one
INSERT INTO control_plane.schedules(
    id, ref, organization_id, project_id, name, target_type, target_ref, preset,
    cron_expression, timezone, input, session_policy, notification_policy,
    dst_gap_policy, dst_fold_policy, misfire_policy, overlap_policy,
    target_version, target_digest, automation_text, prompt_inputs,
    enabled, next_run_at, created_by, current_revision_id
) VALUES (
    $1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, true, $22, $23::uuid, $24::uuid
)
RETURNING ref, name, preset, cron_expression, timezone, session_policy,
          notification_policy, lifecycle_state, enabled, version, next_run_at,
          last_run_at, created_at, updated_at, dst_gap_policy, dst_fold_policy,
          misfire_policy, overlap_policy, target_version, target_digest,
          automation_text, prompt_inputs
