-- name: attachmentsets_bind_turn :exec
UPDATE control_plane.session_turns
SET attachment_set_id = @attachment_set_id::uuid,
    artifact_refs = @artifact_refs::text[]
WHERE id = @turn_id::uuid
  AND attachment_set_id IS NULL;
