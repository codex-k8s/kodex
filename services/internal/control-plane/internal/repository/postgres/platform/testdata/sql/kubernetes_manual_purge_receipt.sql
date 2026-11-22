-- name: kubernetes_manual_purge_receipt :one
SELECT receipt.intent_digest,receipt.response_payload
FROM control_plane.idempotency_receipts receipt
WHERE receipt.organization_id=$1::uuid AND receipt.actor_id=$2::uuid
 AND receipt.operation='artifact.purge' AND receipt.idempotency_key=$3;
