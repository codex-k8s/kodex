-- name: attachmentsets_insert_binding :exec
INSERT INTO control_plane.attachment_bindings(
    ref, organization_id, project_id, attachment_set_id, kind,
    assistant_turn_id, session_turn_id, run_id, owner_gate_id, created_by
) VALUES (
    @binding_ref, @organization_id::uuid, NULLIF(@project_id, '')::uuid,
    @attachment_set_id::uuid, @kind, @assistant_turn_id::uuid,
    @session_turn_id::uuid, @run_id::uuid, @owner_gate_id::uuid,
    @created_by::uuid
);
