-- name: queries_listrunevents_select_run_events_organization_id_ref :many
SELECT event.ref,
       root.ref,
       event.sequence,
       event.type,
       COALESCE(event.node_ref, ''),
       COALESCE(event.edge_ref, ''),
       COALESCE(event.gate_ref, ''),
       COALESCE(event.artifact_ref, ''),
       event.safe_summary,
       event.safe_progress,
       COALESCE(event.run_state, ''),
       COALESCE(event.node_state, ''),
       event.safe_delta,
       event.occurred_at
FROM control_plane.run_events event
JOIN control_plane.runs root ON root.id = event.root_run_id
WHERE event.organization_id = $1::uuid
  AND root.ref = $2
  AND event.sequence > $3
ORDER BY event.sequence
LIMIT $4
