-- name: interaction_approval_readback :one
SELECT gate.ref,gate.state,delivery.ref,delivery.state,run.state,run.version,
    (SELECT count(*) FROM control_plane.interaction_deliveries d WHERE d.root_run_id=run.id AND d.capability_key='mattermost.notifications')
FROM control_plane.runs run
JOIN control_plane.interaction_deliveries delivery ON delivery.root_run_id=run.id AND delivery.capability_key='mattermost.notifications'
JOIN control_plane.owner_gates gate ON gate.id=delivery.approval_gate_id
WHERE run.ref=$1;
