-- name: attachmentsets_select_timestamps :one
SELECT created_at, finalized_at
FROM control_plane.attachment_sets
WHERE id = $1::uuid;
