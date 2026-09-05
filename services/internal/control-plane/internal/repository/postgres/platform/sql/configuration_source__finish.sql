-- name: configuration_source__finish :exec
UPDATE control_plane.managed_configuration_source_work SET state=$2,receipt=$3::jsonb,failure_code=$4,
 completion_sha256=$5,updated_at=clock_timestamp()
WHERE id=$1::uuid AND state IN ('QUEUED','CLAIMED');
