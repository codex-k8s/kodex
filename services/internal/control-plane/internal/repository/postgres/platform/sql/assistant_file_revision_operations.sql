-- name: assistant_file_revision_operations :one
SELECT projection.allowed_operations
FROM control_plane.assistant_context_projection_v3(@organization_id::uuid,@actor_id::uuid,NULLIF(@authority_project,'')::uuid,
 @context_kind,@context_ref,transaction_timestamp(),
 (SELECT project.id FROM control_plane.projects project WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref),@helper_scope) projection
