-- name: memory_record_set_state :exec
UPDATE control_plane.memory_records SET state=$3,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND id=$2::uuid AND state<>'PURGED';
