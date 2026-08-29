-- name: attachmentsets_insert_binding :exec
INSERT INTO control_plane.attachment_bindings(
    ref, organization_id, project_id, attachment_set_id, target_kind,
    target_ref, created_by
) VALUES (
    @binding_ref, @organization_id::uuid, @project_id::uuid,
    @attachment_set_id::uuid, @target_kind, @target_ref, @created_by::uuid
);
