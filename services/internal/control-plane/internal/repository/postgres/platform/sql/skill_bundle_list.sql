-- name: skill_bundle_list :one
WITH visible AS MATERIALIZED (
    SELECT bundle.id,bundle.ref FROM control_plane.skill_bundle_projection bundle
    JOIN control_plane.catalog_access_targets project ON project.organization_id=bundle.organization_id AND project.kind='PROJECT' AND project.id=bundle.project_id
    WHERE bundle.organization_id=@organization_id::uuid
      AND (@project_ref='' OR bundle.project_ref=@project_ref)
      AND (@authority_project='' OR bundle.project_id=NULLIF(@authority_project,'')::uuid)
      AND bundle.state=@state
      AND (@query='' OR bundle.name ILIKE '%' || @query || '%')
      AND (@agent_ref='' OR EXISTS (
          SELECT 1 FROM control_plane.agent_context_bindings binding
          JOIN control_plane.catalog_access_targets agent ON agent.organization_id=binding.organization_id AND agent.kind='AGENT' AND agent.id=binding.agent_id
          WHERE binding.organization_id=bundle.organization_id AND binding.skill_bundle_id=bundle.id AND binding.enabled AND agent.ref=@agent_ref
            AND control_plane.catalog_resource_visible(bundle.organization_id,@actor_id::uuid,'agent.view','AGENT',agent.id,agent.project_id,agent.owner_id,agent.related_ids,@evaluated_at,false)))
      AND control_plane.catalog_resource_visible(bundle.organization_id,@actor_id::uuid,'project.view','PROJECT',project.id,project.project_id,project.owner_id,project.related_ids,@evaluated_at,false)
      AND control_plane.skill_revision_visible(bundle.organization_id,@actor_id::uuid,bundle.current_revision_id,@evaluated_at)
      AND control_plane.skill_revision_visible(bundle.organization_id,@actor_id::uuid,bundle.draft_revision_id,@evaluated_at)
), page AS (
    SELECT id,ref FROM visible WHERE (@cursor_ref='' OR ref>@cursor_ref) ORDER BY ref LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(bundle.projection ORDER BY page.ref),'[]'::jsonb),(SELECT count(*) FROM visible)
FROM page JOIN control_plane.skill_bundle_projection bundle ON bundle.id=page.id;
