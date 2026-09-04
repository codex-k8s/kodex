-- name: memory_record_list :one
WITH visible AS MATERIALIZED (
    SELECT memory.id,memory.ref FROM control_plane.memory_record_projection memory
    WHERE memory.organization_id=@organization_id::uuid
      AND (@project_ref='' OR memory.project_ref=@project_ref)
      AND (@authority_project='' OR memory.project_id=NULLIF(@authority_project,'')::uuid)
      AND memory.state=@state
      AND (@agent_ref='' OR memory.agent_ref=@agent_ref OR EXISTS (
          SELECT 1 FROM control_plane.agent_context_bindings binding
          JOIN control_plane.agents agent ON agent.id=binding.agent_id
          WHERE binding.organization_id=memory.organization_id AND binding.memory_record_id=memory.id AND binding.enabled AND agent.ref=@agent_ref))
      AND (@query='' OR memory.title ILIKE '%' || @query || '%')
      AND control_plane.memory_record_visible(memory.organization_id,@actor_id::uuid,memory.id,@evaluated_at)
      AND (memory.source_run_id IS NULL OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets source WHERE source.organization_id=memory.organization_id AND source.kind='RUN' AND source.id=memory.source_run_id
          AND control_plane.catalog_resource_visible(memory.organization_id,@actor_id::uuid,'run.view','RUN',source.id,source.project_id,source.owner_id,source.related_ids,@evaluated_at,false)))
), page AS (
    SELECT id,ref FROM visible WHERE (@cursor_ref='' OR ref>@cursor_ref) ORDER BY ref LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(memory.projection ORDER BY page.ref),'[]'::jsonb),(SELECT count(*) FROM visible)
FROM page JOIN control_plane.memory_record_projection memory ON memory.id=page.id;
