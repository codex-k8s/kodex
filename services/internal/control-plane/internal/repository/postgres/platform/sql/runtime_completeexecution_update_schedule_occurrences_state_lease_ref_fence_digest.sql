-- name: runtime_completeexecution_update_schedule_occurrences_state_lease_ref_fence_digest :one
UPDATE control_plane.schedule_occurrences SET state=$2,lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE run_id=$1::uuid AND state='MATERIALIZED' RETURNING schedule_id::text
