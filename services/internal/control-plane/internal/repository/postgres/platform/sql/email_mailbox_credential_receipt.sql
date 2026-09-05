-- name: email_mailbox_credential_receipt :one
SELECT response_payload FROM control_plane.idempotency_receipts
WHERE organization_id=$1::uuid AND actor_id=$2::uuid AND operation=$4
    AND idempotency_key=$3 AND expires_at>clock_timestamp();
