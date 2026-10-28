-- name: owner_gate_expiry_proof :one
WITH selected AS (SELECT id FROM control_plane.runs WHERE ref=@root_ref)
SELECT
 (SELECT count(*) FROM control_plane.run_nodes n,selected r WHERE n.root_run_id=r.id AND n.state IN ('PLANNED','QUEUED','RUNNING','WAITING')),
 (SELECT count(*) FROM control_plane.runtime_leases l JOIN control_plane.runs run ON run.id=l.run_id,selected r WHERE run.root_run_id=r.id AND l.state='CLAIMED'),
 (SELECT count(*) FROM control_plane.owner_gates g,selected r WHERE g.root_run_id=r.id AND g.state='OPEN'),
 (SELECT count(*) FROM control_plane.run_events e,selected r WHERE e.root_run_id=r.id AND e.gate_ref=@gate_ref AND e.type='OWNER_GATE_RESOLVED'),
 (SELECT count(*) FROM control_plane.run_events e,selected r WHERE e.root_run_id=r.id),
 (SELECT count(*) FROM control_plane.integration_invocations i JOIN control_plane.runs run ON run.id=i.run_id,selected r WHERE run.root_run_id=r.id AND i.state IN ('WAITING_APPROVAL','READY','RUNNING'));
