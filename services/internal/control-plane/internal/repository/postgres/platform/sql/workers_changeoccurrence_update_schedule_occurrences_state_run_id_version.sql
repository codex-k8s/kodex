-- name: workers_changeoccurrence_update_schedule_occurrences_state_run_id_version :exec
UPDATE control_plane.schedule_occurrences SET state='MATERIALIZED',run_id=$2::uuid,lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
