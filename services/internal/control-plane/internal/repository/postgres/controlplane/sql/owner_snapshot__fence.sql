SELECT concat_ws(':',
    count(resource.id)::text,
    coalesce(sum(resource.version), 0)::text,
    coalesce(max(resource.updated_at)::text, ''),
    (
        SELECT count(incident.id)::text
        FROM control_plane.runtime_execution_incidents AS incident
        WHERE incident.organization_id = @organization_id::uuid
          AND incident.project_id = @project_id::uuid
    ),
    (
        SELECT coalesce(sum(incident.version), 0)::text
        FROM control_plane.runtime_execution_incidents AS incident
        WHERE incident.organization_id = @organization_id::uuid
          AND incident.project_id = @project_id::uuid
    ),
    (
        SELECT coalesce(max(incident.updated_at)::text, '')
        FROM control_plane.runtime_execution_incidents AS incident
        WHERE incident.organization_id = @organization_id::uuid
          AND incident.project_id = @project_id::uuid
    )
)
FROM control_plane.resources AS resource
WHERE resource.organization_id = @organization_id::uuid
  AND resource.project_id = @project_id::uuid
  AND resource.owner_actor_id = @actor_id::uuid;
