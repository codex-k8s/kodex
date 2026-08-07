INSERT INTO control_plane.runtime_execution_incidents (
    id, organization_id, project_id, execution_id, execution_fence,
    kind, evidence_sha256, workload_id, occurred_at,
    version, state, updated_at
) VALUES (
    @id, @organization_id, @project_id, @execution_id, @execution_fence,
    @kind, @evidence_sha256, @workload_id, @occurred_at,
    1, 'OPEN', @occurred_at
);
