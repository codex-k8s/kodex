-- name: bootstrap_component_make_interaction_delivery_due :exec
UPDATE control_plane.interaction_deliveries
SET available_at = clock_timestamp()
WHERE ref = $1
  AND state = 'FAILED'
