-- name: project_assistant__context_creation_allowed :one
SELECT control_plane.assistant_project_profile_creation_allowed(
    @organization_id::uuid,@actor_id::uuid,
    (SELECT project.id FROM control_plane.projects project
        WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref),
    @assistant_scope,@context_kind)
  AND (@project_ref='' OR EXISTS (SELECT 1 FROM control_plane.projects project
      WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref));
