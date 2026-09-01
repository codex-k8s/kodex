-- name: access_sync_lock_subject :one
SELECT 1
FROM control_plane.subjects
WHERE organization_id = @organization_id::uuid AND id = @subject_id::uuid
FOR UPDATE
