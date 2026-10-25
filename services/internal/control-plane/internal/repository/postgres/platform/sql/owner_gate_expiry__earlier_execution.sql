-- name: owner_gate_expiry__earlier_execution :one
SELECT node.id::text,node.ref,run.id::text,run.ref,run.root_run_id::text,
       COALESCE(run.project_id::text,''),COALESCE(project.ref,''),run.execution_deadline_at
FROM control_plane.runs run
JOIN LATERAL (SELECT id,ref FROM control_plane.run_nodes
    WHERE run_id=run.id AND organization_id=run.organization_id AND type='AGENT_EXECUTION'
    ORDER BY created_at,id LIMIT 1) node ON true
LEFT JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.root_run_id=@root_run_id::uuid
  AND run.execution_deadline_at<=@expires_at::timestamptz
  AND (run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING') OR EXISTS (
    SELECT 1 FROM control_plane.owner_gates gate
    JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
    WHERE gate.organization_id=run.organization_id AND gate_node.run_id=run.id AND gate.state='OPEN'))
ORDER BY run.execution_deadline_at,run.id LIMIT 1;
