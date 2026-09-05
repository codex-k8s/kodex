-- name: environment_impact_consumers :many
WITH eligible AS MATERIALIZED (
    SELECT agent.ref AS agent_ref, agent.version AS agent_version, binding.ref AS binding_ref,
           binding.version AS binding_version, revision.ref AS version_ref, project.ref AS project_ref
    FROM control_plane.agent_runtime_environment_bindings binding
    JOIN control_plane.agents agent ON agent.id = binding.agent_id AND agent.state <> 'ARCHIVED'
    JOIN control_plane.projects project ON project.id = agent.project_id AND project.lifecycle = 'ACTIVE'
    JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
    JOIN control_plane.runtime_environment_versions revision ON revision.id = binding.environment_version_id
    JOIN control_plane.catalog_access_targets target
      ON target.organization_id = binding.organization_id AND target.kind = 'AGENT' AND target.id = agent.id
    WHERE binding.organization_id = @organization_id::uuid AND environment.ref = @environment_ref
      AND revision.ref <> @target_ref
      AND (@query='' OR strpos(lower(agent.name || ' ' || agent.ref || ' ' || project.ref),lower(@query))>0)
      AND control_plane.catalog_resource_visible(binding.organization_id, @actor_id::uuid, 'agent.manage',
          target.kind, target.id, target.project_id, target.owner_id, target.related_ids, @evaluated_at)
), page AS (
    SELECT * FROM eligible WHERE agent_ref > @cursor_ref ORDER BY agent_ref LIMIT @page_size
)
SELECT COALESCE(page.agent_ref,''), COALESCE(page.agent_version,0), COALESCE(page.binding_ref,''),
       COALESCE(page.binding_version,0), COALESCE(page.version_ref,''), COALESCE(page.project_ref,''), totals.total
FROM (SELECT count(*) AS total FROM eligible) totals LEFT JOIN page ON true ORDER BY page.agent_ref;
