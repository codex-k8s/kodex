-- name: cfg_copy_insert :one
INSERT INTO control_plane.managed_configuration_sets
 (ref,organization_id,project_id,kind,name,managed_by,source,created_by,copy_provenance)
SELECT @ref,@organization_id::uuid,project.id,@kind,@name,'UI','control-center',@actor_id::uuid,@provenance::jsonb
FROM (SELECT 1) singleton
LEFT JOIN control_plane.projects project ON project.organization_id=@organization_id::uuid AND project.ref=@project_ref AND project.lifecycle='ACTIVE'
WHERE (@kind='INTEGRATION_DEFINITION' AND @project_ref='') OR (@kind='ROLE_IMAGE' AND project.id IS NOT NULL)
RETURNING id::text,ref,COALESCE(project_id::text,''),COALESCE((SELECT ref FROM control_plane.projects WHERE id=project_id),''),kind,name,managed_by,source,source_revision,version,updated_at,'',archived,copy_provenance;
