-- name: platform__artifacts_uploadartifact_select_idempotency_receipts_organization_id_actor_id_operation :one
SELECT intent_digest,response_payload FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid AND actor_id=$2::uuid AND operation=$3 AND idempotency_key=$4 AND expires_at>clock_timestamp() FOR UPDATE
