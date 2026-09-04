-- name: secret_impact_consumers :many
WITH eligible AS MATERIALIZED (
    SELECT COALESCE(binding.ref,revision.ref) AS cursor_ref,environment.ref AS environment_ref,
        environment.version AS environment_version,revision.ref AS environment_version_ref,secrets.revisions,
        COALESCE(agent.ref,'') AS agent_ref,COALESCE(agent.version,0) AS agent_version,
        COALESCE(binding.ref,'') AS binding_ref,COALESCE(binding.version,0) AS binding_version,project.ref AS project_ref
    FROM control_plane.runtime_environment_sets environment
    JOIN control_plane.projects project ON project.id=environment.project_id AND project.lifecycle='ACTIVE'
    JOIN control_plane.catalog_access_targets project_target ON project_target.organization_id=environment.organization_id
      AND project_target.kind='PROJECT' AND project_target.id=project.id
    JOIN control_plane.runtime_environment_versions revision ON revision.environment_set_id=environment.id
    JOIN LATERAL (
        SELECT array_agg(DISTINCT (descriptor->>'revision')::bigint) AS revisions
        FROM jsonb_array_elements(revision.secret_descriptors) descriptor
        WHERE descriptor->>'secret_ref'=@secret_ref AND (descriptor->>'revision')::bigint<>@target_revision::bigint
    ) secrets ON secrets.revisions IS NOT NULL
    LEFT JOIN control_plane.agent_runtime_environment_bindings binding ON binding.environment_version_id=revision.id
    LEFT JOIN control_plane.agents agent ON agent.id=binding.agent_id
    LEFT JOIN control_plane.catalog_access_targets target ON target.organization_id=agent.organization_id AND target.kind='AGENT' AND target.id=agent.id
    WHERE environment.organization_id=@organization_id::uuid AND environment.state='ACTIVE'
      AND (revision.id=environment.current_version_id OR binding.id IS NOT NULL)
      AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
      AND control_plane.catalog_resource_visible(environment.organization_id,@actor_id::uuid,'project.manage',
          'PROJECT',project.id,project.id,project_target.owner_id,project_target.related_ids,@evaluated_at)
      AND (binding.id IS NULL OR (agent.state<>'ARCHIVED' AND control_plane.catalog_resource_visible(environment.organization_id,@actor_id::uuid,'agent.manage',
          'AGENT',agent.id,agent.project_id,target.owner_id,target.related_ids,@evaluated_at)))
), page AS (
    SELECT * FROM eligible WHERE cursor_ref>@cursor_ref ORDER BY cursor_ref LIMIT @page_size
)
SELECT COALESCE(page.cursor_ref,''),COALESCE(page.environment_ref,''),COALESCE(page.environment_version,0),
    COALESCE(page.environment_version_ref,''),COALESCE(page.revisions,'{}'::bigint[]),COALESCE(page.agent_ref,''),
    COALESCE(page.agent_version,0),COALESCE(page.binding_ref,''),COALESCE(page.binding_version,0),COALESCE(page.project_ref,''),totals.total
FROM (SELECT count(*) AS total FROM eligible) totals LEFT JOIN page ON true ORDER BY page.cursor_ref;
