-- name: prompt_context_skills :many
SELECT binding.ref,binding.version,bundle.ref,control_plane.skill_revision_projection(revision.id)
FROM control_plane.agent_context_bindings binding
JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
JOIN control_plane.skill_bundles bundle ON bundle.id=binding.skill_bundle_id AND bundle.project_id=binding.project_id
JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id AND revision.bundle_id=bundle.id
JOIN control_plane.catalog_access_targets project ON project.organization_id=binding.organization_id AND project.kind='PROJECT' AND project.id=binding.project_id
JOIN control_plane.catalog_access_targets agent_target ON agent_target.organization_id=binding.organization_id AND agent_target.kind='AGENT' AND agent_target.id=agent.id
WHERE binding.organization_id=@organization_id::uuid AND agent.ref=@agent_ref AND project.ref=@project_ref
  AND binding.enabled AND bundle.state='ACTIVE' AND revision.state='PUBLISHED' AND revision.scan_state='CLEAN'
  AND revision.reviewed_by IS NOT NULL AND revision.reviewed_at IS NOT NULL
  AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'project.view','PROJECT',project.id,project.project_id,project.owner_id,project.related_ids,@evaluated_at,false)
  AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'agent.view','AGENT',agent.id,agent.project_id,agent_target.owner_id,agent_target.related_ids,@evaluated_at,false)
  AND control_plane.skill_revision_visible(binding.organization_id,@actor_id::uuid,revision.id,@evaluated_at)
ORDER BY binding.ref LIMIT 33;
