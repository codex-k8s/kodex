-- name: configuration_source__renew :exec
UPDATE control_plane.managed_configuration_source_work SET lease_expires_at=$2,updated_at=clock_timestamp()
WHERE id=$1::uuid AND state='CLAIMED';
