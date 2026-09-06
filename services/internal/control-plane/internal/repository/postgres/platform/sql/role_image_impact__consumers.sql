-- name: role_image_impact__consumers :many
WITH sources AS (
 SELECT environment.id AS environment_id,environment.ref,environment.version,
        revision.id AS revision_id,revision.ref AS revision_ref,revision.digest,
        project.id AS project_id,project.ref AS project_ref,project.created_by,
        environment.current_version_id=revision.id AS current_version
 FROM control_plane.runtime_environment_sets environment
 JOIN control_plane.projects project ON project.id=environment.project_id AND project.organization_id=environment.organization_id
 JOIN control_plane.runtime_environment_versions revision ON revision.environment_set_id=environment.id
  AND revision.organization_id=environment.organization_id
 JOIN control_plane.image_artifacts image ON image.id=revision.role_image_artifact_id AND image.organization_id=environment.organization_id
 JOIN control_plane.role_image_recipes recipe ON recipe.id=image.recipe_id AND recipe.organization_id=image.organization_id
 WHERE environment.organization_id=@organization_id::uuid AND recipe.ref=@recipe_ref
  AND image.ref<>@artifact_ref AND environment.state='ACTIVE' AND project.lifecycle='ACTIVE'
  AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
  AND control_plane.catalog_resource_visible(environment.organization_id,@actor_id::uuid,'project.manage',
    'PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,@evaluated_at)
), consumers AS (
 SELECT source.ref AS environment_ref,source.version AS environment_version,source.revision_ref,source.digest,
        source.project_ref,''::text AS agent_ref,0::bigint AS agent_version,''::text AS binding_ref,0::bigint AS binding_version
 FROM sources source WHERE source.current_version
 UNION ALL
 SELECT source.ref,source.version,source.revision_ref,source.digest,source.project_ref,
        agent.ref,agent.version,binding.ref,binding.version
 FROM sources source
 JOIN control_plane.agent_runtime_environment_bindings binding ON binding.environment_set_id=source.environment_id
  AND binding.environment_version_id=source.revision_id AND binding.organization_id=@organization_id::uuid
 JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.organization_id=binding.organization_id
  AND agent.project_id=source.project_id AND agent.state<>'ARCHIVED'
 JOIN control_plane.catalog_access_targets target ON target.organization_id=agent.organization_id AND target.kind='AGENT' AND target.id=agent.id
 WHERE control_plane.catalog_resource_visible(agent.organization_id,@actor_id::uuid,'agent.manage',
   'AGENT',agent.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at)
)
SELECT environment_ref,environment_version,revision_ref,digest,project_ref,agent_ref,agent_version,binding_ref,binding_version
FROM consumers ORDER BY environment_ref COLLATE "C",revision_ref COLLATE "C",agent_ref COLLATE "C" LIMIT 1001;
