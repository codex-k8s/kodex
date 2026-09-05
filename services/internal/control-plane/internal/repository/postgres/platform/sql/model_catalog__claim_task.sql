-- name: model_catalog__claim_task :exec
UPDATE control_plane.provider_model_catalog_tasks
SET state = 'CLAIMED', claimant_id = $2, claim_generation = $3, fence = $4, request_digest = $5, expires_at = $6
WHERE id = $1::uuid AND state = 'PENDING';
