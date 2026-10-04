-- name: environment_impact_consumers :many
WITH eligible AS MATERIALIZED (
    SELECT agent.ref AS agent_ref, agent.version AS agent_version, binding.ref AS binding_ref,
           binding.version AS binding_version, revision.ref AS version_ref, COALESCE(project.ref,'') AS project_ref,
           environment.scope_kind, organization.ref AS organization_ref
    FROM control_plane.agent_runtime_environment_bindings binding
    JOIN control_plane.agents agent ON agent.id = binding.agent_id AND agent.state <> 'ARCHIVED'
    LEFT JOIN control_plane.projects project ON project.id = agent.project_id AND project.lifecycle = 'ACTIVE'
    JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
    JOIN control_plane.organizations organization ON organization.id = environment.organization_id
    JOIN control_plane.runtime_environment_versions revision ON revision.id = binding.environment_version_id
    LEFT JOIN control_plane.catalog_access_targets target
      ON target.organization_id = binding.organization_id AND target.kind = 'AGENT' AND target.id = agent.id
    WHERE binding.organization_id = @organization_id::uuid AND environment.ref = @environment_ref
      AND ((environment.scope_kind='ORGANIZATION' AND agent.project_id IS NULL
          AND control_plane.organization_role_image_actor_allowed(environment.organization_id,@actor_id::uuid))
          OR (environment.scope_kind='PROJECT' AND agent.project_id=environment.project_id AND project.lifecycle='ACTIVE'))
      AND revision.ref <> @target_ref
      AND (@query='' OR strpos(lower(agent.name || ' ' || agent.ref || ' ' || COALESCE(project.ref,'')),lower(@query))>0)
      AND ((environment.scope_kind='ORGANIZATION' AND agent.system_key='system-assistant')
          OR (environment.scope_kind='PROJECT' AND control_plane.catalog_resource_visible(binding.organization_id, @actor_id::uuid, 'agent.manage',
              target.kind, target.id, target.project_id, target.owner_id, target.related_ids, @evaluated_at)))
), page AS (
    SELECT * FROM eligible WHERE agent_ref > @cursor_ref ORDER BY agent_ref LIMIT @page_size
)
SELECT COALESCE(page.agent_ref,''), COALESCE(page.agent_version,0), COALESCE(page.binding_ref,''),
       COALESCE(page.binding_version,0), COALESCE(page.version_ref,''), COALESCE(page.project_ref,''),
       COALESCE(page.scope_kind,''), COALESCE(page.organization_ref,''), totals.total
FROM (SELECT count(*) AS total FROM eligible) totals LEFT JOIN page ON true ORDER BY page.agent_ref;
