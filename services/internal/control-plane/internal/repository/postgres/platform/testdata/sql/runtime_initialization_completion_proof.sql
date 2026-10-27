-- name: runtime_initialization_completion_proof :one
SELECT node.progress_summary,run.result_summary,node.safe_error_code,
 (SELECT count(*) FROM control_plane.run_events event WHERE event.organization_id=node.organization_id
  AND event.aggregate_ref=node.ref AND event.type='TURN_COMPLETED' AND event.safe_summary=node.progress_summary),
 (SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.organization_id=node.organization_id
  AND receipt.idempotency_key=$2)
FROM control_plane.run_nodes node JOIN control_plane.runs run ON run.id=node.run_id
WHERE node.ref=$1;
