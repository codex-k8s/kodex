-- name: role_images_get_admission_claim_receipt :one
SELECT intent_digest, response_payload
FROM control_plane.idempotency_receipts
WHERE organization_id=@organization_id::uuid AND actor_id=@actor_id::uuid
  AND operation='platform.role-images.admission.claim' AND idempotency_key=@claim_key
  AND response_type='IMAGE_ADMISSION_CLAIM' AND expires_at>clock_timestamp()
