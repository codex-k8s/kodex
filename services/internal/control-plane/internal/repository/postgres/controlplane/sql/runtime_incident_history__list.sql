SELECT incident_id::text, version, execution_fence, state, action, reason_code,
    occurred_at, owner_actor_id::text
FROM control_plane.runtime_incident_history
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND (
      owner_actor_id = @actor_id::uuid
      OR EXISTS (
          SELECT 1
          FROM control_plane.project_actor_permissions AS permission
          WHERE permission.organization_id = runtime_incident_history.organization_id
            AND permission.project_id = runtime_incident_history.project_id
            AND permission.actor_id = @actor_id::uuid
            AND permission.permission IN (
                'controlplane.runtime_execution.incident.read',
                'controlplane.runtime_execution.incident.manage'
            )
      )
  )
  AND incident_id = @incident_id::uuid
  AND version < coalesce(nullif(@before_version, 0), 9007199254740991)
ORDER BY version DESC
LIMIT @limit;
