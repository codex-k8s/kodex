-- name: kubernetes_input_claim_deadline :one
SELECT stage.delivery_expires_at FROM control_plane.kubernetes_input_stages stage
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=stage.stream_id AND stream.organization_id=stage.organization_id
WHERE stage.ref=$1 AND stream.ref=$2 AND stage.state='CLAIMED';
