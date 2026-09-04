-- name: configuration_changeschedule_insert_schedule_revision :one
INSERT INTO control_plane.schedule_revisions(
    id, ref, organization_id, schedule_id, revision, name, target_type,
    target_ref, preset, cron_expression, timezone, input, session_policy,
    notification_policy, digest, created_by, dst_gap_policy, dst_fold_policy,
    misfire_policy, overlap_policy, target_version, target_digest,
    automation_text, prompt_inputs
) VALUES (
    $1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14, $15, $16::uuid,
    $17, $18, $19, $20, $21, $22, $23, $24
)
RETURNING created_at
