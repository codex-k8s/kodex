INSERT INTO control_plane.runtime_incident_history (
    organization_id, project_id, incident_id, version, execution_fence, owner_actor_id,
    state, action, reason_code, occurred_at
)
SELECT incident.organization_id, incident.project_id, incident.id,
    incident.version, incident.execution_fence, process.owner_actor_id, incident.state,
    'record', 'incident_recorded', incident.occurred_at
FROM control_plane.runtime_execution_incidents AS incident
JOIN control_plane.runtime_executions AS execution
  ON execution.organization_id = incident.organization_id
 AND execution.project_id = incident.project_id
 AND execution.id = incident.execution_id
JOIN control_plane.resources AS process
  ON process.organization_id = execution.organization_id
 AND process.project_id = execution.project_id
 AND process.id = execution.process_id
 AND process.kind = 'PROCESS_RUN'
WHERE incident.organization_id = @organization_id::uuid
  AND incident.project_id = @project_id::uuid
  AND incident.id = @incident_id::uuid;
