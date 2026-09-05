-- name: template_variables_snapshot :one
SELECT revision.safe_snapshot->'promptSnapshot'
FROM control_plane.runtime_revisions revision
JOIN control_plane.agents agent ON agent.id=revision.agent_id AND agent.organization_id=revision.organization_id
JOIN control_plane.catalog_access_targets target ON target.kind='RUN' AND target.id=revision.run_id AND target.organization_id=revision.organization_id
LEFT JOIN control_plane.projects project ON project.id=revision.project_id
WHERE revision.organization_id=@organization_id::uuid AND revision.ref=@revision_ref
  AND (@project_ref='' OR project.ref=@project_ref)
  AND (@agent_ref='' OR agent.ref=@agent_ref)
  AND (@authority_project='' OR revision.project_id::text=@authority_project)
  AND control_plane.catalog_resource_visible(revision.organization_id,@actor_id::uuid,'run.view',target.kind,
      target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at);
