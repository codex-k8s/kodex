-- name: memory_record_purge :exec
UPDATE control_plane.memory_record_revisions SET summary='' WHERE organization_id=$1::uuid AND record_id=$2::uuid;
