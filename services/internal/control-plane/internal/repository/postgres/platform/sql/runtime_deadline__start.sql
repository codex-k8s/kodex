-- name: runtime_deadline__start :exec
UPDATE control_plane.runs run SET execution_started_at=clock_timestamp()
WHERE run.organization_id=@organization_id::uuid AND run.id IN (@run_id::uuid,@root_run_id::uuid)
  AND run.execution_started_at IS NULL AND run.workflow_version_id IS NOT NULL
  AND (run.id=run.root_run_id OR EXISTS (
      SELECT 1 FROM control_plane.run_nodes node
      JOIN control_plane.workflow_versions version ON version.id=run.workflow_version_id
      CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step
      WHERE node.run_id=run.id AND node.organization_id=run.organization_id
        AND node.type='AGENT_EXECUTION' AND step->>'Key'=node.workflow_step_key
  ));
