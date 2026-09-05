-- name: configuration_writeback__candidates :many
SELECT ref FROM control_plane.managed_configuration_writebacks
WHERE organization_id=$1::uuid
 AND (state='WAITING_APPROVAL' AND expires_at<=clock_timestamp()
   OR state='QUEUED'
   OR state IN ('CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp()))
ORDER BY updated_at,id LIMIT $2;
