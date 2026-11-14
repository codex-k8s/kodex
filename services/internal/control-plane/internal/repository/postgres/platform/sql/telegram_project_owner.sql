-- name: telegram_project_owner :one
SELECT project.id::text
FROM control_plane.projects project
WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref AND project.lifecycle='ACTIVE'
 AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
 AND control_plane.catalog_resource_visible(project.organization_id,@actor_id::uuid,@permission,'PROJECT',
  project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
FOR SHARE OF project;
