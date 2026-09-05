-- name: configuration_source__queue :exec
UPDATE control_plane.managed_configuration_git_sources SET state='QUEUED',failure_code='',
 version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND state IN ('READY','SYNC_BLOCKED');
