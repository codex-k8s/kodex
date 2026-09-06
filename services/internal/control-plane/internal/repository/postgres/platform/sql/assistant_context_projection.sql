-- name: assistant_context_projection :one
SELECT COALESCE(project_id::text,''), project_ref, entity_name, entity_version, allowed_operations
FROM control_plane.assistant_context_projection(
    @organization_id::uuid, @actor_id::uuid, NULLIF(@authority_project,'')::uuid,
    @context_kind, @context_ref, transaction_timestamp(),
    (SELECT project.id FROM control_plane.projects project WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref));
