-- name: attachmentsets_insert_item :exec
INSERT INTO control_plane.attachment_set_items(
    attachment_set_id, position, artifact_id, artifact_ref, artifact_revision,
    artifact_version, file_name, media_type, size_bytes, digest
) VALUES (
    @attachment_set_id::uuid, @position, @artifact_id::uuid, @artifact_ref,
    @artifact_revision, @artifact_version, @file_name, @media_type,
    @size_bytes, @digest
);
