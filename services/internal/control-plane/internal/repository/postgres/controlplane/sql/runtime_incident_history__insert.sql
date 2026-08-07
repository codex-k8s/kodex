INSERT INTO control_plane.runtime_incident_history (
    organization_id, project_id, incident_id, version, execution_fence, owner_actor_id,
    state, action, reason_code, occurred_at
) SELECT incident.organization_id, incident.project_id, incident.id,
    incident.version, incident.execution_fence, @owner_actor_id::uuid,
    incident.state, @action, @reason_code, @occurred_at
FROM control_plane.runtime_execution_incidents AS incident
WHERE incident.organization_id = @organization_id::uuid
  AND incident.project_id = @project_id::uuid
  AND incident.id = @incident_id::uuid
  AND incident.version = @version
  AND incident.execution_fence = @execution_fence
  AND incident.state = @state
  AND incident.action_reason_code = @reason_code;
