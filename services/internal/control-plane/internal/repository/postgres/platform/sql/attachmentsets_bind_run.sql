-- name: attachmentsets_bind_run :exec
UPDATE control_plane.runs
SET input_attachment_set_id = @attachment_set_id::uuid,
    input_artifact_refs = @artifact_refs::text[]
WHERE id = @run_id::uuid
  AND input_attachment_set_id IS NULL;
