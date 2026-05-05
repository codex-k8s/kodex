-- name: membership__list_by_target :many
SELECT id, subject_type, subject_id, target_type, target_id, role_hint, status, source, version, created_at, updated_at
FROM access_memberships
WHERE target_type = @target_type AND target_id = @target_id AND status = @status;
