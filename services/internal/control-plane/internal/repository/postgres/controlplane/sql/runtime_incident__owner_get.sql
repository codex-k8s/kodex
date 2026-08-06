SELECT incident.id::text, incident.organization_id::text, incident.project_id::text,
    incident.execution_id::text, incident.execution_fence, incident.kind,
    incident.evidence_sha256, incident.workload_id, incident.occurred_at,
    incident.version, incident.state, coalesce(incident.action_reason_code, ''),
    incident.updated_at
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
 AND process.state <> 'DELETED'
 AND process.owner_actor_id = @actor_id::uuid
WHERE incident.organization_id = @organization_id::uuid
  AND incident.project_id = @project_id::uuid
  AND incident.id = @incident_id::uuid;
