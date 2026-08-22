-- name: platform__workers_changeoccurrence_5 :exec
UPDATE control_plane.schedule_occurrences SET state=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
