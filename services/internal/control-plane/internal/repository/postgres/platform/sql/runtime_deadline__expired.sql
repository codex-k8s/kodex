-- name: runtime_deadline__expired :many
SELECT node.id::text,node.ref,run.id::text,run.ref,run.root_run_id::text,
       COALESCE(run.project_id::text,''),COALESCE(project.ref,''),run.session_id::text,session.ref,''::text
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN LATERAL (SELECT id,ref FROM control_plane.run_nodes
    WHERE run_id=run.id AND organization_id=run.organization_id AND root_run_id=run.root_run_id
      AND type=CASE WHEN run.id=run.root_run_id AND run.target_type='WORKFLOW' AND EXISTS (
        SELECT 1 FROM control_plane.workflow_versions version
        JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=version.organization_id
        WHERE version.id=run.workflow_version_id AND version.organization_id=run.organization_id
          AND workflow.project_id=run.project_id AND workflow.ref=run.target_ref
          AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
      ) THEN 'ROOT_PROCESS' ELSE 'AGENT_EXECUTION' END
      AND (type<>'ROOT_PROCESS' OR (parent_node_id IS NULL AND materialization_state='MATERIALIZED'))
    ORDER BY created_at,id LIMIT 1) node ON true
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
LEFT JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
WHERE run.organization_id=@organization_id::uuid
  AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
  AND (run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING') OR EXISTS (
    SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id
    WHERE gate.organization_id=run.organization_id AND gate_node.run_id=run.id AND gate.state='OPEN'
  ))
  AND run.execution_deadline_at<=clock_timestamp()
ORDER BY run.execution_deadline_at,run.id
FOR UPDATE OF run SKIP LOCKED LIMIT @limit;
