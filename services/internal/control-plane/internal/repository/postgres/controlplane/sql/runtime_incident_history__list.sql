SELECT incident_id::text, version, state, action, reason_code,
    occurred_at, owner_actor_id::text
FROM control_plane.runtime_incident_history
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @actor_id::uuid
  AND incident_id = @incident_id::uuid
  AND version < coalesce(nullif(@before_version, 0), 9007199254740991)
ORDER BY version DESC
LIMIT @limit;
