SELECT id::text, organization_id::text, project_id::text, execution_id::text,
    execution_fence, kind, evidence_sha256, workload_id, occurred_at,
    version, state, coalesce(action_reason_code, ''), updated_at
FROM control_plane.runtime_execution_incidents
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND id = @incident_id::uuid
FOR UPDATE;
