SELECT id, organization_id, project_id, execution_id, execution_fence,
    kind, evidence_sha256, workload_id, occurred_at
FROM control_plane.runtime_execution_incidents
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND id > coalesce(
      nullif(@after_id, '')::uuid,
      '00000000-0000-0000-0000-000000000000'::uuid
  )
ORDER BY id
LIMIT @limit;
