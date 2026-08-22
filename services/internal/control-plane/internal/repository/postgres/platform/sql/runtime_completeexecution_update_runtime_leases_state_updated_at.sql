-- name: platform__runtime_completeexecution_update_runtime_leases_state_updated_at :exec
UPDATE control_plane.runtime_leases SET state='COMPLETED',updated_at=clock_timestamp() WHERE id=$1::uuid
