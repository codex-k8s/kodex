-- name: attachmentsets_bind_gate_resolution :exec
UPDATE control_plane.owner_gates
SET resolution_attachment_set_id = @attachment_set_id::uuid
WHERE id = @gate_id::uuid
  AND resolution_attachment_set_id IS NULL;
