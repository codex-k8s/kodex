-- name: revision_impact__environment_consumers :many
SELECT project.ref,agent.ref,agent.version,binding.ref,binding.version,revision.ref
FROM control_plane.agent_runtime_environment_bindings binding
JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.organization_id=binding.organization_id AND agent.state<>'ARCHIVED'
JOIN control_plane.projects project ON project.id=agent.project_id AND project.organization_id=binding.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.runtime_environment_sets environment ON environment.id=binding.environment_set_id AND environment.organization_id=binding.organization_id
JOIN control_plane.runtime_environment_versions revision ON revision.id=binding.environment_version_id AND revision.environment_set_id=environment.id AND revision.organization_id=binding.organization_id
JOIN control_plane.catalog_access_targets target ON target.organization_id=binding.organization_id AND target.kind='AGENT' AND target.id=agent.id
WHERE binding.organization_id=@organization_id::uuid AND environment.ref=@environment_ref
 AND (@authority_project='' OR project.id::text=@authority_project)
 AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'agent.manage',
     target.kind,target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at)
ORDER BY agent.ref LIMIT 1001;
