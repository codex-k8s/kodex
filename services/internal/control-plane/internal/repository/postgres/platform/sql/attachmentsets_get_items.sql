-- name: attachmentsets_get_items :many
SELECT item.artifact_ref, item.artifact_revision, item.artifact_version,
       item.file_name, item.media_type, item.size_bytes, item.digest,
       item.source, item.position
FROM control_plane.attachment_set_items item
WHERE item.attachment_set_id = $1::uuid
  AND item.position > $2
ORDER BY item.position
LIMIT $3;
