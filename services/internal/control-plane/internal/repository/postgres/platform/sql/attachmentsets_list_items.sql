-- name: attachmentsets_list_items :many
SELECT item.artifact_id::text, item.artifact_ref, item.artifact_revision,
       item.artifact_version, item.file_name, item.media_type, item.size_bytes,
       item.digest, item.source, item.position
FROM control_plane.attachment_set_items item
WHERE item.attachment_set_id = $1::uuid
ORDER BY item.position;
