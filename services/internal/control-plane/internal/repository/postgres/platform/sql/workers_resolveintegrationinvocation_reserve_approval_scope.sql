-- name: workers_resolveintegrationinvocation_reserve_approval_scope :exec
UPDATE control_plane.integration_approval_scopes
SET reserved_effects=reserved_effects+1
WHERE id=$1::uuid AND revoked_at IS NULL AND expires_at>clock_timestamp()
  AND reserved_effects<max_effects
