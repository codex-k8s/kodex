-- name: platform__workers_changeoccurrence_4 :exec
UPDATE control_plane.schedule_occurrences SET state='MATERIALIZED',run_id=$2::uuid,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
