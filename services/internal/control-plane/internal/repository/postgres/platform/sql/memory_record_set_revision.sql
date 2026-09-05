-- name: memory_record_set_revision :exec
UPDATE control_plane.memory_records SET current_revision_id=$3::uuid,version=version+$4::bigint,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND id=$2::uuid AND state<>'PURGED';
