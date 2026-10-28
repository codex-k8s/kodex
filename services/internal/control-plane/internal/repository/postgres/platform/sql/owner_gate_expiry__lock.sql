-- name: owner_gate_expiry__lock :one
WITH locked_root AS MATERIALIZED (
    SELECT root.id FROM control_plane.runs root
    JOIN control_plane.owner_gates gate ON gate.root_run_id=root.id AND gate.organization_id=root.organization_id
    WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref
      AND gate.state='OPEN'
      AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
    FOR UPDATE OF root
)
SELECT gate.id::text,node.id::text,node.ref,run.id::text,run.ref,gate.root_run_id::text,
       gate.project_id::text,project.ref,run.session_id::text,session.ref,
       gate.expires_at,gate.expires_at<=clock_timestamp()
FROM locked_root
JOIN control_plane.owner_gates gate ON gate.root_run_id=locked_root.id
JOIN control_plane.run_nodes node ON node.id=gate.node_id AND node.organization_id=gate.organization_id
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=gate.organization_id
JOIN control_plane.projects project ON project.id=gate.project_id AND project.organization_id=gate.organization_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.state='OPEN'
  AND gate.expires_at IS NOT NULL AND gate.integration_invocation_id IS NULL
ORDER BY gate.expires_at,gate.id LIMIT 1 FOR UPDATE OF gate;
