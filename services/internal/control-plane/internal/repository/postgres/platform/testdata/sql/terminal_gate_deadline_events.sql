-- name: terminal_gate_deadline_events :one
SELECT COALESCE(jsonb_agg(jsonb_build_array(event.type,event.aggregate_ref,COALESCE(event.node_ref,''),
 COALESCE(event.gate_ref,''),COALESCE(event.run_state,''),COALESCE(event.node_state,''),event.safe_summary)
 ORDER BY event.sequence),'[]'::jsonb)
FROM control_plane.run_events event
JOIN control_plane.runs root ON root.id=event.root_run_id AND root.organization_id=event.organization_id
WHERE root.ref=$1 AND event.sequence>$2;
