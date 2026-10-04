INSERT INTO control_plane.idempotency_receipts
 (organization_id,actor_id,operation,idempotency_key,intent_digest,response_type,response_payload,expires_at)
SELECT organization_id,actor_id,operation,$4,intent_digest,response_type,convert_to($5,'UTF8'),expires_at
FROM control_plane.idempotency_receipts
WHERE organization_id=$1::uuid AND actor_id=$2::uuid AND idempotency_key=$3;
