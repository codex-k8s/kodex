-- name: attachmentsets_select_family :one
SELECT family_ref
FROM control_plane.attachment_sets
WHERE organization_id = $1::uuid
  AND ref = $2
  AND created_by = $3::uuid;
