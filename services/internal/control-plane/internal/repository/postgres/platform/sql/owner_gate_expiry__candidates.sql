-- name: owner_gate_expiry__candidates :many
SELECT gate.ref
FROM control_plane.owner_gates gate
JOIN control_plane.runs root ON root.id=gate.root_run_id AND root.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.state='OPEN'
  AND gate.integration_invocation_id IS NULL AND gate.expires_at<=clock_timestamp()
  AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
ORDER BY gate.expires_at,gate.id LIMIT @limit;
