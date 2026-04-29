-- name: membership__list_by_subject :many
SELECT id, subject_type, subject_id, target_type, target_id, role_hint, status, source, version, created_at, updated_at
FROM access_memberships
WHERE subject_type = @subject_type AND subject_id = @subject_id AND status = @status;
