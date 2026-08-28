-- name: workers_completeintegrationinvocation_insert_effect_receipt :one
INSERT INTO control_plane.integration_effect_receipts(
	ref,organization_id,invocation_id,effect_key,input_digest,provider_effect_ref,response_digest,result_summary
)
VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8)
RETURNING id::text,ref
