-- name: context_bindings_list :many
WITH bindings AS (
    SELECT 'MEMORY'::text AS kind,binding.ref,binding.version,agent.ref AS agent_ref,record.ref AS resource_ref,revision.ref AS revision_ref,revision.digest
    FROM control_plane.agent_context_bindings binding
    JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
    JOIN control_plane.memory_records record ON record.id=binding.memory_record_id AND record.project_id=binding.project_id
    JOIN control_plane.memory_record_revisions revision ON revision.id=binding.memory_revision_id
    WHERE binding.organization_id=@organization_id::uuid AND agent.ref=@agent_ref AND binding.enabled
      AND record.state='ACTIVE'
      AND control_plane.memory_record_visible(binding.organization_id,@actor_id::uuid,record.id,@evaluated_at)
      AND (revision.source_run_id IS NULL OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets source WHERE source.organization_id=binding.organization_id AND source.kind='RUN' AND source.id=revision.source_run_id
            AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'run.view','RUN',source.id,source.project_id,source.owner_id,source.related_ids,@evaluated_at,false)))
    UNION ALL
    SELECT 'SKILL',binding.ref,binding.version,agent.ref,bundle.ref,revision.ref,revision.digest
    FROM control_plane.agent_context_bindings binding
    JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
    JOIN control_plane.skill_bundles bundle ON bundle.id=binding.skill_bundle_id AND bundle.project_id=binding.project_id
    JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id
    JOIN control_plane.catalog_access_targets project ON project.organization_id=binding.organization_id AND project.kind='PROJECT' AND project.id=binding.project_id
    WHERE binding.organization_id=@organization_id::uuid AND agent.ref=@agent_ref AND binding.enabled AND bundle.state='ACTIVE' AND revision.state='PUBLISHED'
      AND control_plane.catalog_resource_visible(binding.organization_id,@actor_id::uuid,'project.view','PROJECT',project.id,project.project_id,project.owner_id,project.related_ids,@evaluated_at,false)
      AND control_plane.skill_revision_visible(binding.organization_id,@actor_id::uuid,revision.id,@evaluated_at)
)
SELECT kind,ref,version,agent_ref,resource_ref,revision_ref,digest FROM bindings ORDER BY ref LIMIT 129;
