-- name: configuration_source__state :exec
UPDATE control_plane.managed_configuration_git_sources SET state=$2,failure_code=$3,
 version=version+1,next_refresh_at=clock_timestamp()+interval '5 minutes',updated_at=clock_timestamp()
WHERE id=$1::uuid;
