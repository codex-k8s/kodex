-- name: configuration_changeschedule_insert_schedule_revision :one
INSERT INTO control_plane.schedule_revisions(
    id, ref, organization_id, schedule_id, revision, name, target_type,
    target_ref, preset, cron_expression, timezone, input, session_policy,
    notification_policy, digest, created_by
) VALUES (
    $1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14, $15, $16::uuid
)
RETURNING created_at
