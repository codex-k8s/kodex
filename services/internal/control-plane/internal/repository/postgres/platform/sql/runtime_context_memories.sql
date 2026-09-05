-- name: runtime_context_memories :many
SELECT binding.ref,binding.version,record.ref,control_plane.memory_revision_projection(revision.id)
FROM control_plane.agent_context_bindings binding
JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
JOIN control_plane.projects project ON project.id=binding.project_id AND project.organization_id=binding.organization_id
JOIN control_plane.memory_records record ON record.id=binding.memory_record_id AND record.project_id=binding.project_id
JOIN control_plane.memory_record_revisions revision ON revision.id=binding.memory_revision_id AND revision.record_id=record.id
JOIN control_plane.memory_record_revisions current ON current.id=record.current_revision_id
JOIN control_plane.catalog_access_targets agent_target ON agent_target.organization_id=binding.organization_id AND agent_target.kind='AGENT' AND agent_target.id=agent.id
WHERE binding.organization_id=@organization_id::uuid AND agent.ref=@agent_ref AND project.ref=@project_ref
  AND binding.enabled AND record.state='ACTIVE' AND (record.agent_id IS NULL OR record.agent_id=agent.id)
  AND revision.retention_until>@evaluated_at AND current.retention_until>@evaluated_at
  AND control_plane.memory_record_visible(binding.organization_id,@actor_id::uuid,record.id,@evaluated_at)
  AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'agent.view','AGENT',agent.id,agent.project_id,agent_target.owner_id,agent_target.related_ids,@evaluated_at,false)
  AND (revision.source_run_id IS NULL OR EXISTS (
      SELECT 1 FROM control_plane.catalog_access_targets source WHERE source.organization_id=binding.organization_id AND source.kind='RUN' AND source.id=revision.source_run_id
        AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'run.view','RUN',source.id,source.project_id,source.owner_id,source.related_ids,@evaluated_at,false)))
ORDER BY binding.ref LIMIT 65
FOR SHARE OF binding,record,revision,current;
