-- name: platform__runtime_completeexecution_1 :exec
UPDATE control_plane.runtime_leases SET state='COMPLETED',updated_at=clock_timestamp() WHERE id=$1::uuid
